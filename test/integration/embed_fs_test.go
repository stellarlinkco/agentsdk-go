//go:build integration
// +build integration

package integration

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
)

//go:embed testdata/.agents/**
var testAgentsFS embed.FS

const (
	testSkillName = "test-skill"
	testAgentName = "test-agent"
)

func TestEmbedFS_PureEmbedded(t *testing.T) {
	t.Parallel()
	projectRoot := t.TempDir()
	if _, err := os.Stat(filepath.Join(projectRoot, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("expected no .agents directory on disk, got err=%v", err)
	}

	rt, _ := newTestRuntime(t, projectRoot, embeddedAgentsFS(t))
	resp := runRequest(t, rt, api.Request{
		Prompt:      "diagnose embedded fs",
		SessionID:   t.Name(),
		ForceSkills: []string{testSkillName},
	})

	if resp.Settings == nil {
		t.Fatalf("expected settings in response")
	}
	if got := resp.Settings.Env["SOURCE"]; got != "embed" {
		t.Fatalf("expected embedded SOURCE env, got %q", got)
	}
	if got := resp.Settings.Env["EMBED_ONLY"]; got != "1" {
		t.Fatalf("expected embedded env fallback, got %q", got)
	}

	if len(resp.SkillResults) != 1 {
		t.Fatalf("expected 1 skill result, got %d", len(resp.SkillResults))
	}
	if resp.SkillResults[0].Definition.Name != testSkillName {
		t.Fatalf("loaded skill %q, want %s", resp.SkillResults[0].Definition.Name, testSkillName)
	}
}

func TestEmbedFS_OSOverridesEmbedded(t *testing.T) {
	t.Parallel()
	projectRoot := t.TempDir()
	writeProjectFile(t, projectRoot, ".agents/settings.local.json", `{
  "env": {
    "SOURCE": "local",
    "LOCAL_ONLY": "1"
  }
}`)

	rt, _ := newTestRuntime(t, projectRoot, embeddedAgentsFS(t))
	resp := runRequest(t, rt, api.Request{
		Prompt:      "os override",
		SessionID:   t.Name(),
		ForceSkills: []string{testSkillName},
	})

	env := resp.Settings.Env
	if env["SOURCE"] != "local" {
		t.Fatalf("expected local SOURCE override, got %q", env["SOURCE"])
	}
	if env["LOCAL_ONLY"] != "1" {
		t.Fatalf("expected LOCAL_ONLY env, got %q", env["LOCAL_ONLY"])
	}
	if env["EMBED_ONLY"] != "1" {
		t.Fatalf("expected embedded env keys to remain, got %q", env["EMBED_ONLY"])
	}
}

func TestEmbedFS_MixedSkills(t *testing.T) {
	t.Parallel()
	projectRoot := t.TempDir()
	writeProjectFile(t, projectRoot, ".agents/skills/test-skill/SKILL.md", `---
name: test-skill
description: local override skill
allowed-tools: bash
---
# Local Skill

Only the local version should run.
`)

	rt, _ := newTestRuntime(t, projectRoot, embeddedAgentsFS(t))
	resp := runRequest(t, rt, api.Request{
		Prompt:      "mixed skills",
		SessionID:   t.Name(),
		ForceSkills: []string{testSkillName},
	})

	if len(resp.SkillResults) != 1 {
		t.Fatalf("expected 1 skill result, got %d", len(resp.SkillResults))
	}
	result := resp.SkillResults[0]
	if result.Definition.Description != "local override skill" {
		t.Fatalf("expected local description, got %q", result.Definition.Description)
	}
	body := skillBody(t, result.Result.Output)
	if !strings.Contains(body, "Only the local version") {
		t.Fatalf("skill body not loaded from local override: %q", body)
	}
	source, _ := result.Result.Metadata["source"].(string)
	if source == "" || !strings.Contains(source, filepath.Join(projectRoot, ".agents", "skills")) {
		t.Fatalf("skill source should point to project override, got %q", source)
	}
}

func TestEmbedFS_Priority(t *testing.T) {
	t.Parallel()
	projectRoot := t.TempDir()

	embedMap := fstest.MapFS{
		".agents":                   &fstest.MapFile{Mode: fs.ModeDir},
		".agents/settings.json":     &fstest.MapFile{Data: []byte(`{"env":{"SOURCE":"embed","EMBED_ONLY":"1"}}`)},
		".agents/skills":            &fstest.MapFile{Mode: fs.ModeDir},
		".agents/skills/test-skill": &fstest.MapFile{Mode: fs.ModeDir},
		".agents/skills/test-skill/SKILL.md": &fstest.MapFile{Data: []byte(`---
name: test-skill
description: embed version
---
Embedded priority skill
`)},
	}

	writeProjectFile(t, projectRoot, ".agents/settings.json", `{
  "env": {
    "SOURCE": "priority-local",
    "LOCAL_ONLY": "1"
  }
}`)

	writeProjectFile(t, projectRoot, ".agents/skills/test-skill/SKILL.md", `---
name: test-skill
description: highest priority
---
Local priority skill wins
`)

	rt, _ := newTestRuntime(t, projectRoot, embedMap)
	resp := runRequest(t, rt, api.Request{
		Prompt:      "priority rules",
		SessionID:   t.Name(),
		ForceSkills: []string{testSkillName},
	})

	env := resp.Settings.Env
	if env["SOURCE"] != "priority-local" {
		t.Fatalf("expected OS settings.json to override embed, got %q", env["SOURCE"])
	}
	if env["LOCAL_ONLY"] != "1" {
		t.Fatalf("expected LOCAL_ONLY env, got %q", env["LOCAL_ONLY"])
	}
	if _, ok := env["EMBED_ONLY"]; ok {
		t.Fatalf("embed env should not leak when OS version exists")
	}

	if len(resp.SkillResults) != 1 {
		t.Fatalf("expected 1 skill result, got %d", len(resp.SkillResults))
	}
	if resp.SkillResults[0].Definition.Description != "highest priority" {
		t.Fatalf("expected OS skill description, got %q", resp.SkillResults[0].Definition.Description)
	}
}

func TestEmbedFS_NilEmbedFS(t *testing.T) {
	t.Parallel()
	projectRoot := t.TempDir()
	writeProjectFile(t, projectRoot, ".agents/settings.json", `{"env":{"SOURCE":"disk-only"}}`)
	writeProjectFile(t, projectRoot, ".agents/skills/test-skill/SKILL.md", `---
name: test-skill
description: disk skill
---
Disk only skill
`)

	rt, _ := newTestRuntime(t, projectRoot, nil)
	resp := runRequest(t, rt, api.Request{
		Prompt:      "nil embed",
		SessionID:   t.Name(),
		ForceSkills: []string{testSkillName},
	})

	if resp.Settings.Env["SOURCE"] != "disk-only" {
		t.Fatalf("expected disk settings to load without embed: %q", resp.Settings.Env["SOURCE"])
	}
	if len(resp.SkillResults) != 1 {
		t.Fatalf("expected disk skill to execute, got %d", len(resp.SkillResults))
	}
}

func newTestRuntime(t *testing.T, projectRoot string, embed fs.FS) (*api.Runtime, *recordingModel) {
	t.Helper()
	model := newRecordingModel()
	rt, err := api.New(context.Background(), api.Options{
		ProjectRoot: projectRoot,
		EmbedFS:     embed,
		Model:       model,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		rt.Close()
	})
	return rt, model
}

func embeddedAgentsFS(t *testing.T) fs.FS {
	t.Helper()
	fsys, err := fs.Sub(testAgentsFS, "testdata")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	return fsys
}

func runRequest(t *testing.T, rt *api.Runtime, req api.Request) *api.Response {
	t.Helper()
	if strings.TrimSpace(req.SessionID) == "" {
		req.SessionID = fmt.Sprintf("%s-session", t.Name())
	}
	resp, err := rt.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	return resp
}

func writeProjectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func skillBody(t *testing.T, output any) string {
	t.Helper()
	payload, ok := output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected skill output type %T", output)
	}
	body, _ := payload["body"].(string)
	return strings.TrimSpace(body)
}

type recordingModel struct {
	mu       sync.Mutex
	lastReq  model.Request
	response string
}

func newRecordingModel() *recordingModel {
	return &recordingModel{response: "ok"}
}

func (m *recordingModel) Complete(_ context.Context, req model.Request) (*model.Response, error) {
	m.mu.Lock()
	m.lastReq = req
	m.mu.Unlock()
	return &model.Response{
		Message: model.Message{Role: "assistant", Content: m.response},
		Usage:   model.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	}, nil
}

func (m *recordingModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	if cb != nil {
		return cb(model.StreamResult{Final: true, Response: resp})
	}
	return nil
}

func (m *recordingModel) LastRequest() model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastReq
}

func TestEmbedFS_Subagents(t *testing.T) {
	t.Parallel()
	projectRoot := t.TempDir()

	rt, _ := newTestRuntime(t, projectRoot, embeddedAgentsFS(t))
	resp := runRequest(t, rt, api.Request{
		Prompt:         "test",
		SessionID:      t.Name(),
		TargetSubagent: testAgentName,
	})

	if resp.Subagent == nil {
		t.Fatalf("expected subagent result")
	}
	if resp.Subagent.Subagent != testAgentName {
		t.Fatalf("loaded subagent %q, want %s", resp.Subagent.Subagent, testAgentName)
	}
}
