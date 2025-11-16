package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Bookmark 记录断点续播所需的 WAL 位点和状态快照。
type Bookmark struct {
	ID       string          `json:"id"`
	Position int64           `json:"position"`
	State    json.RawMessage `json:"state,omitempty"`
}

var (
	// ErrBookmarkNotFound 表示请求的 Bookmark 不存在。
	ErrBookmarkNotFound = errors.New("bookmark: not found")

	errNilBookmark = errors.New("bookmark: nil reference")
	errNilStore    = errors.New("bookmark: store is nil")
)

// NewBookmark 创建新的断点标记，state 将以 JSON 形式持久化。
func NewBookmark(id string, position int64, state any) (*Bookmark, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("bookmark: id is empty")
	}
	if position < 0 {
		return nil, fmt.Errorf("bookmark: invalid position %d", position)
	}
	snapshot, err := encodeState(state)
	if err != nil {
		return nil, err
	}
	return &Bookmark{ID: id, Position: position, State: snapshot}, nil
}

// Clone 深拷贝 Bookmark，避免共享底层 slice。
func (b *Bookmark) Clone() *Bookmark {
	if b == nil {
		return nil
	}
	clone := *b
	if len(b.State) > 0 {
		clone.State = append(json.RawMessage(nil), b.State...)
	}
	return &clone
}

// Snapshot 使用新的状态快照更新 Bookmark。
func (b *Bookmark) Snapshot(state any) error {
	if b == nil {
		return errNilBookmark
	}
	snapshot, err := encodeState(state)
	if err != nil {
		return err
	}
	b.State = snapshot
	return nil
}

// Restore 将状态快照解码到目标对象。
func (b *Bookmark) Restore(target any) error {
	if b == nil || len(b.State) == 0 || target == nil {
		return nil
	}
	return json.Unmarshal(b.State, target)
}

// Resume 基于 Bookmark 恢复到对应的 WAL 位置，并将状态写入 target。
func (b *Bookmark) Resume(target any) (int64, error) {
	if b == nil {
		return 0, errNilBookmark
	}
	if target != nil {
		if err := b.Restore(target); err != nil {
			return 0, err
		}
	}
	return b.Position, nil
}

// Advance 更新 WAL 位置，防止回退。
func (b *Bookmark) Advance(position int64) error {
	if b == nil {
		return errNilBookmark
	}
	if position < b.Position {
		return fmt.Errorf("bookmark: position rollback %d -> %d", b.Position, position)
	}
	b.Position = position
	return nil
}

// Serialize 将 Bookmark 序列化为 JSON。
func (b *Bookmark) Serialize() ([]byte, error) {
	if b == nil {
		return nil, errNilBookmark
	}
	return json.Marshal(b)
}

// DeserializeBookmark 从 JSON 载荷中恢复 Bookmark。
func DeserializeBookmark(payload []byte) (*Bookmark, error) {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return nil, errors.New("bookmark: empty payload")
	}
	var b Bookmark
	if err := json.Unmarshal(payload, &b); err != nil {
		return nil, fmt.Errorf("bookmark: deserialize: %w", err)
	}
	return b.Clone(), nil
}

// BookmarkStore 提供线程安全的检查点登记与恢复能力。
type BookmarkStore struct {
	mu    sync.RWMutex
	items map[string]*Bookmark
}

// NewBookmarkStore 创建 BookmarkStore，可选地预加载初始数据。
func NewBookmarkStore(initial ...*Bookmark) *BookmarkStore {
	store := &BookmarkStore{items: make(map[string]*Bookmark)}
	for _, bm := range initial {
		if bm == nil {
			continue
		}
		store.items[bm.ID] = bm.Clone()
	}
	return store
}

// Checkpoint 记录一个新的 Bookmark，返回该 Bookmark 的深拷贝。
func (s *BookmarkStore) Checkpoint(id string, position int64, state any) (*Bookmark, error) {
	if s == nil {
		return nil, errNilStore
	}
	bm, err := NewBookmark(id, position, state)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[string]*Bookmark)
	}
	s.items[bm.ID] = bm
	return bm.Clone(), nil
}

// Resume 根据 ID 恢复 checkpoint，将快照写入 target 并返回 WAL position。
func (s *BookmarkStore) Resume(id string, target any) (int64, error) {
	if s == nil {
		return 0, errNilStore
	}
	s.mu.RLock()
	bm := s.items[id]
	s.mu.RUnlock()
	if bm == nil {
		return 0, fmt.Errorf("%w: %s", ErrBookmarkNotFound, id)
	}
	return bm.Clone().Resume(target)
}

// Serialize 将 store 中的 Bookmark 序列化为稳定排序的 JSON。
func (s *BookmarkStore) Serialize() ([]byte, error) {
	if s == nil {
		return nil, errNilStore
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.items) == 0 {
		return []byte("[]"), nil
	}
	list := make([]*Bookmark, 0, len(s.items))
	for _, bm := range s.items {
		list = append(list, bm.Clone())
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return json.Marshal(list)
}

// DeserializeBookmarkStore 恢复 BookmarkStore。
func DeserializeBookmarkStore(payload []byte) (*BookmarkStore, error) {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return NewBookmarkStore(), nil
	}
	var list []*Bookmark
	if err := json.Unmarshal(payload, &list); err != nil {
		return nil, fmt.Errorf("bookmark: deserialize store: %w", err)
	}
	return NewBookmarkStore(list...), nil
}

func encodeState(state any) (json.RawMessage, error) {
	switch v := state.(type) {
	case nil:
		return nil, nil
	case []byte:
		return append(json.RawMessage(nil), v...), nil
	case json.RawMessage:
		return append(json.RawMessage(nil), v...), nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("bookmark: marshal state: %w", err)
		}
		return data, nil
	}
}
