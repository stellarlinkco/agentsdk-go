package event

import "time"

// Bookmark 表示事件流中的断点标记，用于断点续播与回放。
type Bookmark struct {
	Seq       int64     `json:"seq"`
	Timestamp time.Time `json:"timestamp"`
}

// Clone 返回 Bookmark 的副本，避免共享指针导致的数据竞争。
func (b *Bookmark) Clone() *Bookmark {
	if b == nil {
		return nil
	}
	clone := *b
	return &clone
}

// EventStore 定义事件持久化与回放接口。
type EventStore interface {
	Append(event Event) error
	ReadSince(bookmark *Bookmark) ([]Event, error)
	ReadRange(start, end *Bookmark) ([]Event, error)
	LastBookmark() (*Bookmark, error)
}

func compareBookmark(a, b *Bookmark) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	case a.Seq < b.Seq:
		return -1
	case a.Seq > b.Seq:
		return 1
	default:
		return 0
	}
}

func newBookmark(seq int64) *Bookmark {
	if seq < 0 {
		seq = 0
	}
	return &Bookmark{
		Seq:       seq,
		Timestamp: time.Now().UTC(),
	}
}
