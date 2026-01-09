package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeRolloutName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "session"},
		{in: "   ", want: "session"},
		{in: "sess-1", want: "sess-1"},
		{in: "a/b", want: "a_b"},
		{in: "A Z", want: "A_Z"},
		{in: "x.y", want: "x.y"},
	}

	for _, tc := range cases {
		if got := safeRolloutName(tc.in); got != tc.want {
			t.Fatalf("safeRolloutName(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAtomicWriteFileRenameErrorCleansTemp(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	if err := atomicWriteFile(targetDir, []byte("hi"), 0o600); err == nil {
		t.Fatal("expected rename error when destination is a directory")
	}

	leftovers, err := filepath.Glob(filepath.Join(dir, "target.*.tmp"))
	if err != nil {
		t.Fatalf("glob tmp: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("expected temp files cleaned up, got %v", leftovers)
	}
}

func TestRolloutWriterWriteCompactEventWritesJSON(t *testing.T) {
	root := t.TempDir()
	writer := newRolloutWriter(root, "rollouts")
	if writer == nil {
		t.Fatal("expected rollout writer")
	}

	res := compactResult{
		summary:       "summary",
		originalMsgs:  2,
		preservedMsgs: 1,
		tokensBefore:  10,
		tokensAfter:   3,
	}
	if err := writer.WriteCompactEvent("sess", res); err != nil {
		t.Fatalf("WriteCompactEvent: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(root, "rollouts"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 rollout file, got %d", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(root, "rollouts", entries[0].Name()))
	if err != nil {
		t.Fatalf("read rollout file: %v", err)
	}
	if !strings.Contains(string(data), `"session_id": "sess"`) {
		t.Fatalf("expected session_id in file, got %s", string(data))
	}
}

func TestRolloutWriterWriteCompactEventNoopsForNilOrEmpty(t *testing.T) {
	var nilWriter *RolloutWriter
	if err := nilWriter.WriteCompactEvent("sess", compactResult{}); err != nil {
		t.Fatalf("nil writer should noop, got %v", err)
	}

	writer := &RolloutWriter{dir: "   "}
	if err := writer.WriteCompactEvent("sess", compactResult{}); err != nil {
		t.Fatalf("empty dir writer should noop, got %v", err)
	}
}

func TestRolloutWriterWriteCompactEventMkdirError(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "rollouts")
	if err := os.WriteFile(target, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	writer := &RolloutWriter{dir: target}
	if err := writer.WriteCompactEvent("sess", compactResult{summary: "s"}); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestAtomicWriteFileCreateTempError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "file.json")
	if err := atomicWriteFile(path, []byte("hi"), 0o600); err == nil {
		t.Fatal("expected error when target directory does not exist")
	}
}
