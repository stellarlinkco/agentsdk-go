package api

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
	"github.com/stellarlinkco/agentsdk-go/pkg/sandbox"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
	toolbuiltin "github.com/stellarlinkco/agentsdk-go/pkg/tool/builtin"
)

func registerTools(registry *tool.Registry, opts Options, settings *config.Settings, skReg *skills.Registry) error {
	entry := effectiveEntryPoint(opts)
	tools := opts.Tools

	if len(tools) == 0 {
		sandboxDisabled := settings != nil && settings.Sandbox != nil && settings.Sandbox.Enabled != nil && !*settings.Sandbox.Enabled
		if skReg == nil {
			skReg = skills.NewRegistry()
		}

		factories := builtinToolFactories(opts.ProjectRoot, sandboxDisabled, entry, settings, skReg)
		names := builtinOrder(entry)
		selectedNames := filterBuiltinNames(opts.EnabledBuiltinTools, names)
		for _, name := range selectedNames {
			ctor := factories[name]
			if ctor == nil {
				continue
			}
			impl := ctor()
			if impl == nil {
				continue
			}
			tools = append(tools, impl)
		}

		if len(opts.CustomTools) > 0 {
			tools = append(tools, opts.CustomTools...)
		}
	}

	disallowed := toLowerSet(opts.DisallowedTools)
	if settings != nil && len(settings.DisallowedTools) > 0 {
		if disallowed == nil {
			disallowed = map[string]struct{}{}
		}
		for _, name := range settings.DisallowedTools {
			if key := canonicalToolName(name); key != "" {
				disallowed[key] = struct{}{}
			}
		}
		if len(disallowed) == 0 {
			disallowed = nil
		}
	}

	seen := make(map[string]struct{})
	for _, impl := range tools {
		if impl == nil {
			continue
		}
		name := strings.TrimSpace(impl.Name())
		if name == "" {
			continue
		}
		canon := canonicalToolName(name)
		if disallowed != nil {
			if _, blocked := disallowed[canon]; blocked {
				log.Printf("tool %s skipped: disallowed", name)
				continue
			}
		}
		if _, ok := seen[canon]; ok {
			log.Printf("tool %s skipped: duplicate name", name)
			continue
		}
		seen[canon] = struct{}{}
		if err := registry.Register(impl); err != nil {
			return fmt.Errorf("api: register tool %s: %w", impl.Name(), err)
		}
	}

	return nil
}

func builtinToolFactories(root string, sandboxDisabled bool, entry EntryPoint, settings *config.Settings, skReg *skills.Registry) map[string]func() tool.Tool {
	factories := map[string]func() tool.Tool{}

	bashCtor := func() tool.Tool {
		var bash *toolbuiltin.BashTool
		if sandboxDisabled {
			bash = toolbuiltin.NewBashToolWithSandbox(root, nil)
		} else {
			bash = toolbuiltin.NewBashToolWithRoot(root)
		}
		if entry == EntryPointCLI {
			bash.AllowShellMetachars(true)
		}
		return bash
	}

	readCtor := func() tool.Tool {
		if sandboxDisabled {
			return toolbuiltin.NewReadToolWithSandbox(root, nil)
		}
		return toolbuiltin.NewReadToolWithRoot(root)
	}
	writeCtor := func() tool.Tool {
		if sandboxDisabled {
			return toolbuiltin.NewWriteToolWithSandbox(root, nil)
		}
		return toolbuiltin.NewWriteToolWithRoot(root)
	}
	editCtor := func() tool.Tool {
		if sandboxDisabled {
			return toolbuiltin.NewEditToolWithSandbox(root, nil)
		}
		return toolbuiltin.NewEditToolWithRoot(root)
	}

	respectGitignore := true
	if settings != nil && settings.RespectGitignore != nil {
		respectGitignore = *settings.RespectGitignore
	}
	grepCtor := func() tool.Tool {
		if sandboxDisabled {
			grep := toolbuiltin.NewGrepToolWithSandbox(root, nil)
			grep.SetRespectGitignore(respectGitignore)
			return grep
		}
		grep := toolbuiltin.NewGrepToolWithRoot(root)
		grep.SetRespectGitignore(respectGitignore)
		return grep
	}
	globCtor := func() tool.Tool {
		if sandboxDisabled {
			glob := toolbuiltin.NewGlobToolWithSandbox(root, nil)
			glob.SetRespectGitignore(respectGitignore)
			return glob
		}
		glob := toolbuiltin.NewGlobToolWithRoot(root)
		glob.SetRespectGitignore(respectGitignore)
		return glob
	}
	factories["bash"] = bashCtor
	factories["read"] = readCtor
	factories["write"] = writeCtor
	factories["edit"] = editCtor
	factories["grep"] = grepCtor
	factories["glob"] = globCtor
	factories["skill"] = func() tool.Tool { return toolbuiltin.NewSkillTool(skReg, nil) }

	return factories
}

func builtinOrder(entry EntryPoint) []string {
	_ = entry
	return []string{"bash", "read", "write", "edit", "glob", "grep", "skill"}
}

func filterBuiltinNames(enabled []string, order []string) []string {
	if enabled == nil {
		return append([]string(nil), order...)
	}
	if len(enabled) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(enabled))
	repl := strings.NewReplacer("-", "_", " ", "_")
	for _, name := range enabled {
		key := strings.ToLower(strings.TrimSpace(name))
		key = repl.Replace(key)
		if key != "" {
			set[key] = struct{}{}
		}
	}
	var filtered []string
	for _, name := range order {
		if _, ok := set[name]; ok {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func effectiveEntryPoint(opts Options) EntryPoint {
	entry := opts.EntryPoint
	if entry == "" {
		entry = opts.Mode.EntryPoint
	}
	if entry == "" {
		entry = defaultEntrypoint
	}
	return entry
}

func registerMCPServers(ctx context.Context, registry *tool.Registry, manager *sandbox.Manager, servers []mcpServer) error {
	for _, server := range servers {
		spec := server.Spec
		if err := enforceSandboxHost(manager, spec); err != nil {
			return err
		}
		opts := tool.MCPServerOptions{
			Headers:       server.Headers,
			Env:           server.Env,
			EnabledTools:  server.EnabledTools,
			DisabledTools: server.DisabledTools,
		}
		if server.TimeoutSeconds > 0 {
			opts.Timeout = time.Duration(server.TimeoutSeconds) * time.Second
		}
		if server.ToolTimeoutSeconds > 0 {
			opts.ToolTimeout = time.Duration(server.ToolTimeoutSeconds) * time.Second
		}

		var err error
		if !hasMCPServerOptions(opts) {
			err = registry.RegisterMCPServer(ctx, spec, server.Name)
		} else {
			err = registry.RegisterMCPServerWithOptions(ctx, spec, server.Name, opts)
		}
		if err != nil {
			return fmt.Errorf("api: register MCP %s: %w", spec, err)
		}
	}
	return nil
}

func hasMCPServerOptions(opts tool.MCPServerOptions) bool {
	return len(opts.Headers) > 0 ||
		len(opts.Env) > 0 ||
		opts.Timeout > 0 ||
		len(opts.EnabledTools) > 0 ||
		len(opts.DisabledTools) > 0 ||
		opts.ToolTimeout > 0
}

func enforceSandboxHost(manager *sandbox.Manager, server string) error {
	if manager == nil || strings.TrimSpace(server) == "" {
		return nil
	}
	u, err := url.Parse(server)
	if err != nil || u == nil || strings.TrimSpace(u.Scheme) == "" {
		return nil
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	base, _, _ := strings.Cut(scheme, "+")
	switch base {
	case "http", "https", "sse":
		if err := manager.CheckNetwork(u.Host); err != nil {
			return fmt.Errorf("api: MCP host denied: %w", err)
		}
	}
	return nil
}

func resolveModel(ctx context.Context, opts Options) (model.Model, error) {
	if opts.Model != nil {
		return opts.Model, nil
	}
	if opts.ModelFactory != nil {
		mdl, err := opts.ModelFactory.Model(ctx)
		if err != nil {
			return nil, fmt.Errorf("api: model factory: %w", err)
		}
		return mdl, nil
	}
	return nil, ErrMissingModel
}

func defaultSessionID(entry EntryPoint) string {
	prefix := strings.TrimSpace(string(entry))
	if prefix == "" {
		prefix = string(defaultEntrypoint)
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
