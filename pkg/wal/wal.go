package wal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	// DefaultSegmentBytes limits every WAL segment to 10MB before rotation.
	DefaultSegmentBytes int64 = 10 * 1024 * 1024
	metaFile                  = "wal.meta"
)

// ErrClosed indicates that the WAL has already been closed.
var ErrClosed = errors.New("wal: closed")

type config struct {
	segmentBytes int64
	disableSync  bool
	fileMode     os.FileMode
}

// Option configures WAL instances.
type Option func(*config)

// WithSegmentBytes overrides the default segment size limit.
func WithSegmentBytes(n int64) Option {
	return func(cfg *config) {
		if n > headerSize+crcSize {
			cfg.segmentBytes = n
		}
	}
}

// WithDisabledSync turns off fsync (tests only).
func WithDisabledSync() Option {
	return func(cfg *config) {
		cfg.disableSync = true
	}
}

// WithFileMode sets the permission bits applied to new WAL files.
func WithFileMode(mode os.FileMode) Option {
	return func(cfg *config) {
		cfg.fileMode = mode
	}
}

// WAL implements a segmented write-ahead log with crash recovery guarantees.
type WAL struct {
	dir     string
	cfg     config
	mu      sync.Mutex
	file    *os.File
	writer  *bufio.Writer
	current *segment

	segments       []*segment
	nextSegmentIdx int64
	base           Position
	next           Position
	closed         bool
}

type segment struct {
	index int64
	path  string
	start Position
	end   Position
	size  int64
}

// Open initializes a WAL rooted at dir, creating it if it does not exist.
func Open(dir string, opts ...Option) (*WAL, error) {
	cfg := config{
		segmentBytes: DefaultSegmentBytes,
		fileMode:     0o600,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.segmentBytes <= headerSize+crcSize {
		return nil, fmt.Errorf("wal: segment size too small")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("wal: mkdir %s: %w", dir, err)
	}

	w := &WAL{
		dir: dir,
		cfg: cfg,
	}
	if err := w.loadMeta(); err != nil {
		return nil, err
	}
	if err := w.loadSegments(); err != nil {
		return nil, err
	}
	if err := w.ensureActiveSegment(); err != nil {
		return nil, err
	}
	return w, nil
}

// Append writes entry to the WAL and returns its absolute position.
func (w *WAL) Append(entry Entry) (Position, error) {
	if len(entry.Type) == 0 {
		return 0, fmt.Errorf("wal: entry type required")
	}

	raw, err := entry.encode()
	if err != nil {
		return 0, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, ErrClosed
	}
	if err := w.ensureActiveSegmentLocked(); err != nil {
		return 0, err
	}
	if err := w.rollLocked(len(raw)); err != nil {
		return 0, err
	}

	pos := w.next
	n, err := w.writer.Write(raw)
	if err != nil {
		return 0, err
	}
	if n != len(raw) {
		return 0, io.ErrShortWrite
	}
	if err := w.writer.Flush(); err != nil {
		return 0, err
	}
	if w.current != nil {
		if w.current.start > pos {
			w.current.start = pos
		}
		w.current.end = pos
		w.current.size += int64(len(raw))
	}
	w.next++
	return pos, nil
}

// Sync flushes buffered writes to disk and issues fsync unless disabled.
func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	return w.syncLocked()
}

// Replay iterates through every record in order.
func (w *WAL) Replay(apply func(Entry) error) error {
	if apply == nil {
		return fmt.Errorf("wal: replay callback required")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.syncLocked(); err != nil {
		return err
	}

	for _, seg := range w.segments {
		if err := replaySegment(seg, apply); err != nil {
			return err
		}
	}
	return nil
}

// Truncate drops every record whose position is less than upto.
func (w *WAL) Truncate(upto Position) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	if upto <= w.base {
		return nil
	}
	if upto > w.next {
		upto = w.next
	}
	if err := w.syncLocked(); err != nil {
		return err
	}

	var kept []*segment
	for i, seg := range w.segments {
		if seg.end < seg.start || w.current == nil {
			// nothing persisted yet
		}
		if seg.end < upto {
			if err := w.removeSegment(seg); err != nil {
				return err
			}
			w.base = seg.end + 1
			continue
		}
		if upto <= seg.start {
			kept = append(kept, seg)
			continue
		}
		if err := w.trimSegment(seg, upto); err != nil {
			return err
		}
		w.base = upto
		kept = append(kept, seg)
		kept = append(kept, w.segments[i+1:]...)
		break
	}
	if len(kept) == 0 {
		if err := w.createSegmentLocked(); err != nil {
			return err
		}
		kept = w.segments
	}
	w.segments = kept
	return w.persistMetaLocked()
}

// Close flushes and releases underlying resources.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true

	var err error
	if syncErr := w.syncLocked(); syncErr != nil {
		err = syncErr
	}
	if w.file != nil {
		if closeErr := w.file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	w.file = nil
	w.writer = nil
	w.current = nil
	return err
}

func (w *WAL) rollLocked(nextLen int) error {
	if w.current == nil {
		return w.createSegmentLocked()
	}
	if w.current.size+int64(nextLen) <= w.cfg.segmentBytes {
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	w.writer = nil
	w.current = nil
	return w.createSegmentLocked()
}

func (w *WAL) ensureActiveSegment() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ensureActiveSegmentLocked()
}

func (w *WAL) ensureActiveSegmentLocked() error {
	if len(w.segments) == 0 {
		return w.createSegmentLocked()
	}
	if w.current != nil {
		return nil
	}
	last := w.segments[len(w.segments)-1]
	file, err := os.OpenFile(last.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, w.cfg.fileMode)
	if err != nil {
		return err
	}
	w.file = file
	w.writer = bufio.NewWriter(file)
	w.current = last
	return nil
}

func (w *WAL) createSegmentLocked() error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(w.dir, formatSegmentName(w.nextSegmentIdx))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, w.cfg.fileMode)
	if err != nil {
		return err
	}
	seg := &segment{
		index: w.nextSegmentIdx,
		path:  path,
		start: w.next,
		end:   w.next - 1,
	}
	w.nextSegmentIdx++
	w.segments = append(w.segments, seg)
	w.file = file
	w.writer = bufio.NewWriter(file)
	w.current = seg
	return nil
}

func (w *WAL) syncLocked() error {
	if w.writer != nil {
		if err := w.writer.Flush(); err != nil {
			return err
		}
	}
	if w.file != nil && !w.cfg.disableSync {
		return w.file.Sync()
	}
	return nil
}

func (w *WAL) loadSegments() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	var indexes []int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if idx, ok := parseSegmentIndex(entry.Name()); ok {
			indexes = append(indexes, idx)
		}
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })

	pos := w.base
	for _, idx := range indexes {
		path := filepath.Join(w.dir, formatSegmentName(idx))
		seg, nextPos, err := w.scanSegment(path, idx, pos)
		if err != nil {
			return err
		}
		w.segments = append(w.segments, seg)
		pos = nextPos
		w.nextSegmentIdx = idx + 1
	}
	if w.nextSegmentIdx == 0 {
		w.nextSegmentIdx = 1
	}
	w.next = pos
	return nil
}

func (w *WAL) scanSegment(path string, idx int64, start Position) (*segment, Position, error) {
	file, err := os.OpenFile(path, os.O_RDWR, w.cfg.fileMode)
	if err != nil {
		return nil, start, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	seg := &segment{
		index: idx,
		path:  path,
		start: start,
		end:   start - 1,
	}
	var (
		offset int64
		pos    = start
	)
	for {
		entry, read, err := decodeEntry(reader)
		if err == io.EOF {
			break
		}
		if errors.Is(err, errPartial) {
			if truncErr := file.Truncate(offset); truncErr != nil {
				return nil, start, truncErr
			}
			break
		}
		if err != nil {
			return nil, start, err
		}
		_ = entry
		seg.end = pos
		pos++
		offset += read
	}
	seg.size = offset
	return seg, pos, nil
}

func replaySegment(seg *segment, apply func(Entry) error) error {
	if seg.end < seg.start {
		// no data yet
		return nil
	}
	file, err := os.Open(seg.path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	pos := seg.start
	for {
		entry, _, err := decodeEntry(reader)
		if err == io.EOF {
			break
		}
		if errors.Is(err, errPartial) {
			// ignore dangling bytes
			break
		}
		if err != nil {
			return err
		}
		entry.Position = pos
		if err := apply(entry); err != nil {
			return err
		}
		pos++
	}
	return nil
}

func (w *WAL) trimSegment(seg *segment, upto Position) error {
	if seg.end < upto {
		return nil
	}
	if seg == w.current {
		if err := w.syncLocked(); err != nil {
			return err
		}
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
		w.writer = nil
		w.current = nil
	}

	file, err := os.Open(seg.path)
	if err != nil {
		return err
	}
	defer file.Close()

	tmp, err := os.CreateTemp(w.dir, "wal-trim-*")
	if err != nil {
		return err
	}

	reader := bufio.NewReader(file)
	kept := Position(0)
	pos := seg.start
	for {
		entry, _, err := decodeEntry(reader)
		if err == io.EOF {
			break
		}
		if errors.Is(err, errPartial) {
			break
		}
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return err
		}
		if pos >= upto {
			raw, encErr := entry.encode()
			if encErr != nil {
				tmp.Close()
				os.Remove(tmp.Name())
				return encErr
			}
			if _, writeErr := tmp.Write(raw); writeErr != nil {
				tmp.Close()
				os.Remove(tmp.Name())
				return writeErr
			}
			kept++
		}
		pos++
	}
	if !w.cfg.disableSync {
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), seg.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}

	seg.start = upto
	seg.end = upto + kept - 1
	if kept == 0 {
		seg.end = seg.start - 1
	}
	fileInfo, err := os.Stat(seg.path)
	if err == nil {
		seg.size = fileInfo.Size()
	}

	return w.ensureActiveSegmentLocked()
}

func (w *WAL) removeSegment(seg *segment) error {
	if seg == w.current {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
		w.writer = nil
		w.current = nil
	}
	if err := os.Remove(seg.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (w *WAL) loadMeta() error {
	path := filepath.Join(w.dir, metaFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var meta struct {
		Base Position `json:"base"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	w.base = meta.Base
	return nil
}

func (w *WAL) persistMetaLocked() error {
	meta := struct {
		Base Position `json:"base"`
	}{
		Base: w.base,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(w.dir, metaFile+".tmp")
	if err := os.WriteFile(tmpPath, data, w.cfg.fileMode); err != nil {
		return err
	}
	if !w.cfg.disableSync {
		if f, err := os.OpenFile(tmpPath, os.O_RDWR, w.cfg.fileMode); err == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return os.Rename(tmpPath, filepath.Join(w.dir, metaFile))
}

func formatSegmentName(index int64) string {
	return fmt.Sprintf("segment-%06d.wal", index)
}

func parseSegmentIndex(name string) (int64, bool) {
	if !strings.HasPrefix(name, "segment-") || !strings.HasSuffix(name, ".wal") {
		return 0, false
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(name, "segment-"), ".wal")
	var idx int64
	for _, ch := range trimmed {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		idx = idx*10 + int64(ch-'0')
	}
	return idx, true
}
