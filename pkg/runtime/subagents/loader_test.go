package subagents

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
)

func TestLoadFromFS_Basic(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		"---",
		"name: helper",
		"description: basic helper",
		"---",
		"System prompt body",
	}, "\n")
	mustWrite(t, root, ".agents/agents/helper.md", content)

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(regs) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(regs))
	}

	reg := findRegistration(t, regs, "helper")
	if reg.Definition.Description != "basic helper" {
		t.Fatalf("unexpected description: %+v", reg.Definition)
	}
	res, err := reg.Handler.Handle(context.Background(), Context{}, Request{Instruction: "run"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.Output != "System prompt body" {
		t.Fatalf("unexpected output %q", res.Output)
	}
}

func TestLoadFromFS_IgnoresUserDir(t *testing.T) {
	projectRoot := t.TempDir()
	userHome := t.TempDir()

	mustWrite(t, userHome, ".agents/agents/user-only.md", strings.Join([]string{
		"---",
		"name: user-only",
		"description: only user",
		"---",
		"user only prompt",
	}, "\n"))
	mustWrite(t, projectRoot, ".agents/agents/shared.md", strings.Join([]string{
		"---",
		"name: shared",
		"description: project def",
		"---",
		"project prompt",
	}, "\n"))

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: projectRoot, UserHome: userHome, EnableUser: true})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(regs) != 1 {
		t.Fatalf("expected only project registrations, got %d", len(regs))
	}

	shared := findRegistration(t, regs, "shared")
	res, err := shared.Handler.Handle(context.Background(), Context{}, Request{Instruction: "go"})
	if err != nil || res.Output != "project prompt" {
		t.Fatalf("expected project prompt, got %v %q", err, res.Output)
	}
	for _, reg := range regs {
		if reg.Definition.Name == "user-only" {
			t.Fatalf("user-level subagent should be ignored")
		}
	}
}

func TestLoadFromFS_NoProjectDir(t *testing.T) {
	projectRoot := t.TempDir()
	userHome := t.TempDir()

	mustWrite(t, userHome, ".agents/agents/user-only.md", strings.Join([]string{
		"---",
		"name: user-only",
		"description: only user",
		"---",
		"user only prompt",
	}, "\n"))

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: projectRoot, UserHome: userHome, EnableUser: true})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(regs) != 0 {
		t.Fatalf("expected no registrations without project config, got %d", len(regs))
	}
}

func TestLoadFromFS_YAML(t *testing.T) {
	root := t.TempDir()
	body := strings.Join([]string{
		"---",
		"name: custom-agent",
		"description: greeting agent",
		"tools: read, write",
		"model: haiku",
		"permissionMode: plan",
		"skills: docs, go ",
		"---",
		"## prompt body",
	}, "\n")
	path := mustWrite(t, root, ".agents/agents/ignored.md", body)

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(regs) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(regs))
	}

	reg := regs[0]
	if reg.Definition.Name != "custom-agent" || reg.Definition.Description != "greeting agent" {
		t.Fatalf("unexpected definition: %+v", reg.Definition)
	}
	if !reflect.DeepEqual(reg.Definition.BaseContext.ToolWhitelist, []string{"read", "write"}) {
		t.Fatalf("unexpected whitelist: %+v", reg.Definition.BaseContext.ToolWhitelist)
	}
	if reg.Definition.BaseContext.Model != "haiku" || reg.Definition.DefaultModel != "haiku" {
		t.Fatalf("model not propagated: %+v", reg.Definition)
	}

	res, err := reg.Handler.Handle(context.Background(), Context{}, Request{Instruction: "run"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.Output != "## prompt body" {
		t.Fatalf("unexpected output %q", res.Output)
	}
	if res.Metadata == nil {
		t.Fatalf("expected metadata")
	}
	if src, ok := res.Metadata["source"]; !ok || src != path {
		t.Fatalf("missing source metadata: %#v", res.Metadata)
	}
	if pm := res.Metadata["permission-mode"]; pm != "plan" {
		t.Fatalf("permission-mode mismatch: %#v", res.Metadata)
	}
	if tools, ok := res.Metadata["tools"].([]string); !ok || !reflect.DeepEqual(tools, []string{"read", "write"}) {
		t.Fatalf("tools metadata mismatch: %#v", res.Metadata)
	}
	if skills, ok := res.Metadata["skills"].([]string); !ok || !reflect.DeepEqual(skills, []string{"docs", "go"}) {
		t.Fatalf("skills metadata mismatch: %#v", res.Metadata)
	}
}

func TestLoadFromFS_MetadataParsing(t *testing.T) {
	root := t.TempDir()
	body := strings.Join([]string{
		"---",
		"name: worker",
		"description: with lists",
		"tools: read, read, bash",
		"skills: Go, docs, go",
		"model: inherit",
		"permissionMode: default",
		"---",
		"list body",
	}, "\n")
	mustWrite(t, root, ".agents/agents/worker.md", body)

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(regs) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(regs))
	}
	reg := regs[0]
	if !reflect.DeepEqual(reg.Definition.BaseContext.ToolWhitelist, []string{"bash", "read"}) {
		t.Fatalf("tool whitelist mismatch: %+v", reg.Definition.BaseContext.ToolWhitelist)
	}
	if reg.Definition.BaseContext.Model != "" || reg.Definition.DefaultModel != "" {
		t.Fatalf("inherit model should be empty, got %+v", reg.Definition)
	}
	res, err := reg.Handler.Handle(context.Background(), Context{}, Request{Instruction: "go"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if pm := res.Metadata["permission-mode"]; pm != "default" {
		t.Fatalf("expected permission-mode default, got %#v", res.Metadata)
	}
	if skills, ok := res.Metadata["skills"].([]string); !ok || !reflect.DeepEqual(skills, []string{"docs", "go"}) {
		t.Fatalf("skills metadata mismatch: %#v", res.Metadata)
	}
}

func TestLoadFromFS_Errors(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, ".agents/agents/bad name.md", strings.Join([]string{
		"---",
		"description: missing name uses fallback",
		"---",
		"body",
	}, "\n"))
	mustWrite(t, root, ".agents/agents/broken.md", "---\nname: ok\n")
	mustWrite(t, root, ".agents/agents/good.md", strings.Join([]string{
		"---",
		"name: good",
		"description: ok",
		"---",
		"good body",
	}, "\n"))

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if len(regs) != 1 {
		t.Fatalf("expected only good file loaded, got %d", len(regs))
	}
	if len(errs) < 2 {
		t.Fatalf("expected aggregated errors, got %v", errs)
	}
	if !hasError(errs, "invalid name") {
		t.Fatalf("missing invalid name error: %v", errs)
	}
	if !hasError(errs, "missing closing frontmatter") && !hasError(errs, "decode YAML") {
		t.Fatalf("missing frontmatter error: %v", errs)
	}
}

func TestLoadFromFS_ModelValidationError(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, ".agents/agents/invalid-model.md", strings.Join([]string{
		"---",
		"name: invalid-model",
		"description: bad model",
		"model: delta",
		"---",
		"body",
	}, "\n"))

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if len(regs) != 0 {
		t.Fatalf("expected registrations to be skipped, got %d", len(regs))
	}
	if !hasError(errs, "invalid model") {
		t.Fatalf("expected invalid model error, got %v", errs)
	}
}

func TestValidateMetadataRejectsInvalidFields(t *testing.T) {
	meta := SubagentMetadata{Name: "ok", Description: "desc", Model: "unknown"}
	if err := validateMetadata(meta); err == nil || !strings.Contains(err.Error(), "invalid model") {
		t.Fatalf("expected invalid model error, got %v", err)
	}
	meta.Model = "sonnet"
	meta.PermissionMode = "illegal"
	if err := validateMetadata(meta); err == nil || !strings.Contains(err.Error(), "invalid permissionMode") {
		t.Fatalf("expected invalid permission error, got %v", err)
	}
}

func TestNormalizeModelBoundaries(t *testing.T) {
	if model := normalizeModel("inherit"); model != "" {
		t.Fatalf("inherit should return empty, got %q", model)
	}
	if model := normalizeModel("unknown"); model != "" {
		t.Fatalf("unknown should normalize to empty, got %q", model)
	}
}

func TestLoadSubagentDirRejectsFilePath(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "notdir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	fsys := config.NewFS(root, nil)
	_, errs := loadSubagentDir(file, fsys)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "not a directory") {
		t.Fatalf("expected directory error, got %v", errs)
	}
}

func mustWrite(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := join(root, relative)
	if err := makeDirs(path); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func makeDirs(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0o755)
}

func join(parts ...string) string {
	return filepath.Join(parts...)
}

func findRegistration(t *testing.T, regs []SubagentRegistration, name string) SubagentRegistration {
	t.Helper()
	for _, reg := range regs {
		if reg.Definition.Name == name {
			return reg
		}
	}
	t.Fatalf("registration %s not found", name)
	return SubagentRegistration{}
}

func hasError(errs []error, substr string) bool {
	for _, err := range errs {
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), substr) {
			return true
		}
	}
	return false
}
