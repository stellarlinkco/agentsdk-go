package api

import (
	"context"
	"errors"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/runtime/commands"
	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
	"github.com/cexll/agentsdk-go/pkg/runtime/subagents"
	"github.com/cexll/agentsdk-go/pkg/sandbox"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

func TestRunStreamProducesEvents(t *testing.T) {
	root := newClaudeProject(t)
	mdl := &stubModel{responses: []*model.Response{{Message: model.Message{Role: "assistant", Content: "stream"}}}}
	rt, err := New(context.Background(), Options{ProjectRoot: root, Model: mdl})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	ch, err := rt.RunStream(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("run stream: %v", err)
	}
	seenDone := false
	for evt := range ch {
		if evt.Type == "done" {
			seenDone = true
		}
	}
	if !seenDone {
		t.Fatal("expected done event")
	}
}

func TestRunStreamRejectsEmptyPrompt(t *testing.T) {
	rt := &Runtime{opts: Options{ProjectRoot: t.TempDir()}, mode: ModeContext{EntryPoint: EntryPointCLI}, histories: newHistoryStore()}
	if _, err := rt.RunStream(context.Background(), Request{Prompt: "   "}); err == nil {
		t.Fatal("expected empty prompt error")
	}
}

func TestExecuteCommandsIgnoresPlainText(t *testing.T) {
	rt := &Runtime{}
	cmds, prompt, err := rt.executeCommands(context.Background(), "free text", &Request{Prompt: "free text"})
	if err != nil {
		t.Fatalf("execute commands: %v", err)
	}
	if len(cmds) != 0 || prompt != "free text" {
		t.Fatalf("unexpected command handling: %v %q", cmds, prompt)
	}
}

func TestExecuteCommandsUnknownCommand(t *testing.T) {
	exec := commands.NewExecutor()
	rt := &Runtime{cmdExec: exec}
	_, _, err := rt.executeCommands(context.Background(), "/unknown", &Request{Prompt: "/unknown"})
	if !errors.Is(err, commands.ErrUnknownCommand) {
		t.Fatalf("expected unknown command error, got %v", err)
	}
}

func TestRegisterHelpersRejectNilHandlers(t *testing.T) {
	if _, err := registerSkills([]SkillRegistration{{Definition: skills.Definition{Name: "x"}}}); err == nil {
		t.Fatal("expected skill error")
	}
	if _, err := registerCommands([]CommandRegistration{{Definition: commands.Definition{Name: "x"}}}); err == nil {
		t.Fatal("expected command error")
	}
	if _, err := registerSubagents([]SubagentRegistration{{Definition: subagents.Definition{Name: "x"}}}); err == nil {
		t.Fatal("expected subagent error")
	}
}

func TestRegisterSubagentsEmpty(t *testing.T) {
	mgr, err := registerSubagents(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr != nil {
		t.Fatalf("expected nil manager, got %#v", mgr)
	}
}

func TestRegisterMCPServersNoop(t *testing.T) {
	registry := tool.NewRegistry()
	mgr := sandbox.NewManager(nil, sandbox.NewDomainAllowList(), nil)
	if err := registerMCPServers(registry, mgr, nil); err != nil {
		t.Fatalf("register MCP servers: %v", err)
	}
}

func TestDefaultSessionIDUsesEntrypoint(t *testing.T) {
	id := defaultSessionID("")
	if len(id) == 0 || id[:3] != string(defaultEntrypoint) {
		t.Fatalf("unexpected default session id: %s", id)
	}
}

func TestToolWhitelistDeniesExecution(t *testing.T) {
	root := newClaudeProject(t)
	mdl := &stubModel{responses: []*model.Response{{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "c1", Name: "echo", Arguments: map[string]any{"text": "hi"}}}}}}}
	rt, err := New(context.Background(), Options{ProjectRoot: root, Model: mdl, Tools: []tool.Tool{&echoTool{}}})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer rt.Close()

	_, err = rt.Run(context.Background(), Request{Prompt: "call", ToolWhitelist: []string{}})
	if err == nil {
		t.Fatal("expected whitelist error")
	}
}

func TestAvailableToolsFiltersWhitelist(t *testing.T) {
	reg := tool.NewRegistry()
	_ = reg.Register(&echoTool{})
	defs := availableTools(reg, map[string]struct{}{"missing": {}})
	if len(defs) != 0 {
		t.Fatalf("expected tools filtered out, got %v", defs)
	}
}

func TestSchemaToMap(t *testing.T) {
	schema := &tool.JSONSchema{Type: "object", Required: []string{"x"}, Properties: map[string]any{"x": map[string]any{"type": "string"}}}
	mapped := schemaToMap(schema)
	if mapped["type"] != "object" || len(mapped["required"].([]string)) != 1 {
		t.Fatalf("unexpected map: %+v", mapped)
	}
}

func TestHistoryStoreCreatesOnce(t *testing.T) {
	store := newHistoryStore()
	a := store.Get("s1")
	b := store.Get("s1")
	if a != b {
		t.Fatal("expected same history instance")
	}
}

func TestExecuteSubagentBranches(t *testing.T) {
	rt := &Runtime{}
	// nil manager returns original prompt
	res, out, err := rt.executeSubagent(context.Background(), "p", skills.ActivationContext{}, &Request{Prompt: "p"})
	if err != nil || res != nil || out != "p" {
		t.Fatalf("unexpected result: res=%v out=%q err=%v", res, out, err)
	}

	// no matching subagent with empty target suppresses error
	rt.subMgr = subagents.NewManager()
	res, out, err = rt.executeSubagent(context.Background(), "p", skills.ActivationContext{}, &Request{Prompt: "p"})
	if err != nil || res != nil || out != "p" {
		t.Fatalf("no-match branch failed: res=%v out=%q err=%v", res, out, err)
	}

	// unknown explicit target returns error
	_, _, err = rt.executeSubagent(context.Background(), "p", skills.ActivationContext{}, &Request{Prompt: "p", TargetSubagent: "missing"})
	if err == nil {
		t.Fatal("expected error for unknown subagent")
	}
}

func TestExecuteSubagentSuccess(t *testing.T) {
	mgr := subagents.NewManager()
	err := mgr.Register(subagents.Definition{Name: "ops"}, subagents.HandlerFunc(func(context.Context, subagents.Context, subagents.Request) (subagents.Result, error) {
		return subagents.Result{
			Output: "new-prompt",
			Metadata: map[string]any{
				"api.prompt_override": "new-prompt",
				"api.tags":            map[string]string{"sub": "1"},
			},
		}, nil
	}))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	rt := &Runtime{subMgr: mgr}
	req := &Request{Prompt: "p"}
	res, out, err := rt.executeSubagent(context.Background(), "p", skills.ActivationContext{}, req)
	if err != nil || res == nil || out != "new-prompt" {
		t.Fatalf("unexpected result: res=%v out=%q err=%v", res, out, err)
	}
	if req.Tags["sub"] != "1" {
		t.Fatalf("expected tags propagated, got %+v", req.Tags)
	}
}

func TestConvertRunResultHelpers(t *testing.T) {
	if got := convertRunResult(runResult{}); got != nil {
		t.Fatalf("expected nil result, got %+v", got)
	}
	out := &agent.ModelOutput{Content: "ok", ToolCalls: []agent.ToolCall{{Name: "t", Input: map[string]any{"x": 1}}}}
	res := convertRunResult(runResult{output: out})
	if res == nil || len(res.ToolCalls) != 1 || res.ToolCalls[0].Arguments["x"].(int) != 1 {
		t.Fatalf("unexpected converted result: %+v", res)
	}
}

func TestNewTrimmerHelper(t *testing.T) {
	rt := &Runtime{opts: Options{TokenLimit: 0}}
	if rt.newTrimmer() != nil {
		t.Fatal("expected nil trimmer when limit is zero")
	}
	rt.opts.TokenLimit = 10
	if rt.newTrimmer() == nil {
		t.Fatal("expected non-nil trimmer")
	}
}

func TestResolveModelPrefersFactory(t *testing.T) {
	mdl := &stubModel{}
	called := false
	resolved, err := resolveModel(context.Background(), Options{ModelFactory: ModelFactoryFunc(func(context.Context) (model.Model, error) {
		called = true
		return mdl, nil
	})})
	if err != nil || resolved != mdl || !called {
		t.Fatalf("resolveModel factory branch failed: m=%v err=%v called=%v", resolved, err, called)
	}
	if _, err := resolveModel(context.Background(), Options{}); !errors.Is(err, ErrMissingModel) {
		t.Fatalf("expected ErrMissingModel, got %v", err)
	}
}
