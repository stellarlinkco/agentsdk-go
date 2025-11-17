package event

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	errStoreClosed     = errors.New("event: store closed")
	errStoreNil        = errors.New("event: store is nil")
	errMissingBookmark = errors.New("event: bookmark missing on event")
)

// FileEventStore 使用 JSONL 文件实现事件持久化，兼容回放与断点续播。
type FileEventStore struct {
	mu   sync.RWMutex
	path string
	file *os.File
}

// NewFileEventStore 创建文件事件存储，必要时会创建目录。
func NewFileEventStore(path string) (*FileEventStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("event: file store path is empty")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("event: create dir: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("event: open store: %w", err)
	}
	return &FileEventStore{path: path, file: file}, nil
}

func (s *FileEventStore) Append(evt Event) error {
	if s == nil {
		return errStoreNil
	}
	if evt.Bookmark == nil {
		return errMissingBookmark
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("event: marshal event: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return errStoreClosed
	}
	if _, err := s.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("event: append: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("event: sync: %w", err)
	}
	return nil
}

func (s *FileEventStore) ReadSince(bookmark *Bookmark) ([]Event, error) {
	events, err := s.readAll()
	if err != nil {
		return nil, err
	}
	if bookmark == nil {
		return events, nil
	}
	filtered := make([]Event, 0, len(events))
	for _, evt := range events {
		if evt.Bookmark == nil {
			continue
		}
		if compareBookmark(bookmark, evt.Bookmark) < 0 {
			filtered = append(filtered, evt)
		}
	}
	return filtered, nil
}

func (s *FileEventStore) ReadRange(start, end *Bookmark) ([]Event, error) {
	events, err := s.readAll()
	if err != nil {
		return nil, err
	}
	filtered := make([]Event, 0, len(events))
	for _, evt := range events {
		bm := evt.Bookmark
		if bm == nil {
			continue
		}
		if start != nil && compareBookmark(bm, start) <= 0 {
			continue
		}
		if end != nil && compareBookmark(bm, end) > 0 {
			break
		}
		filtered = append(filtered, evt)
	}
	return filtered, nil
}

func (s *FileEventStore) LastBookmark() (*Bookmark, error) {
	events, err := s.readAll()
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	last := events[len(events)-1]
	if last.Bookmark == nil {
		return nil, nil
	}
	return last.Bookmark.Clone(), nil
}

func (s *FileEventStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

func (s *FileEventStore) readAll() ([]Event, error) {
	if s == nil {
		return nil, errStoreNil
	}
	s.mu.RLock()
	path := s.path
	s.mu.RUnlock()
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("event: read store: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1<<20)
	var events []Event
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("event: scan store: %w", err)
	}
	return events, nil
}
