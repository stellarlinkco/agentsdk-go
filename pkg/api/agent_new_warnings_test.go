package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

func TestRuntimeNewContinuesOnEmbeddedHooksAndLoaderWarnings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir .agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "settings.json"), []byte(`{"model":"claude-3-opus"}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// Force AGENTS.md loader warning path.
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("@../outside.md"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	// Force command loader warnings.
	commandsDir := filepath.Join(agentsDir, "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandsDir, "bad.md"), []byte("---\nname: \"Bad Name\"\n---\n"), 0o600); err != nil {
		t.Fatalf("write bad command: %v", err)
	}

	// Force skill loader warnings.
	skillsDir := filepath.Join(agentsDir, "skills", "bad-skill")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("---\nname: \"Bad Name\"\ndescription: x\n---\n"), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Force subagent loader warnings.
	subagentsDir := filepath.Join(agentsDir, "agents")
	if err := os.MkdirAll(subagentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subagentsDir, "bad.md"), []byte("no frontmatter"), 0o600); err != nil {
		t.Fatalf("write bad agent: %v", err)
	}

	// Force rules loader warning path (rules path is not a directory).
	if err := os.WriteFile(filepath.Join(agentsDir, "rules"), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write rules: %v", err)
	}

	mdl := &stubModel{responses: []*model.Response{{Message: model.Message{Role: "assistant", Content: "ok"}}}}
	rt, err := New(context.Background(), Options{
		ProjectRoot: root,
		Model:       mdl,
		EmbedFS: fstest.MapFS{
			// Force embedded hook materializer warning (not a directory).
			".agents/hooks": &fstest.MapFile{Data: []byte("x")},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
}
