package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cexll/agentsdk-go/pkg/wal"
)

const (
	recordMessage    = "message"
	recordCheckpoint = "checkpoint"
	recordResume     = "resume"
)

// FileSession persists conversation transcripts through a WAL.
type FileSession struct {
	id      string
	root    string
	dir     string
	walDir  string
	log     *wal.WAL
	walOpts []wal.Option

	mu          sync.RWMutex
	messages    []Message
	checkpoints map[string]*checkpointState
	seq         uint64
	closed      bool
	now         func() time.Time
}

type checkpointState struct {
	position wal.Position
	snapshot []Message
	created  time.Time
}

// NewFileSession creates (or re-opens) a durable session located at root/id.
func NewFileSession(id, root string, opts ...wal.Option) (*FileSession, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return nil, ErrInvalidSessionID
	}
	sessionDir := filepath.Join(root, trimmed)
	walDir := filepath.Join(sessionDir, "wal")
	if err := os.MkdirAll(walDir, 0o755); err != nil {
		return nil, fmt.Errorf("session: mkdir wal dir: %w", err)
	}
	log, err := wal.Open(walDir, opts...)
	if err != nil {
		return nil, err
	}
	fs := &FileSession{
		id:          trimmed,
		root:        root,
		dir:         sessionDir,
		walDir:      walDir,
		log:         log,
		walOpts:     append([]wal.Option(nil), opts...),
		checkpoints: make(map[string]*checkpointState),
		now:         time.Now,
	}
	if err := fs.reload(); err != nil {
		_ = log.Close()
		return nil, err
	}
	return fs, nil
}

// ID returns the session identifier.
func (s *FileSession) ID() string { return s.id }

// Append appends a message to the persistent transcript.
func (s *FileSession) Append(msg Message) error {
	if strings.TrimSpace(msg.Role) == "" {
		return fmt.Errorf("%w: role is required", ErrInvalidMessage)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}

	clone := cloneMessage(msg)
	s.seq++
	if clone.ID == "" {
		clone.ID = fmt.Sprintf("%s-%06d", s.id, s.seq)
	}
	if clone.Timestamp.IsZero() {
		clone.Timestamp = s.now().UTC()
	} else {
		clone.Timestamp = clone.Timestamp.UTC()
	}
	clone.ToolCalls = cloneToolCalls(clone.ToolCalls)

	record := walRecord{
		Kind:    recordMessage,
		Message: &clone,
	}
	if _, err := s.appendRecord(record); err != nil {
		return err
	}
	s.messages = append(s.messages, cloneMessage(clone))
	return nil
}

// List returns messages matching the filter.
func (s *FileSession) List(filter Filter) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrSessionClosed
	}
	role := strings.TrimSpace(filter.Role)
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	limit := filter.Limit
	if limit < 0 {
		limit = 0
	}
	var (
		start time.Time
		end   time.Time
	)
	hasStart := filter.StartTime != nil
	if hasStart {
		start = filter.StartTime.UTC()
	}
	hasEnd := filter.EndTime != nil
	if hasEnd {
		end = filter.EndTime.UTC()
	}
	var (
		result  []Message
		skipped int
	)
	for _, msg := range s.messages {
		if role != "" && msg.Role != role {
			continue
		}
		if hasStart && msg.Timestamp.Before(start) {
			continue
		}
		if hasEnd && msg.Timestamp.After(end) {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		result = append(result, cloneMessage(msg))
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

// Checkpoint captures the current transcript for future resuming.
func (s *FileSession) Checkpoint(name string) error {
	normalized, err := normalizeCheckpointName(name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}
	snapshot := cloneMessages(s.messages)
	record := walRecord{
		Kind:       recordCheckpoint,
		Checkpoint: normalized,
		Snapshot:   snapshot,
		Created:    s.now().UTC(),
	}
	pos, err := s.appendRecord(record)
	if err != nil {
		return err
	}
	s.checkpoints[normalized] = &checkpointState{
		position: pos,
		snapshot: snapshot,
		created:  record.Created,
	}
	s.gcLocked()
	return nil
}

// Resume rewinds the session to a previously created checkpoint.
func (s *FileSession) Resume(name string) error {
	normalized, err := normalizeCheckpointName(name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}
	cp, ok := s.checkpoints[normalized]
	if !ok {
		return fmt.Errorf("%w: %s", ErrCheckpointNotFound, normalized)
	}
	restore := cloneMessages(cp.snapshot)
	record := walRecord{
		Kind:       recordResume,
		Checkpoint: normalized,
	}
	if _, err := s.appendRecord(record); err != nil {
		return err
	}
	s.messages = restore
	s.seq = uint64(len(s.messages))
	s.gcLocked()
	return nil
}

// Fork clones the transcript into a new session rooted at the same directory.
func (s *FileSession) Fork(id string) (Session, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return nil, ErrInvalidSessionID
	}
	s.mu.RLock()
	snapshot := cloneMessages(s.messages)
	s.mu.RUnlock()

	child, err := NewFileSession(trimmed, s.root, s.walOpts...)
	if err != nil {
		return nil, err
	}
	for _, msg := range snapshot {
		if err := child.Append(msg); err != nil {
			_ = child.Close()
			return nil, err
		}
	}
	return child, nil
}

// Close releases underlying resources.
func (s *FileSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.log.Close()
}

func (s *FileSession) appendRecord(rec walRecord) (wal.Position, error) {
	payload, err := json.Marshal(rec)
	if err != nil {
		return 0, err
	}
	pos, err := s.log.Append(wal.Entry{Type: rec.Kind, Data: payload})
	if err != nil {
		return 0, err
	}
	if err := s.log.Sync(); err != nil {
		return 0, err
	}
	return pos, nil
}

func (s *FileSession) reload() error {
	var (
		messages    []Message
		checkpoints = make(map[string]*checkpointState)
		seq         uint64
	)
	err := s.log.Replay(func(e wal.Entry) error {
		var rec walRecord
		if err := json.Unmarshal(e.Data, &rec); err != nil {
			return err
		}
		switch rec.Kind {
		case recordMessage:
			if rec.Message == nil {
				return fmt.Errorf("session: message record missing payload")
			}
			msg := cloneMessage(*rec.Message)
			msg.Timestamp = msg.Timestamp.UTC()
			messages = append(messages, msg)
			seq++
		case recordCheckpoint:
			cpSnapshot := cloneMessages(rec.Snapshot)
			messages = cloneMessages(cpSnapshot)
			seq = uint64(len(messages))
			checkpoints[rec.Checkpoint] = &checkpointState{
				position: e.Position,
				snapshot: cpSnapshot,
				created:  rec.Created.UTC(),
			}
		case recordResume:
			cp, ok := checkpoints[rec.Checkpoint]
			if !ok {
				return fmt.Errorf("session: resume references unknown checkpoint %s", rec.Checkpoint)
			}
			messages = cloneMessages(cp.snapshot)
			seq = uint64(len(messages))
		default:
			return fmt.Errorf("session: unknown wal record %s", rec.Kind)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.messages = messages
	s.checkpoints = checkpoints
	s.seq = seq
	return nil
}

func (s *FileSession) gcLocked() {
	if len(s.checkpoints) == 0 {
		return
	}
	var earliest *checkpointState
	for _, cp := range s.checkpoints {
		if earliest == nil || cp.position < earliest.position {
			earliest = cp
		}
	}
	if earliest != nil && earliest.position > 0 {
		_ = s.log.Truncate(earliest.position)
	}
}

type walRecord struct {
	Kind       string    `json:"kind"`
	Message    *Message  `json:"message,omitempty"`
	Checkpoint string    `json:"checkpoint,omitempty"`
	Snapshot   []Message `json:"snapshot,omitempty"`
	Created    time.Time `json:"created,omitempty"`
}

var _ Session = (*FileSession)(nil)
