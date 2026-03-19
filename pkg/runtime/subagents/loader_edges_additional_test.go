package subagents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
)

func TestParseFrontMatterRejectsMissingFrontMatter(t *testing.T) {
	t.Parallel()

	_, _, err := parseFrontMatter("hello")
	if err == nil || !strings.Contains(err.Error(), "missing YAML frontmatter") {
		t.Fatalf("expected missing frontmatter error, got %v", err)
	}
}

func TestParseFrontMatterRejectsBadYAML(t *testing.T) {
	t.Parallel()

	_, _, err := parseFrontMatter("---\nname: [\n---\nbody")
	if err == nil || !strings.Contains(err.Error(), "decode YAML") {
		t.Fatalf("expected decode YAML error, got %v", err)
	}
}

func TestValidateMetadataMissingFields(t *testing.T) {
	t.Parallel()

	if err := validateMetadata(SubagentMetadata{}); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("expected name required, got %v", err)
	}
	if err := validateMetadata(SubagentMetadata{Name: "ok"}); err == nil || !strings.Contains(err.Error(), "description is required") {
		t.Fatalf("expected description required, got %v", err)
	}
}

func TestLoadSubagentDir_StatPermissionErrorIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions behave differently on windows")
	}

	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o700); err != nil {
			t.Errorf("chmod cleanup: %v", err)
		}
	})

	fsLayer := config.NewFS("", nil)
	_, errs := loadSubagentDir(filepath.Join(blocked, "agents"), fsLayer)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "subagents: stat") {
		t.Fatalf("expected stat error, got %v", errs)
	}
}

func TestLoadSubagentDir_RecordsWalkErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions behave differently on windows")
	}

	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	privateDir := filepath.Join(agentsDir, "private")
	if err := os.MkdirAll(privateDir, 0o700); err != nil {
		t.Fatalf("mkdir private: %v", err)
	}
	if err := os.WriteFile(filepath.Join(privateDir, "x.md"), []byte("---\nname: x\ndescription: x\n---\nbody"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(privateDir, 0o000); err != nil {
		t.Fatalf("chmod private: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(privateDir, 0o700); err != nil {
			t.Errorf("chmod cleanup: %v", err)
		}
	})

	fsLayer := config.NewFS(root, nil)
	_, errs := loadSubagentDir(agentsDir, fsLayer)
	if len(errs) == 0 || !hasError(errs, "subagents: walk") {
		t.Fatalf("expected walk error, got %v", errs)
	}
}
