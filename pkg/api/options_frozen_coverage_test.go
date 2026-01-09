package api

import (
	"testing"

	corehooks "github.com/cexll/agentsdk-go/pkg/core/hooks"
	coremw "github.com/cexll/agentsdk-go/pkg/core/middleware"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/runtime/commands"
	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
	"github.com/cexll/agentsdk-go/pkg/runtime/subagents"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

func TestOptionsFrozenClonesCollections(t *testing.T) {
	matcher := skills.MatcherFunc(func(skills.ActivationContext) skills.MatchResult { return skills.MatchResult{Score: 1} })
	skillDef := skills.Definition{
		Name:     "skill",
		Metadata: map[string]string{"k": "v"},
		Matchers: []skills.Matcher{matcher},
	}
	subDef := subagents.Definition{
		Name:        "sub",
		BaseContext: subagents.Context{SessionID: "sess", Metadata: map[string]any{"k": "v"}, ToolWhitelist: []string{"Bash"}},
		Matchers:    []skills.Matcher{matcher},
	}

	opts := Options{
		Mode:       ModeContext{EntryPoint: EntryPointCLI, CLI: &CLIContext{Args: []string{"--x"}, Flags: map[string]string{"k": "v"}}},
		Middleware: []middleware.Middleware{nil},
		Tools:      []tool.Tool{&stubTool{name: "legacy"}},
		EnabledBuiltinTools: []string{
			"bash",
		},
		DisallowedTools: []string{"grep"},
		CustomTools:     []tool.Tool{&stubTool{name: "custom"}},
		MCPServers:      []string{"mcp://server"},
		TypedHooks:      []corehooks.ShellHook{{Env: map[string]string{"A": "B"}}},
		HookMiddleware:  []coremw.Middleware{nil},
		Skills:          []SkillRegistration{{Definition: skillDef}},
		Commands:        []CommandRegistration{{Definition: commands.Definition{Name: "cmd"}}},
		Subagents:       []SubagentRegistration{{Definition: subDef}},
		ModelPool:       map[ModelTier]model.Model{ModelTierLow: &stubModel{}},
		SubagentModelMapping: map[string]ModelTier{
			"sub": ModelTierLow,
		},
	}

	frozen := opts.frozen()

	opts.EnabledBuiltinTools[0] = "changed"
	opts.DisallowedTools[0] = "changed"
	opts.MCPServers[0] = "changed"
	opts.Mode.CLI.Args[0] = "--changed"
	opts.Mode.CLI.Flags["k"] = "changed"

	if len(frozen.EnabledBuiltinTools) != 1 || frozen.EnabledBuiltinTools[0] != "bash" {
		t.Fatalf("EnabledBuiltinTools=%v, want [bash]", frozen.EnabledBuiltinTools)
	}
	if len(frozen.DisallowedTools) != 1 || frozen.DisallowedTools[0] != "grep" {
		t.Fatalf("DisallowedTools=%v, want [grep]", frozen.DisallowedTools)
	}
	if len(frozen.MCPServers) != 1 || frozen.MCPServers[0] != "mcp://server" {
		t.Fatalf("MCPServers=%v, want [mcp://server]", frozen.MCPServers)
	}
	if frozen.Mode.CLI == nil || len(frozen.Mode.CLI.Args) != 1 || frozen.Mode.CLI.Args[0] != "--x" {
		t.Fatalf("Mode.CLI.Args=%v, want [--x]", frozen.Mode.CLI)
	}
	if frozen.Mode.CLI.Flags["k"] != "v" {
		t.Fatalf("Mode.CLI.Flags=%v, want map[k:v]", frozen.Mode.CLI.Flags)
	}

	opts.Skills[0].Definition.Metadata["k"] = "changed"
	if frozen.Skills[0].Definition.Metadata["k"] != "v" {
		t.Fatalf("Skills.Metadata=%v, want map[k:v]", frozen.Skills[0].Definition.Metadata)
	}

	opts.Subagents[0].Definition.BaseContext.Metadata["k"] = "changed"
	if frozen.Subagents[0].Definition.BaseContext.Metadata["k"] != "v" {
		t.Fatalf("Subagents.BaseContext.Metadata=%v, want map[k:v]", frozen.Subagents[0].Definition.BaseContext.Metadata)
	}
}
