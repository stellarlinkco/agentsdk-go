package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	hooks "github.com/stellarlinkco/agentsdk-go/pkg/hooks"
	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/subagents"
	"github.com/stellarlinkco/agentsdk-go/pkg/sandbox"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

var (
	ErrMissingModel        = errors.New("api: model factory is required")
	ErrConcurrentExecution = errors.New("concurrent execution on same session is not allowed")
	ErrRuntimeClosed       = errors.New("api: runtime is closed")
	ErrMaxIterations       = errors.New("max iterations reached")
	ErrToolUseDenied       = errors.New("api: tool use denied by hook")
)

var resolveProjectRoot = ResolveProjectRoot

var filepathAbs = filepath.Abs

type EntryPoint string

const (
	EntryPointCLI      EntryPoint = "cli"
	EntryPointCI       EntryPoint = "ci"
	EntryPointPlatform EntryPoint = "platform"
	defaultEntrypoint             = EntryPointCLI
	defaultMaxSessions            = 1000
)

type ModelTier string

const (
	ModelTierLow  ModelTier = "low"  // Low cost: Haiku
	ModelTierMid  ModelTier = "mid"  // Mid cost: Sonnet
	ModelTierHigh ModelTier = "high" // High cost: Opus
)

type ModeContext struct {
	EntryPoint EntryPoint
}

type SandboxOptions struct {
	Root          string
	AllowedPaths  []string
	NetworkAllow  []string
	ResourceLimit sandbox.ResourceLimits
}

type SkillRegistration struct {
	Definition skills.Definition
	Handler    skills.Handler
}

type SubagentRegistration struct {
	Definition subagents.Definition
	Handler    subagents.Handler
}

type ModelFactory interface {
	Model(ctx context.Context) (model.Model, error)
}

type ModelFactoryFunc func(context.Context) (model.Model, error)

func (fn ModelFactoryFunc) Model(ctx context.Context) (model.Model, error) {
	if fn == nil {
		return nil, ErrMissingModel
	}
	return fn(ctx)
}

// Options configures the unified SDK runtime.
type Options struct {
	EntryPoint        EntryPoint
	Mode              ModeContext
	ProjectRoot       string
	EmbedFS           fs.FS
	SettingsPath      string
	SettingsOverrides *config.Settings
	SettingsLoader    *config.SettingsLoader

	Model        model.Model
	ModelFactory ModelFactory

	ModelPool            map[ModelTier]model.Model
	SubagentModelMapping map[string]ModelTier

	DefaultEnableCache bool

	SystemPrompt string
	RulesEnabled *bool // nil = 默认启用，false = 禁用

	Middleware          []middleware.Middleware
	MiddlewareTimeout   time.Duration
	MaxIterations       int
	Timeout             time.Duration
	TokenLimit          int
	MaxSessions         int
	Tools               []tool.Tool
	EnabledBuiltinTools []string
	DisallowedTools     []string
	CustomTools         []tool.Tool
	MCPServers          []string

	TypedHooks        []hooks.ShellHook
	HookMiddleware    []hooks.Middleware
	HookTimeout       time.Duration
	DisableSafetyHook bool

	Skills           []SkillRegistration
	Subagents        []SubagentRegistration
	Sandbox          SandboxOptions
	AutoCompact      CompactConfig
	OTEL             OTELConfig
	fsLayer          *config.FS
	settingsSnapshot *config.Settings
	skReg            *skills.Registry
	subMgr           *subagents.Manager
	tracer           Tracer
}

func DefaultSubagentDefinitions() []subagents.Definition {
	return subagents.BuiltinDefinitions()
}

type Request struct {
	Prompt            string
	ContentBlocks     []model.ContentBlock // Multimodal content; when non-empty, used alongside Prompt
	Mode              ModeContext
	SessionID         string
	RequestID         string    `json:"request_id,omitempty"` // Auto-generated UUID or user-provided
	Model             ModelTier // Optional: override model tier for this request
	EnablePromptCache *bool     // Optional: enable prompt caching (nil uses global default)
	Traits            []string
	Tags              map[string]string
	Channels          []string
	Metadata          map[string]any
	TargetSubagent    string
	ToolWhitelist     []string
	ForceSkills       []string
}

type Response struct {
	Mode            ModeContext
	RequestID       string `json:"request_id,omitempty"` // UUID for distributed tracing
	Result          *Result
	SkillResults    []SkillExecution
	Subagent        *subagents.Result
	HookEvents      []hooks.Event
	ProjectConfig   *config.Settings
	Settings        *config.Settings
	SandboxSnapshot SandboxReport
	Tags            map[string]string
}

type Result struct {
	Output     string
	StopReason string
	Usage      model.Usage
	ToolCalls  []model.ToolCall
}

type SkillExecution struct {
	Definition skills.Definition
	Result     skills.Result
	Err        error
}

type SandboxReport struct {
	Roots          []string
	AllowedPaths   []string
	AllowedDomains []string
	ResourceLimits sandbox.ResourceLimits
}

func WithMaxSessions(n int) func(*Options) {
	return func(o *Options) {
		if n > 0 {
			o.MaxSessions = n
		}
	}
}

func WithAutoCompact(config CompactConfig) func(*Options) {
	return func(o *Options) {
		o.AutoCompact = config
	}
}

func WithOTEL(config OTELConfig) func(*Options) {
	return func(o *Options) {
		o.OTEL = config
	}
}

func (o Options) withDefaults() Options {
	if o.EntryPoint == "" {
		o.EntryPoint = defaultEntrypoint
	}
	if o.Mode.EntryPoint == "" {
		o.Mode.EntryPoint = o.EntryPoint
	}

	if o.ProjectRoot == "" || o.ProjectRoot == "." {
		if resolved, err := resolveProjectRoot(); err == nil {
			o.ProjectRoot = resolved
		} else {
			o.ProjectRoot = "."
		}
	}
	o.ProjectRoot = filepath.Clean(o.ProjectRoot)
	if trimmed := strings.TrimSpace(o.SettingsPath); trimmed != "" {
		if abs, err := filepathAbs(trimmed); err == nil {
			o.SettingsPath = abs
		} else {
			o.SettingsPath = trimmed
		}
	}

	if o.Sandbox.Root == "" {
		o.Sandbox.Root = o.ProjectRoot
	}

	if len(o.Sandbox.NetworkAllow) == 0 {
		o.Sandbox.NetworkAllow = defaultNetworkAllowList(o.EntryPoint)
	}

	if o.MaxSessions <= 0 {
		o.MaxSessions = defaultMaxSessions
	}
	return o
}

// frozen returns a defensive copy of Options so callers can safely reuse/mutate
// the original Options struct without racing against a live Runtime.
func (o Options) frozen() Options {
	o.Mode = ModeContext{EntryPoint: o.Mode.EntryPoint}

	if len(o.Middleware) > 0 {
		o.Middleware = append([]middleware.Middleware(nil), o.Middleware...)
	}
	if len(o.Tools) > 0 {
		o.Tools = append([]tool.Tool(nil), o.Tools...)
	}
	if len(o.EnabledBuiltinTools) > 0 {
		o.EnabledBuiltinTools = append([]string(nil), o.EnabledBuiltinTools...)
	}
	if len(o.DisallowedTools) > 0 {
		o.DisallowedTools = append([]string(nil), o.DisallowedTools...)
	}
	if len(o.CustomTools) > 0 {
		o.CustomTools = append([]tool.Tool(nil), o.CustomTools...)
	}
	if len(o.MCPServers) > 0 {
		o.MCPServers = append([]string(nil), o.MCPServers...)
	}
	if len(o.TypedHooks) > 0 {
		hooks := make([]hooks.ShellHook, len(o.TypedHooks))
		for i, hook := range o.TypedHooks {
			hooks[i] = hook
			if len(hook.Env) > 0 {
				hooks[i].Env = maps.Clone(hook.Env)
			}
		}
		o.TypedHooks = hooks
	}
	if len(o.HookMiddleware) > 0 {
		o.HookMiddleware = append([]hooks.Middleware(nil), o.HookMiddleware...)
	}
	if len(o.Skills) > 0 {
		skillsCopy := make([]SkillRegistration, len(o.Skills))
		for i, reg := range o.Skills {
			skillsCopy[i] = reg
			def := reg.Definition
			if len(def.Metadata) > 0 {
				def.Metadata = maps.Clone(def.Metadata)
			}
			if len(def.Matchers) > 0 {
				def.Matchers = append([]skills.Matcher(nil), def.Matchers...)
			}
			skillsCopy[i].Definition = def
		}
		o.Skills = skillsCopy
	}
	if len(o.Subagents) > 0 {
		subCopy := make([]SubagentRegistration, len(o.Subagents))
		for i, reg := range o.Subagents {
			subCopy[i] = reg
			def := reg.Definition
			def.BaseContext = def.BaseContext.Clone()
			if len(def.Matchers) > 0 {
				def.Matchers = append([]skills.Matcher(nil), def.Matchers...)
			}
			subCopy[i].Definition = def
		}
		o.Subagents = subCopy
	}

	o.Sandbox = freezeSandboxOptions(o.Sandbox)

	if len(o.ModelPool) > 0 {
		o.ModelPool = maps.Clone(o.ModelPool)
	}
	if len(o.SubagentModelMapping) > 0 {
		o.SubagentModelMapping = maps.Clone(o.SubagentModelMapping)
	}

	return o
}

func freezeSandboxOptions(in SandboxOptions) SandboxOptions {
	out := in
	if len(in.AllowedPaths) > 0 {
		out.AllowedPaths = append([]string(nil), in.AllowedPaths...)
	}
	if len(in.NetworkAllow) > 0 {
		out.NetworkAllow = append([]string(nil), in.NetworkAllow...)
	}
	return out
}

// defaultNetworkAllowList 默认允许本地网络；访问外网需显式配置
func defaultNetworkAllowList(_ EntryPoint) []string {
	return []string{
		"localhost",
		"127.0.0.1",
		"::1",       // IPv6 localhost
		"0.0.0.0",   // 本机所有接口
		"*.local",   // 本地域名
		"192.168.*", // 私有网段
		"10.*",      // 私有网段
		"172.16.*",  // 私有网段
	}
}

func (o Options) modeContext() ModeContext {
	mode := o.Mode
	if mode.EntryPoint == "" {
		mode.EntryPoint = o.EntryPoint
	}
	if mode.EntryPoint == "" {
		mode.EntryPoint = defaultEntrypoint
	}
	return mode
}

func (r Request) normalized(defaultMode ModeContext, fallbackSession string) Request {
	req := r
	if req.Mode.EntryPoint == "" {
		req.Mode.EntryPoint = defaultMode.EntryPoint
	}
	if req.SessionID == "" {
		req.SessionID = strings.TrimSpace(fallbackSession)
	}
	if req.Tags == nil {
		req.Tags = map[string]string{}
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	if len(req.ToolWhitelist) > 0 {
		req.ToolWhitelist = cloneStrings(req.ToolWhitelist)
	}
	if len(req.ContentBlocks) > 0 {
		req.ContentBlocks = append([]model.ContentBlock(nil), req.ContentBlocks...)
	}
	if len(req.Channels) > 0 {
		req.Channels = cloneStrings(req.Channels)
	}
	if len(req.Traits) > 0 {
		req.Traits = cloneStrings(req.Traits)
	}
	return req
}

func (r Request) activationContext(prompt string) skills.ActivationContext {
	ctx := skills.ActivationContext{Prompt: prompt}
	if len(r.Channels) > 0 {
		ctx.Channels = append([]string(nil), r.Channels...)
	}
	if len(r.Traits) > 0 {
		ctx.Traits = append([]string(nil), r.Traits...)
	}
	if len(r.Tags) > 0 {
		ctx.Tags = maps.Clone(r.Tags)
	}
	if len(r.Metadata) > 0 {
		ctx.Metadata = maps.Clone(r.Metadata)
	}
	return ctx
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	slices.Sort(out)
	return slices.Compact(out)
}

// WithModelPool configures a pool of models indexed by tier.
func WithModelPool(pool map[ModelTier]model.Model) func(*Options) {
	return func(o *Options) {
		if pool != nil {
			o.ModelPool = pool
		}
	}
}

// WithSubagentModelMapping configures subagent-type-to-tier mappings for model selection.
// Keys should be lowercase subagent type names (e.g., "explore", "plan").
func WithSubagentModelMapping(mapping map[string]ModelTier) func(*Options) {
	return func(o *Options) {
		if mapping != nil {
			o.SubagentModelMapping = mapping
		}
	}
}

// HookRecorder mirrors the historical api hook recorder contract.
type HookRecorder interface {
	Record(hooks.Event)
	Drain() []hooks.Event
}

// hookRecorder stores hook events for the response payload.
type hookRecorder struct {
	events []hooks.Event
}

func (r *hookRecorder) Record(evt hooks.Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	r.events = append(r.events, evt)
}

func (r *hookRecorder) Drain() []hooks.Event {
	defer func() { r.events = nil }()
	if len(r.events) == 0 {
		return nil
	}
	return append([]hooks.Event(nil), r.events...)
}

// defaultHookRecorder implements HookRecorder when callers do not provide one.
func defaultHookRecorder() *hookRecorder {
	return &hookRecorder{}
}

// runtimeHookAdapter wraps the hook executor and recorder.
type runtimeHookAdapter struct {
	executor *hooks.Executor
	recorder HookRecorder

	disableSafetyHook bool
}

func (h *runtimeHookAdapter) PreToolUse(ctx context.Context, evt hooks.ToolUsePayload) (map[string]any, error) {
	params := evt.Params
	if h == nil {
		if err := hooks.SafetyCheck(evt.Name, params); err != nil {
			return nil, err
		}
		return params, nil
	}
	if !h.disableSafetyHook {
		if err := hooks.SafetyCheck(evt.Name, params); err != nil {
			return nil, err
		}
	}
	if h.executor == nil {
		return params, nil
	}
	results, err := h.executor.Execute(ctx, hooks.Event{Type: hooks.PreToolUse, Payload: evt})
	if err != nil {
		return nil, err
	}
	h.record(hooks.Event{Type: hooks.PreToolUse, Payload: evt})

	// Print hook stderr output for debugging
	for _, res := range results {
		if res.Stderr != "" {
			fmt.Fprint(os.Stderr, res.Stderr)
		}
	}

	for _, res := range results {
		if res.Output == nil {
			continue
		}
		// Check top-level decision
		if res.Output.Decision == "deny" {
			return nil, fmt.Errorf("%w: %s", ErrToolUseDenied, evt.Name)
		}
		// Check continue=false
		if res.Output.Continue != nil && !*res.Output.Continue {
			return nil, fmt.Errorf("%w: %s", ErrToolUseDenied, evt.Name)
		}
		// Check hookSpecificOutput for PreToolUse
		if hso := res.Output.HookSpecificOutput; hso != nil {
			if hso.UpdatedInput != nil {
				params = hso.UpdatedInput
			}
			if hso.PermissionDecision == "deny" {
				return nil, fmt.Errorf("%w: %s", ErrToolUseDenied, evt.Name)
			}
		}
	}
	return params, nil
}

func (h *runtimeHookAdapter) PostToolUse(ctx context.Context, evt hooks.ToolResultPayload) error {
	if h == nil || h.executor == nil {
		return nil
	}
	results, err := h.executor.Execute(ctx, hooks.Event{Type: hooks.PostToolUse, Payload: evt})
	if err != nil {
		return err
	}
	h.record(hooks.Event{Type: hooks.PostToolUse, Payload: evt})

	// Print hook stderr output for debugging
	for _, res := range results {
		if res.Stderr != "" {
			fmt.Fprint(os.Stderr, res.Stderr)
		}
	}

	// Check if any hook wants to stop
	for _, res := range results {
		if res.Output != nil && res.Output.Continue != nil && !*res.Output.Continue {
			return fmt.Errorf("hooks: PostToolUse hook requested stop: %s", res.Output.StopReason)
		}
	}
	return nil
}

func (h *runtimeHookAdapter) Stop(ctx context.Context, reason string) error {
	if h == nil || h.executor == nil {
		return nil
	}
	payload := hooks.StopPayload{Reason: reason}
	if err := h.executor.Publish(hooks.Event{Type: hooks.Stop, Payload: payload}); err != nil {
		return err
	}
	h.record(hooks.Event{Type: hooks.Stop, Payload: payload})
	return nil
}

func (h *runtimeHookAdapter) SessionStart(ctx context.Context, evt hooks.SessionStartPayload) error {
	if h == nil || h.executor == nil {
		return nil
	}
	if err := h.executor.Publish(hooks.Event{Type: hooks.SessionStart, Payload: evt}); err != nil {
		return err
	}
	h.record(hooks.Event{Type: hooks.SessionStart, Payload: evt})
	return nil
}

func (h *runtimeHookAdapter) SessionEnd(ctx context.Context, evt hooks.SessionEndPayload) error {
	if h == nil || h.executor == nil {
		return nil
	}
	if err := h.executor.Publish(hooks.Event{Type: hooks.SessionEnd, Payload: evt}); err != nil {
		return err
	}
	h.record(hooks.Event{Type: hooks.SessionEnd, Payload: evt})
	return nil
}

func (h *runtimeHookAdapter) SubagentStart(ctx context.Context, evt hooks.SubagentStartPayload) error {
	if h == nil || h.executor == nil {
		return nil
	}
	if err := h.executor.Publish(hooks.Event{Type: hooks.SubagentStart, Payload: evt}); err != nil {
		return err
	}
	h.record(hooks.Event{Type: hooks.SubagentStart, Payload: evt})
	return nil
}

func (h *runtimeHookAdapter) SubagentStop(ctx context.Context, evt hooks.SubagentStopPayload) error {
	if h == nil || h.executor == nil {
		return nil
	}
	if err := h.executor.Publish(hooks.Event{Type: hooks.SubagentStop, Payload: evt}); err != nil {
		return err
	}
	h.record(hooks.Event{Type: hooks.SubagentStop, Payload: evt})
	return nil
}

func (h *runtimeHookAdapter) record(evt hooks.Event) {
	if h == nil || h.recorder == nil {
		return
	}
	h.recorder.Record(evt)
}
