package toolbuiltin

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/sandbox"
)

func TestGrepToolExecute_MissedValidationBranches(t *testing.T) {
	t.Parallel()
	skipIfWindows(t)

	dir := cleanTempDir(t)

	if _, err := NewGrepToolWithRoot(dir).Execute(nil, map[string]any{"pattern": "x", "path": dir}); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("expected nil context error, got %v", err)
	}

	if _, err := (*GrepTool)(nil).Execute(context.Background(), map[string]any{"pattern": "x", "path": dir}); err == nil || !strings.Contains(err.Error(), "not initialised") {
		t.Fatalf("expected nil tool error, got %v", err)
	}

	tool := NewGrepToolWithSandbox(dir, sandbox.NewFileSystemAllowList(dir))

	for name, params := range map[string]map[string]any{
		"missing pattern": {"path": dir},
		"bad output_mode": {"pattern": "x", "path": dir, "output_mode": "wat"},
		"bad glob type":   {"pattern": "x", "path": dir, "glob": 123},
		"bad type type":   {"pattern": "x", "path": dir, "type": 123},
		"bad head_limit":  {"pattern": "x", "path": dir, "head_limit": -1},
		"bad offset":      {"pattern": "x", "path": dir, "offset": -1},
		"bad -n":          {"pattern": "x", "path": dir, "-n": "maybe"},
		"bad -i":          {"pattern": "x", "path": dir, "-i": "maybe"},
		"bad multiline":   {"pattern": "x", "path": dir, "multiline": "nope"},
	} {
		_, err := tool.Execute(context.Background(), params)
		if err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestGrepToolExecute_ContextCancelledAfterResolveSearchPath(t *testing.T) {
	t.Parallel()
	skipIfWindows(t)

	dir := cleanTempDir(t)
	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("hit"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tool := NewGrepToolWithRoot(dir)
	_, err := tool.Execute(ctx, map[string]any{"pattern": "hit", "path": target})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestGrepTool_ResolveSearchPath_SandboxAndStatBranches(t *testing.T) {
	t.Parallel()
	skipIfWindows(t)

	dir := cleanTempDir(t)
	other := cleanTempDir(t)

	tool := NewGrepToolWithSandbox(dir, sandbox.NewFileSystemAllowList(dir))

	if _, _, err := tool.resolveSearchPath(map[string]any{"path": other}); err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("expected sandbox error for other dir, got %v", err)
	}

	if _, _, err := tool.resolveSearchPath(map[string]any{"path": "missing.txt"}); err == nil || !strings.Contains(err.Error(), "stat path") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

func TestGrepParams_MissedBranches(t *testing.T) {
	t.Parallel()

	if got, err := parseContextLines(nil, 5); err != nil || got != 0 {
		t.Fatalf("parseContextLines(nil)=%d err=%v", got, err)
	}

	if got, ok, err := parseBoolParam(nil, "-n"); err != nil || ok || got {
		t.Fatalf("parseBoolParam(nil)=%v,%v err=%v", got, ok, err)
	}

	if _, _, err := parseContextParams(map[string]any{"-C": "x"}, 5); err == nil {
		t.Fatalf("expected -C type error")
	}
	if _, _, err := parseContextParams(map[string]any{"-C": -1}, 5); err == nil {
		t.Fatalf("expected -C negative error")
	}
	if _, _, err := parseContextParams(map[string]any{"-A": "x"}, 5); err == nil {
		t.Fatalf("expected -A type error")
	}
	if _, _, err := parseContextParams(map[string]any{"-A": -1}, 5); err == nil {
		t.Fatalf("expected -A negative error")
	}
	if _, _, err := parseContextParams(map[string]any{"-B": "x"}, 5); err == nil {
		t.Fatalf("expected -B type error")
	}
	if _, _, err := parseContextParams(map[string]any{"-B": -1}, 5); err == nil {
		t.Fatalf("expected -B negative error")
	}
}

func TestGrepSearch_MissedBranches(t *testing.T) {
	t.Parallel()
	skipIfWindows(t)

	dir := cleanTempDir(t)
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("hit\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	tool := NewGrepToolWithRoot(dir)
	re := regexp.MustCompile("hit")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var matches []GrepMatch
	if _, err := tool.searchFile(ctx, target, re, grepSearchOptions{}, &matches); err == nil {
		t.Fatalf("expected cancellation error")
	}

	// searchDirectory propagates searchFile errors.
	matches = nil
	if _, err := tool.searchDirectory(context.Background(), dir, re, grepSearchOptions{glob: "["}, &matches); err == nil || !strings.Contains(err.Error(), "invalid glob") {
		t.Fatalf("expected invalid glob error, got %v", err)
	}

	allowErrOpts := grepSearchOptions{glob: "["}
	matches = nil
	if _, err := tool.searchFile(context.Background(), target, re, allowErrOpts, &matches); err == nil || !strings.Contains(err.Error(), "invalid glob") {
		t.Fatalf("expected invalid glob error, got %v", err)
	}

	typeErrOpts := grepSearchOptions{typeGlobs: []string{"[]"}}
	matches = nil
	if _, err := tool.searchFile(context.Background(), target, re, typeErrOpts, &matches); err == nil || !strings.Contains(err.Error(), "invalid type pattern") {
		t.Fatalf("expected invalid type pattern error, got %v", err)
	}

	if got := uniqueFiles([]GrepMatch{{File: "a"}, {File: "a"}, {File: "b"}}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("uniqueFiles=%v", got)
	}
}
