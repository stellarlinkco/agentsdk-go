package toolbuiltin

import (
	"path/filepath"
	"testing"
)

func TestResolveTypeGlobs_CoverageGaps(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"cs":     {"*.cs"},
		"swift":  {"*.swift"},
		"bash":   {"*.sh"},
		"kotlin": {"*.kt", "*.kts"},
	}
	for typ, want := range cases {
		got := resolveTypeGlobs(typ)
		if len(got) != len(want) {
			t.Fatalf("type=%q got=%v want=%v", typ, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("type=%q got=%v want=%v", typ, got, want)
			}
		}
	}
}

func TestParseContextParams_ClampsAandB(t *testing.T) {
	t.Parallel()

	before, after, err := parseContextParams(map[string]any{"-A": 99, "-B": 99}, 5)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if before != 5 || after != 5 {
		t.Fatalf("before=%d after=%d", before, after)
	}
}

func TestResolveSearchPath_PathTypeError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	tool := NewGrepToolWithRoot(resolvedRoot)
	if _, _, err := tool.resolveSearchPath(map[string]any{"path": 1}); err == nil {
		t.Fatalf("expected error")
	}
}
