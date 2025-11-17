package main

import (
	"slices"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/session"
)

func TestMemorySessionBasics(t *testing.T) {
	memory := newMemorySession(t, "memory-basics")
	defer memory.Close()

	messages := []session.Message{
		{Role: "user", Content: "你好 agentsdk"},
		{Role: "assistant", Content: "欢迎来到内存会话"},
		{Role: "user", Content: "请继续记录"},
	}
	seedTranscript(t, memory, messages)

	got := listMessages(t, memory, session.Filter{})
	if len(got) != len(messages) {
		t.Fatalf("got %d messages want %d", len(got), len(messages))
	}
	for i, msg := range got {
		want := messages[i]
		if msg.Role != want.Role || msg.Content != want.Content {
			t.Fatalf("message %d = %+v want role=%s content=%s", i, msg, want.Role, want.Content)
		}
		if msg.ID == "" || msg.Timestamp.IsZero() {
			t.Fatalf("message %d missing id/timestamp: %+v", i, msg)
		}
	}
}

func TestCheckpointSaveAndResume(t *testing.T) {
	memory := newMemorySession(t, "memory-checkpoint")
	defer memory.Close()

	seed := []session.Message{
		{Role: "user", Content: "turn-1"},
		{Role: "assistant", Content: "turn-2"},
		{Role: "user", Content: "turn-3"},
	}
	seedTranscript(t, memory, seed)
	initial := listMessages(t, memory, session.Filter{})

	if err := memory.Checkpoint("baseline-turn"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	mustAppend(t, memory, session.Message{Role: "assistant", Content: "should be rolled back"})

	if err := memory.Resume("baseline-turn"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	assertTranscriptEqual(t, listMessages(t, memory, session.Filter{}), initial)
}

func TestSessionFork(t *testing.T) {
	parent := newMemorySession(t, "memory-fork")
	defer parent.Close()

	seedTranscript(t, parent, []session.Message{
		{Role: "user", Content: "fork-root"},
		{Role: "assistant", Content: "prepare to split"},
	})
	parentView := listMessages(t, parent, session.Filter{})

	child, err := parent.Fork("memory-fork-child")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	defer child.Close()

	assertTranscriptEqual(t, listMessages(t, child, session.Filter{}), parentView)
	mustAppend(t, child, session.Message{Role: "assistant", Content: "child-specific path"})
	assertTranscriptEqual(t, listMessages(t, parent, session.Filter{}), parentView)
	if got := listMessages(t, child, session.Filter{}); len(got) != len(parentView)+1 {
		t.Fatalf("child message count = %d want %d", len(got), len(parentView)+1)
	}
}

func TestSessionFilter(t *testing.T) {
	memory := newMemorySession(t, "memory-filter")
	defer memory.Close()

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	transcript := []session.Message{
		{Role: "user", Content: "bootstrap", Timestamp: base},
		{Role: "assistant", Content: "confirm", Timestamp: base.Add(1 * time.Minute)},
		{Role: "assistant", Content: "second assistant", Timestamp: base.Add(2 * time.Minute)},
		{Role: "user", Content: "wrap up", Timestamp: base.Add(3 * time.Minute)},
	}
	seedTranscript(t, memory, transcript)

	start := transcript[1].Timestamp
	end := transcript[2].Timestamp
	cases := []struct {
		name   string
		filter session.Filter
		want   []string
	}{
		{name: "role filter", filter: session.Filter{Role: "user"}, want: []string{"bootstrap", "wrap up"}},
		{name: "time window", filter: session.Filter{StartTime: &start, EndTime: &end}, want: []string{"confirm", "second assistant"}},
		{name: "offset and limit", filter: session.Filter{Offset: 1, Limit: 2}, want: []string{"confirm", "second assistant"}},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := listMessages(t, memory, tt.filter)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d want %d", len(got), len(tt.want))
			}
			for i, msg := range got {
				if tt.filter.Role != "" && msg.Role != tt.filter.Role {
					t.Fatalf("role = %s want %s", msg.Role, tt.filter.Role)
				}
				if msg.Content != tt.want[i] {
					t.Fatalf("content[%d] = %s want %s", i, msg.Content, tt.want[i])
				}
			}
		})
	}
}

func TestFileSessionPersistence(t *testing.T) {
	root := t.TempDir()
	fileSession := newFileSession(t, "file-persist", root)

	base := []session.Message{
		{Role: "user", Content: "persist input"},
		{Role: "assistant", Content: "persist reply"},
	}
	seedTranscript(t, fileSession, base)
	stable := listMessages(t, fileSession, session.Filter{})
	if err := fileSession.Checkpoint("file-baseline"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	mustAppend(t, fileSession, session.Message{Role: "assistant", Content: "volatile"})
	if err := fileSession.Resume("file-baseline"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	assertTranscriptEqual(t, listMessages(t, fileSession, session.Filter{}), stable)

	if err := fileSession.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reloaded := newFileSession(t, "file-persist", root)
	defer reloaded.Close()

	persisted := listMessages(t, reloaded, session.Filter{})
	assertTranscriptEqual(t, persisted, stable)

	child, err := reloaded.Fork("file-child")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	defer child.Close()
	mustAppend(t, child, session.Message{Role: "assistant", Content: "child divergence"})
	parentAfter := listMessages(t, reloaded, session.Filter{})
	assertTranscriptEqual(t, parentAfter, stable)
}

func newMemorySession(t *testing.T, id string) *session.MemorySession {
	t.Helper()
	memory, err := session.NewMemorySession(id)
	if err != nil {
		t.Fatalf("new memory session: %v", err)
	}
	return memory
}

func newFileSession(t *testing.T, id, root string) *session.FileSession {
	t.Helper()
	fs, err := session.NewFileSession(id, root)
	if err != nil {
		t.Fatalf("new file session: %v", err)
	}
	return fs
}

func seedTranscript(t *testing.T, sess session.Session, msgs []session.Message) {
	t.Helper()
	for _, msg := range msgs {
		mustAppend(t, sess, msg)
	}
}

func mustAppend(t *testing.T, sess session.Session, msg session.Message) {
	t.Helper()
	if err := appendMessage(sess, msg); err != nil {
		t.Fatalf("append message: %v", err)
	}
}

func listMessages(t *testing.T, sess session.Session, filter session.Filter) []session.Message {
	t.Helper()
	msgs, err := sess.List(filter)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return msgs
}

func assertTranscriptEqual(t *testing.T, got, want []session.Message) {
	t.Helper()
	if slices.EqualFunc(got, want, func(x, y session.Message) bool {
		return x.ID == y.ID &&
			x.Role == y.Role &&
			x.Content == y.Content &&
			x.Timestamp.Equal(y.Timestamp)
	}) {
		return
	}
	t.Fatalf("transcripts differ\n got: %+v\nwant: %+v", got, want)
}
