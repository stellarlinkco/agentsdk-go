package toolbuiltin

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestGrepToolSearchFile_RejectsSandboxEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("match"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewGrepToolWithRoot(root)
	opts := grepSearchOptions{}
	re := regexp.MustCompile("match")
	var matches []GrepMatch
	if _, err := tool.searchFile(context.Background(), outside, re, opts, &matches); err == nil {
		t.Fatalf("expected sandbox validation error")
	}
}

func TestGrepToolSearchFile_MultilineContextBranches(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	path := filepath.Join(root, "a.txt")
	content := "aaa\nmatch1\nmatch2\nbbb\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	tool := NewGrepToolWithRoot(resolvedRoot)
	opts := grepSearchOptions{before: 1, after: 1, multiline: true}
	re := regexp.MustCompile("match1\\nmatch2")

	var matches []GrepMatch
	truncated, err := tool.searchFile(context.Background(), path, re, opts, &matches)
	if err != nil || truncated {
		t.Fatalf("searchFile err=%v truncated=%v", err, truncated)
	}
	if len(matches) != 1 {
		t.Fatalf("matches=%v", matches)
	}
	if len(matches[0].Before) != 1 || matches[0].Before[0] != "aaa" {
		t.Fatalf("before=%v", matches[0].Before)
	}
	if len(matches[0].After) != 1 || matches[0].After[0] != "match2" {
		t.Fatalf("after=%v", matches[0].After)
	}
}
