# Architecture Document: agentsdk-go v2 Simplification

**Version**: 1.0
**Date**: 2026-03-17
**Author**: System Architect
**Quality Score**: 94/100
**PRD Reference**: `docs/refactor/PRD.md`
**Status**: Final

## Quick Reference (Agent Context)

> **Architecture Style**: Modular monolith (single Go module, layered packages)
> **Primary Stack**: Go 1.24+ / anthropic-sdk-go / openai-go / MCP go-sdk / OTel
> **Dependency Direction**: `api -> {model, tool, middleware, hooks, config, message, sandbox, mcp, runtime/*}`, never reverse
> **Naming Rule**: Go standard: packages lowercase, types PascalCase, files snake_case, constants PascalCase
> **API Format**: Go function calls (not HTTP REST -- this is an SDK)
> **Error Pattern**: Sentinel errors at package level (`var ErrXxx = errors.New(...)`), wrap with `fmt.Errorf("pkg: action: %w", err)`
> **Project Root**: `pkg/` with ~11 domain-based packages
> **Key Constraint**: Zero new dependencies; <=20K non-test LOC; <=11 packages; single Model interface

---

## Executive Summary

agentsdk-go v2 is a big-bang rewrite that reduces the SDK from ~34K non-test lines across 24+ packages to ~15-20K lines across ~11 packages. The rewrite eliminates the dual Model interface, simplifies compaction into a prompt-compression step that strips tool I/O, consolidates event types from 16 to 7, middleware stages from 6 to 4, and built-in tools from 11 to 7.

The agent core loop (~189 lines in v1) is absorbed into `pkg/api`, calling `model.Model.CompleteStream` directly without a bridge adapter. The Runtime struct drops from 17+ fields to ~7 essential fields. ACP and task management are removed from core.

All architectural decisions trace directly to the PRD's KISS/YAGNI philosophy: every line must carry its weight.

---

## Architecture Overview

### Architecture Principles

1. **Single Interface, Zero Adapters**: One `model.Model` interface with two methods. No bridge structs, no glue code. Rationale: the dual interface added ~200 lines of adapter code that obscured data flow (PRD G1).
2. **Compaction Is a Controlled Model Call**: When compaction triggers, core performs a dedicated prompt-compression model call and strips tool I/O from the compressed portion. Rationale: tool output is high-token noise; compression preserves intent while keeping the preserved tail unmodified (PRD G3).
3. **Minimal Surface Area**: 7 events, 4 middleware stages, 7 tools. Features outside this core set are deleted from v2 core and can be reintroduced by users as custom code. Rationale: every extension point is maintenance surface; fewer points means less drift (PRD G5).
4. **YOLO Default, Safety Hook**: All tool executions are allowed by default. A Go-native safety hook blocks catastrophic commands. Users opt-in to stricter models. Rationale: the permission system added complexity for a problem most SDK users solve differently in their own infrastructure (PRD FR-9).

### Package Dependency Graph

```
                    ┌──────────────────────────────────────────┐
                    │                 pkg/api                  │
                    │  (Runtime, agent loop, system prompt,    │
                    │   compaction, request orchestration)     │
                    └──┬───┬───┬───┬───┬───┬───┬───┬───┬──┬──┘
                       │   │   │   │   │   │   │   │   │  │
          ┌────────────┘   │   │   │   │   │   │   │   │  └──────────┐
          ▼                ▼   │   ▼   │   ▼   │   ▼   ▼             ▼
     ┌─────────┐   ┌──────────┤  ┌────┤  ┌────┤  ┌─────────┐  ┌──────────┐
     │pkg/model│   │pkg/tool  │  │pkg/│  │pkg/│  │pkg/hooks │  │pkg/config│
     │         │   │          │  │mid │  │msg │  │          │  │          │
     │ Model   │   │ Registry │  │dle │  │    │  │ Executor │  │ Settings │
     │ interf. │   │ Executor │  │ware│  │    │  │ Events   │  │ Loader   │
     └─────────┘   │ builtin/ │  │    │  │    │  │          │  │ Rules    │
                   └──────────┘  └────┘  └────┘  └──────────┘  └──────────┘
                        │                                           │
                   ┌────┴────┐                               ┌─────┴─────┐
                   ▼         ▼                               ▼           ▼
              ┌────────┐ ┌──────────┐                   ┌─────────┐ ┌────────┐
              │pkg/mcp │ │pkg/sand- │                   │pkg/run- │ │pkg/git-│
              │        │ │box       │                   │time/    │ │ignore  │
              │ Client │ │ Manager  │                   │ skills/ │ │        │
              └────────┘ └──────────┘                   │ subag./ │ └────────┘
                                                        │ cmds/   │
                                                        └─────────┘

     (No contrib/ in v2 core.)
```

**Dependency Rules (strict, never reverse):**
- `pkg/api` depends on all other `pkg/*` packages.
- `pkg/tool/builtin` depends on `pkg/tool`, `pkg/sandbox`, `pkg/gitignore`.
- `pkg/hooks` depends on nothing in `pkg/` except its own event types.
- `pkg/model` depends on nothing in `pkg/`.
- `pkg/message` depends on nothing in `pkg/`.
- `pkg/middleware` depends on `pkg/runtime/skills` (for trace middleware skill logging only).
- `pkg/config` depends on nothing in `pkg/`.
- `pkg/mcp` depends on nothing in `pkg/`.
- `pkg/sandbox` depends on nothing in `pkg/`.
- `pkg/gitignore` depends on nothing in `pkg/`.

### Target Package Inventory (~11 packages)

| # | Package | Responsibility | Absorbed From |
|---|---------|---------------|---------------|
| 1 | `pkg/api` | Runtime struct, agent loop, compaction, system prompt, request orchestration | `pkg/agent` (233 lines), system prompt parts of `pkg/prompts` |
| 2 | `pkg/model` | Model interface, Anthropic provider, OpenAI provider, types | Stays |
| 3 | `pkg/tool` | Tool interface, Registry, Executor, Validator | Stays |
| 4 | `pkg/tool/builtin` | 7 built-in tools + helpers | Drops task/slash/askuser/todo/web/tools |
| 5 | `pkg/middleware` | Middleware interface (4 stages), Chain, Trace, HTTP trace | Absorbs `pkg/core/middleware` (24 lines) |
| 6 | `pkg/hooks` | Hook Executor, Event types, Event payloads | Merges `pkg/core/events` + `pkg/core/hooks` |
| 7 | `pkg/config` | Settings types, SettingsLoader, RulesLoader, hot-reload | Stays |
| 8 | `pkg/message` | In-memory History, Message types, token counter | Stays (as-is per PRD) |
| 9 | `pkg/sandbox` | Filesystem/network isolation manager | Stays |
| 10 | `pkg/mcp` | MCP client session management | Stays |
| 11 | `pkg/gitignore` | .gitignore pattern matcher | Stays (as-is per PRD) |
| 12 | `pkg/runtime/skills` | Skills registry, loader, matcher, prompt templates | Absorbs skill-related templates from `pkg/prompts` |
| 13 | `pkg/runtime/subagents` | Subagent manager, definitions, dispatch | Stays |
**Note on count**: `pkg/runtime/*` sub-packages (`skills`, `subagents`) are counted as subdirectories under `pkg/runtime`. `pkg/runtime/commands` is **deleted** (slash commands removed per PRD). `pkg/runtime/tasks` is **deleted** (tasks removed). `pkg/security` is **absorbed into `pkg/hooks/safety.go`** (safety hook replaces the full permission system). The `runtime/` directory is a logical group, not counted as a separate package.

**Final directory count**: `api`, `model`, `tool`, `tool/builtin`, `middleware`, `hooks`, `config`, `message`, `sandbox`, `mcp`, `gitignore`, `runtime/skills`, `runtime/subagents` = 13 directories. To meet <=11, flatten `runtime/skills` -> `pkg/skills`, `runtime/subagents` -> `pkg/subagents` (removing the `runtime/` container).

---

## Core Interfaces

### 1. model.Model (Single Interface)

**Location**: `pkg/model/interface.go`

```go
// Model is the provider-agnostic interface for LLM completion.
// Both Anthropic and OpenAI providers implement this directly.
// The agent loop calls CompleteStream for streaming, Complete for blocking.
type Model interface {
    Complete(ctx context.Context, req Request) (*Response, error)
    CompleteStream(ctx context.Context, req Request, cb StreamHandler) error
}
```

**What changes from v1**:
- `agent.Model` (with `Generate(ctx, *Context) (*ModelOutput, error)`) is **deleted**.
- The `conversationModel` bridge adapter in `pkg/api/agent.go` (lines 993-1081 in v1) is **deleted**.
- The agent loop in `pkg/api` calls `model.CompleteStream` directly, managing conversation history and tool call extraction inline.

**Why**: The bridge adapter converted between two representations of the same data (messages + tool calls). This is a pure translation layer with zero business logic. Removing it eliminates ~200 lines and one entire package (`pkg/agent`).

### 2. tool.Tool

**Location**: `pkg/tool/tool.go` (unchanged)

```go
type Tool interface {
    Name() string
    Description() string
    Schema() *JSONSchema
    Execute(ctx context.Context, params map[string]any) (*ToolResult, error)
}
```

### 3. middleware.Middleware (4 stages)

**Location**: `pkg/middleware/types.go`

```go
type Stage int

const (
    StageBeforeAgent Stage = iota
    StageBeforeTool
    StageAfterTool
    StageAfterAgent
)

type Middleware interface {
    Name() string
    BeforeAgent(ctx context.Context, st *State) error
    BeforeTool(ctx context.Context, st *State) error
    AfterTool(ctx context.Context, st *State) error
    AfterAgent(ctx context.Context, st *State) error
}

// Funcs adapts function pointers to Middleware.
type Funcs struct {
    Identifier    string
    OnBeforeAgent func(ctx context.Context, st *State) error
    OnBeforeTool  func(ctx context.Context, st *State) error
    OnAfterTool   func(ctx context.Context, st *State) error
    OnAfterAgent  func(ctx context.Context, st *State) error
}
```

**What changes from v1**:
- `StageBeforeModel` and `StageAfterModel` are **removed**.
- `BeforeModel()` and `AfterModel()` methods are **removed** from the interface.
- Model-level interception is unnecessary -- the model call is a deterministic function of messages + tools. Observability of model calls is achieved via `BeforeAgent` (sees the request) and `AfterAgent` (sees the response including model usage).

**Why**: In v1, `BeforeModel`/`AfterModel` were used by trace middleware to observe model requests/responses. This can be done more naturally at the agent level: `BeforeAgent` fires once per request and `AfterAgent` fires once per response, giving the trace middleware a cleaner signal. Two fewer methods on the interface means less boilerplate for every middleware implementation.

### 4. hooks.EventType (7 events)

**Location**: `pkg/hooks/types.go`

```go
type EventType string

const (
    PreToolUse    EventType = "PreToolUse"
    PostToolUse   EventType = "PostToolUse"
    SessionStart  EventType = "SessionStart"
    SessionEnd    EventType = "SessionEnd"
    Stop          EventType = "Stop"
    SubagentStart EventType = "SubagentStart"
    SubagentStop  EventType = "SubagentStop"
)
```

**Deleted events** (9 total): `PostToolUseFailure`, `PreCompact`, `ContextCompacted`, `UserPromptSubmit`, `PermissionRequest`, `Notification`, `TokenUsage`, `ModelSelected`, `MCPToolsChanged`.

**Why each was removed**:
- `PostToolUseFailure`: Subsumed by `PostToolUse` -- the payload already contains an `Err` field.
- `PreCompact`, `ContextCompacted`: Compaction is an internal runtime concern and does not need hook intervention.
- `UserPromptSubmit`: YOLO mode does not need to validate user input via hooks.
- `PermissionRequest`: YOLO mode defaults to allow-all; dangerous command blocking is handled by the Go-native safety hook in `PreToolUse`, not a separate permission event.
- `Notification`, `TokenUsage`, `ModelSelected`, `MCPToolsChanged`: Niche events with zero known shell hook consumers in the wild.

**Why SubagentStart/SubagentStop are kept**: Subagent lifecycle visibility is essential for observability and tracing. Users need to know when child agents are spawned and terminated, especially for debugging long-running multi-agent workflows.

### 5. api.Runtime (Slim Struct)

**Location**: `pkg/api/agent.go`

```go
type Runtime struct {
    opts      Options          // frozen configuration
    model     model.Model      // the single model provider
    registry  *tool.Registry   // tool registry
    executor  *tool.Executor   // tool executor
    hooks     *hooks.Executor  // hook executor
    histories *historyStore    // in-memory session histories
    compactor *compactor       // prompt-compression compactor

    mu        sync.RWMutex
    closeOnce sync.Once
    closeErr  error
    closed    bool
}
```

**Removed fields** (10 from v1):
- `cmdExec` -- slash command execution folded into request path
- `taskStore` -- deleted (tasks removed)
- `tokens` -- token tracking is derivable from model responses
- `recorder` -- hook recording folded into per-request state
- `sessionGate` -- concurrent session guard removed (callers manage)
- `historyPersister` -- disk persistence removed from core
- `rulesLoader` -- rules loaded once at init, not stored as field
- `ownsTaskStore` -- gone with `taskStore`
- `sandbox` -- moved to tool executor concern
- `sbRoot`, `cfg`, `fs`, `settings`, `mode`, `tracer` -- consolidated or removed

**Why**: Most removed fields serve niche use cases (task tracking, disk persistence, session gating) that belong in application code. The Runtime should be a thin orchestrator, not a god object.

---

## Data Flow

### Request Processing Flow (Blocking Run)

```
User Code
    │
    ▼
Runtime.Run(ctx, Request)
    │
    ├── 1. Resolve session history (historyStore.getOrCreate)
    ├── 2. Append user message to history
    ├── 3. Build model.Request (system prompt + history + tools)
    ├── 4. LOOP:
    │       │
    │       ├── 4a. Check context cancellation
    │       ├── 4b. Check max iterations
    │       ├── 4c. middleware.Chain.Execute(BeforeAgent) [first iteration only]
    │       ├── 4d. model.CompleteStream(ctx, req, handler)
    │       │       └── handler accumulates StreamResults into Response
    │       ├── 4e. Append assistant message to history
    │       ├── 4g. If no tool calls or Done → break
    │       ├── 4h. For each tool call:
    │       │       ├── hooks.Execute(PreToolUse)
    │       │       ├── SafetyHook check
    │       │       ├── middleware.Chain.Execute(BeforeTool)
    │       │       ├── tool.Execute(ctx, params)
    │       │       ├── middleware.Chain.Execute(AfterTool)
    │       │       ├── hooks.Execute(PostToolUse)
    │       │       └── Append tool result to history
    │       ├── 4i. compactor.MaybeCompact(history)
    │       └── 4j. Rebuild model.Request with updated history
    │
    ├── 5. Return final output
    │
    ▼
User Code receives Result
```

### Streaming Data Flow

```
Runtime.RunStream(ctx, Request) → chan StreamEvent
    │
    └── Internally:
        ├── Same loop as Run()
        ├── model.CompleteStream callback emits deltas to channel
        ├── Tool calls/results emitted as structured events
        └── Final event signals completion
```

### Agent Loop Integration (v2 vs v1)

**v1 data flow** (three-layer indirection):
```
Runtime.Run → agent.Agent.Run → conversationModel.Generate → model.CompleteStream
                                      ↑ bridge adapter
```

**v2 data flow** (direct):
```
Runtime.Run → model.CompleteStream (inline loop)
```

The agent loop logic from `pkg/agent/agent.go` (lines 70-189) is inlined into `Runtime.runLoop()` in `pkg/api`. This eliminates:
- The `agent.Model` interface
- The `agent.Context` type (replaced by `middleware.State`)
- The `agent.ToolExecutor` interface (replaced by direct `tool.Executor` calls)
- The `conversationModel` bridge adapter (~90 lines)

---

## Prompt Compression Compaction Design (Strip Tool I/O)

### Motivation

v1 compaction (`pkg/api/compact.go`, 450 lines) mixes two concerns:
1. Memory management (reduce context size)
2. Information preservation (summarize intent)

In v2, compaction is still a model call (prompt compression) because it preserves intent better than pure dropping, but it must be tightly controlled:
- Preserve the last N messages unmodified.
- Strip tool I/O from the compressed portion so tool outputs do not dominate context.
- Avoid v1 complexity (no fallback model, no retries, no rollout writers).

### Algorithm

```
Input: messages[]  (full conversation history)
       preserveCount  (number of recent messages to keep, default 5)
       threshold  (trigger ratio, default 0.8)
       tokenLimit  (model context window size)

Trigger condition:
  tokenCount(messages) / tokenLimit >= threshold
  AND len(messages) > preserveCount

Algorithm:
  1. cut = len(messages) - preserveCount
  2. Compute toolTransactionSpans(messages)
  3. For each span that straddles the cut point:
       Move cut earlier to span.start (never orphan tool results in the preserved tail)
  4. If cut <= 0: skip compaction (all messages are within one transaction)
  5. head = messages[0:cut]
  6. tail = messages[cut:]
  7. Build compression input from head by filtering out tool-call/tool-result content
  8. Call model.Complete(...) with a dedicated "prompt compression" instruction
  9. Replace history with: [summaryMessage] + tail
  10. Return: {tokensBefore, tokensAfter, preservedCount, summarySize}
```

### What Is Stripped

For the compressed portion (head), tool-call/tool-result content is excluded from the compression input. The preserved tail is kept verbatim.

### Tool Transaction Boundary Preservation

The `toolTransactionSpans()` function from v1 is **reused** (it is correct and ~40 lines). A tool transaction is the span from an assistant message containing tool calls to the last corresponding tool result message. The compaction cut point is always adjusted to avoid splitting a transaction.

```go
type toolTransactionSpan struct {
    start int  // index of assistant message with tool calls
    end   int  // index after last tool result message
}
```

**Edge case**: If the entire history is one unfinished tool transaction, compaction is skipped. This is correct behavior -- you cannot drop messages mid-transaction.

### CompactConfig (Simplified)

```go
type CompactConfig struct {
    Enabled       bool    `json:"enabled"`
    Threshold     float64 `json:"threshold"`       // trigger ratio (default 0.8)
    PreserveCount int     `json:"preserve_count"`   // keep latest N messages (default 5)
}
```

**Removed fields** (from v1 CompactConfig):
- `SummaryModel`, `FallbackModel` -- compaction uses the primary model with a dedicated prompt
- `MaxRetries`, `RetryDelay` -- no retries in core compaction
- `PreserveInitial`, `InitialCount` -- system prompt is injected per-request, not stored in history
- `PreserveUserText`, `UserTextTokens` -- over-engineering for edge cases
- `RolloutDir` -- no rollout persistence in core

### File Size Target

The entire compaction implementation targets **<= 200 non-test lines** in a single file `pkg/api/compact.go`.

---

## Subagent Result Summary Injection

### Problem

In v1, when a subagent completes work, the parent agent receives the raw result but has no mechanism to inject a structured summary into its own context. The subagent's work is a "black box" -- the parent knows the task completed but not what was learned.

### Mechanism

When a subagent completes via `Manager.Dispatch()`, the result is formatted as a structured summary and injected into the parent agent's conversation history as a system message:

```go
// In Runtime, after subagent dispatch returns:
summary := formatSubagentSummary(result)
parentHistory.Append(message.Message{
    Role:    "user",          // user role so model treats it as context
    Content: summary,
})
```

### Summary Format

```
[Subagent Result: {name}]
Task: {instruction}
Status: {success|error}
Output: {result.Output, truncated to 2000 chars}
{if error: Error: {result.Error}}
```

### Why User Role

The summary is injected as a `user` message (not `system`) because:
1. System messages in Anthropic API have special handling and token costs
2. User messages are treated as conversation context the model should address
3. The model naturally responds to user-role information

### Configuration

Summary injection is enabled by default when subagents are registered. It can be disabled via `Options.DisableSubagentSummary bool`.

---

## Safety Hook Mechanism

### Design

The safety hook replaces v1's complex permission system (`pkg/security/approval.go`, `pkg/security/permission_matcher.go`, `pkg/security/resolver.go`) with a single Go function that blocks catastrophic operations.

```go
// SafetyHook is called before every tool execution. Return a non-nil
// error to block the tool call. The error message is returned to the model.
type SafetyHook func(ctx context.Context, toolName string, params map[string]any) error
```

### Default Safety Hook

The default hook reuses the blocklist patterns from `pkg/security/validator.go`:

```go
func DefaultSafetyHook(ctx context.Context, toolName string, params map[string]any) error {
    if toolName != "bash" && toolName != "Bash" {
        return nil // only bash commands need safety validation
    }
    command, ok := params["command"].(string)
    if !ok || command == "" {
        return nil
    }
    return defaultValidator.Validate(command)
}
```

**Blocked patterns** (same as v1 `security.Validator`):
- Commands: `dd`, `mkfs`, `fdisk`, `parted`, `shutdown`, `reboot`, `halt`, `poweroff`, `mount`, `sudo`
- Fragments: `rm -rf`, `rm -fr`, `rm -r`, `rm --recursive`, `rmdir -p`, `rm *`, `rm /`, `-rf /`, `--no-preserve-root`
- Arguments: `--no-preserve-root`, `--preserve-root=false`, `/dev/`, `../`

### YOLO Default

By default, `Options.SafetyHook` is set to `DefaultSafetyHook`. This means:
- All tool calls are allowed without interactive permission prompts
- Only catastrophic bash commands are blocked
- No "ask" or "deny" permission rules -- tools just execute

### Opt-in Stricter Security

Users who need stricter security can:
1. **Replace the safety hook**: `Options.SafetyHook = myCustomHook`
2. **Use sandbox isolation**: `Options.Sandbox` enables filesystem/network guards
3. **Use permission hooks**: Register `PreToolUse` hooks that return deny decisions

### Performance

The safety hook is a Go function call, not a shell command. Overhead: <1ms per tool call (string matching against ~15 patterns). No process spawning, no JSON serialization.

---

## Configuration System Design

### Settings Precedence (unchanged from v1)

```
Priority (high → low):
1. Runtime overrides (Options.SettingsOverrides)
2. .agents/settings.local.json (gitignored, developer-specific)
3. .agents/settings.json (project-level, tracked)
4. SDK defaults
```

### Settings Struct (unchanged)

The `config.Settings` struct is unchanged from v1. Key fields:

```go
type Settings struct {
    Permissions     *PermissionsConfig `json:"permissions,omitempty"`
    Hooks           *HooksConfig       `json:"hooks,omitempty"`
    Env             map[string]string  `json:"env,omitempty"`
    Model           string             `json:"model,omitempty"`
    MCP             *MCPConfig         `json:"mcp,omitempty"`
    Sandbox         *SandboxConfig     `json:"sandbox,omitempty"`
    DisallowedTools []string           `json:"disallowedTools,omitempty"`
    RespectGitignore *bool             `json:"respectGitignore,omitempty"`
    // ... other fields preserved
}
```

### Hot-Reload

Configuration hot-reload via `fsnotify` is preserved. When `.agents/settings.json` or `.agents/settings.local.json` changes:
1. Re-load and merge settings
2. Update tool registry (add/remove tools per `DisallowedTools`)
3. Update hook executor (register/unregister hooks per `Hooks`)
4. No restart required

### Directory Structure

```
.agents/
├── settings.json        # Project configuration (tracked)
├── settings.local.json  # Developer overrides (gitignored)
├── skills/              # Skill definitions (*.md files)
└── agents/              # Subagent definitions
```

### AGENTS.md / Memory + Rules

At `Runtime.New()`, the runtime optionally loads `AGENTS.md` (with `@include` support) and appends it under a `## Memory` header to the system prompt. Project rules are loaded from `.agents/rules` and consumed during initialization (they are not stored on the Runtime).

---

## Component Responsibilities

### pkg/api (~2000 lines target)

| Responsibility | Description |
|---------------|-------------|
| Runtime struct | Slim orchestrator with ~7 fields |
| Agent loop | Inlined from `pkg/agent`, drives model/tool iteration |
| Compaction | Prompt compression with tool I/O stripping, <=200 lines |
| System prompt | Assembled from rules, skills, session context |
| Request handling | `Run()`, `RunStream()`, request normalization |
| History management | Session-keyed in-memory history store |
| Lifecycle | `New()`, `Close()` with `sync.Once` idempotency |

### pkg/model (~2000 lines target)

| Responsibility | Description |
|---------------|-------------|
| Model interface | Single `Complete`/`CompleteStream` interface |
| AnthropicProvider | Anthropic SDK wrapper, streaming, token counting |
| OpenAIProvider | OpenAI SDK wrapper, chat completions + responses API |
| Types | Message, Request, Response, StreamResult, ToolCall, ToolDefinition, Usage, ContentBlock |
| Provider factory | `NewAnthropicProvider()`, `NewOpenAI()` constructors |

### pkg/tool (~1500 lines target) + pkg/tool/builtin (~4500 lines target)

| Responsibility | Description |
|---------------|-------------|
| Tool interface | `Name()`, `Description()`, `Schema()`, `Execute()` |
| Registry | Thread-safe tool registration, lookup, listing |
| Executor | JSON Schema validation, tool dispatch, error wrapping |
| Validator | Parameter validation against JSON Schema |
| MCP tools | MCP client session management, tool proxy |
| Built-in tools | `bash`, `read`, `write`, `edit`, `glob`, `grep`, `skill` |

**Deleted tools**: `todo_write` (dead code), `task`/`taskcreate`/`taskget`/`tasklist`/`taskupdate`/`killtask` (tasks removed), `slashcommand` (commands removed), `askuserquestion` (YOLO mode), `webfetch`/`websearch` (not core), `bash_output`/`bash_status` (not core).

### pkg/middleware (~1800 lines target)

| Responsibility | Description |
|---------------|-------------|
| Middleware interface | 4-stage interception: BeforeAgent, BeforeTool, AfterTool, AfterAgent |
| Chain | Sequential execution with short-circuit semantics |
| Funcs helper | Function-pointer adapter for quick middleware creation |
| State | Shared mutable state across middleware invocations |
| TraceMiddleware | JSONL + HTML trace viewer (OTel integration) |
| HTTP trace | HTTP-specific trace middleware variant |

### pkg/hooks (~500 lines target)

| Responsibility | Description |
|---------------|-------------|
| EventType | 7 event type constants |
| Event | Lightweight event struct with type, session ID, payload |
| Payload types | ToolUsePayload, ToolResultPayload, SessionPayload, StopPayload, SubagentPayload |
| Executor | Shell hook spawner with JSON stdin, exit code semantics |
| Selector | Regex-based hook matching by tool name and payload pattern |
| ShellHook | Hook definition: event, command, timeout, async, once |

**Merged from**: `pkg/core/events` (types + payloads) + `pkg/core/hooks` (executor + selector).
**Also merged**: `pkg/core/middleware` (24 lines, the `Handler`/`Middleware`/`Chain` types for hook middleware) becomes internal to `pkg/hooks`.

### pkg/config (~1500 lines target)

| Responsibility | Description |
|---------------|-------------|
| Settings types | `Settings`, `PermissionsConfig`, `HooksConfig`, `MCPConfig`, `SandboxConfig` |
| SettingsLoader | Layer-based settings merge (defaults < project < local < runtime) |
| RulesLoader | `.agents/rules` project rules loading |
| Hot-reload | fsnotify-based configuration watching |
| FS abstraction | Testable filesystem operations |

### pkg/message (as-is, ~275 lines)

| Responsibility | Description |
|---------------|-------------|
| History | Thread-safe in-memory message store |
| Message | Role + content + tool calls + metadata |
| TokenCounter | Naive token estimation |
| Clone utilities | Deep copy for isolation |

### pkg/sandbox (~400 lines target)

| Responsibility | Description |
|---------------|-------------|
| Manager | Filesystem path whitelist, symlink resolution |
| Network isolation | Allow-list for outbound connections |

**Note**: Command validation (safety hook) moves to `pkg/hooks/safety.go`, not sandbox. Sandbox is purely for path/network isolation. The `pkg/security/` package is deleted — its `Validator` logic is extracted into the Go-native `DefaultSafetyHook` function in `pkg/hooks/`.

### pkg/mcp (~450 lines target)

| Responsibility | Description |
|---------------|-------------|
| ClientSession | MCP protocol client |
| Transport | stdio and SSE transport builders |
| Tool proxy | Convert MCP tools to `tool.Tool` interface |

### pkg/runtime/skills (~1000 lines target)

| Responsibility | Description |
|---------------|-------------|
| Registry | Skill definition storage and lookup |
| Loader | Load skills from `.agents/skills/` directory |
| Matcher | Pattern matching for skill activation |
| Prompt templates | Skill-related prompt template rendering (absorbed from `pkg/prompts`) |

### pkg/runtime/subagents (~700 lines target)

| Responsibility | Description |
|---------------|-------------|
| Manager | Subagent registration and dispatch |
| Definition | Subagent type definitions (general-purpose, explore, plan) |
| Context | Subagent execution context with tool whitelist |
| Result | Subagent output + summary formatting |

---

## Options Struct (Simplified)

```go
type Options struct {
    // Required
    ModelFactory model.Model
    ProjectRoot  string

    // Core behavior
    MaxIterations  int
    Timeout        time.Duration
    MaxSessions    int

    // Compaction
    Compact CompactConfig

    // Extension points
    Middleware  []middleware.Middleware
    SafetyHook SafetyHook    // default: DefaultSafetyHook
    Hooks      []hooks.ShellHook

    // Configuration
    SettingsOverrides *config.Settings

    // Optional features
    MCP              *config.MCPConfig
    Sandbox          *SandboxOptions
    Skills           []SkillRegistration
    Subagents        []SubagentRegistration
    CustomTools      []tool.Tool

    // Metadata
    EntryPoint EntryPoint
}
```

---

## ACP / Tasks

ACP (`pkg/acp`) and tasks (`pkg/runtime/tasks`) are deleted in v2 core. The repository does not carry a `contrib/` module for them.

---

## Architecture Decision Records

### ADR-001: Merge Agent Loop into Runtime

- **Context**: `pkg/agent` (233 lines) defines `agent.Model` and the agent loop. `pkg/api` wraps it with a `conversationModel` bridge adapter to convert between `model.Model` and `agent.Model`.
- **Options considered**: (A) Keep separate packages, simplify bridge. (B) Merge `pkg/agent` into `pkg/api`.
- **Decision**: Option B -- merge into `pkg/api`.
- **Rationale**: The agent loop is tightly coupled to Runtime's history management, compaction, and hook dispatch. Separating them requires a bridge adapter that adds complexity without modularity benefit. The agent loop is ~120 lines of logic -- not large enough to justify its own package.
- **Consequences**: `pkg/agent` package is deleted. The `agent.Model`, `agent.Context`, `agent.ToolCall`, `agent.ToolResult`, `agent.ModelOutput` types are all deleted. Any code referencing these types must be updated.

### ADR-002: Prompt Compression Compaction (Strip Tool I/O)

- **Context**: v1 compaction calls an LLM to summarize dropped messages (450 lines including retry, fallback model, rollout writer).
- **Options considered**: (A) Pure strip: drop oldest messages, no summarization. (B) Prompt compression: summarize old content using the model. (C) Keep v1 approach with fallback/retry/rollout.
- **Decision**: Option B -- prompt compression, but strip tool I/O from the compressed portion and keep the preserved tail verbatim.
- **Rationale**: Tool outputs are high-token noise; removing them from compression input improves summary signal. Pure strip loses intent. Keeping v1 fallback/retry/rollout complexity is not justified in core.
- **Consequences**: No `SummaryModel`/`FallbackModel` fields; no retry/fallback/rollout machinery in core compaction. Compaction behavior is testable with a stub model.

### ADR-003: 4 Middleware Stages Instead of 6

- **Context**: v1 has 6 stages: BeforeAgent, BeforeModel, AfterModel, BeforeTool, AfterTool, AfterAgent. BeforeModel/AfterModel add per-iteration overhead but model calls are deterministic functions of the current history.
- **Options considered**: (A) Keep 6 stages. (B) Remove BeforeModel/AfterModel, keep 4 (BeforeAgent, BeforeTool, AfterTool, AfterAgent).
- **Decision**: Option B -- 4 stages.
- **Rationale**: BeforeModel/AfterModel fire every iteration of the agent loop. In practice, the only consumer was trace middleware which can achieve the same observability via `BeforeAgent` (request boundary) and `AfterAgent` (response boundary) plus `State.ModelInput`/`State.ModelOutput` fields. Removing per-iteration middleware overhead also improves latency for multi-iteration runs.
- **Consequences**: Trace middleware uses `BeforeAgent`/`AfterAgent` for request/response-level tracing. Model-level detail (request, response, usage) is available via `middleware.State` fields populated by the agent loop. Two fewer methods on the interface means less boilerplate for every middleware implementation.

### ADR-004: YOLO Default Security

- **Context**: v1 has a multi-layer permission system: `PermissionsConfig` with allow/deny/ask rules, `ApprovalRecord` persistence, `PermissionRequestHandler` callbacks, and `security.Resolver` resolution logic.
- **Options considered**: (A) Keep full permission system. (B) YOLO default with safety hook. (C) Remove all security.
- **Decision**: Option B -- YOLO default with Go-native safety hook.
- **Rationale**: The permission system adds ~800 lines of code for a feature most SDK users don't use (they have their own security layers). A simple Go function that blocks `rm -rf /` and `sudo` covers the catastrophic-mistake case with <1ms overhead and zero configuration.
- **Consequences**: `security.ApprovalRecord`, `security.Resolver`, `api.PermissionRequestHandler` are removed from core. Permission-based security can be re-added via `PreToolUse` hooks. The `Permissions` field in `Settings` still exists (for settings file compat) but only `deny` rules are enforced by default.

### ADR-005: Events Merged into Hooks Package

- **Context**: v1 has `pkg/core/events` (types + payloads, 549 lines) and `pkg/core/hooks` (executor, 732 lines) as separate packages. The hooks package imports events. No other package imports events without also importing hooks.
- **Options considered**: (A) Keep separate. (B) Merge into single `pkg/hooks` package.
- **Decision**: Option B -- merge into `pkg/hooks`.
- **Rationale**: Events and hooks are a single concern: "lifecycle notifications with optional shell command execution." Separating them creates an artificial boundary that requires cross-package imports without enabling independent use. Merging reduces the package count by 2 (events + core container).
- **Consequences**: Import paths change from `pkg/core/events` and `pkg/core/hooks` to `pkg/hooks`. The `pkg/core/` directory is deleted entirely (its only other content, `pkg/core/middleware` at 24 lines, merges into `pkg/middleware`).

### ADR-006: Security Package Absorption into Hooks

- **Context**: `pkg/security` contains `Validator` (command validation), `ApprovalRecord`, `PermissionMatcher`, `Resolver`. With YOLO default, only `Validator` survives.
- **Options considered**: (A) Keep `pkg/security` with just `Validator`. (B) Merge `Validator` into `pkg/sandbox`. (C) Merge `Validator` into `pkg/hooks` as the `DefaultSafetyHook`.
- **Decision**: Option C -- merge into `pkg/hooks`.
- **Rationale**: The `Validator` is functionally a `PreToolUse` hook that blocks dangerous bash commands. This is exactly what the safety hook does. Placing it in `pkg/hooks/safety.go` makes the relationship explicit: safety validation is a built-in hook, not a separate security layer. `pkg/sandbox` remains focused on path/network isolation (orthogonal concern).
- **Consequences**: `pkg/security/` is deleted entirely. `hooks.DefaultSafetyHook` absorbs the validator patterns. Import paths update accordingly. Approval/permission matcher code is deleted (YOLO default).

---

## Testing Strategy

### Test Principles

1. **Requirement-driven**: Each test traces to a PRD story or acceptance criterion.
2. **No real API calls**: All tests use mock/stub Model implementations.
3. **No v1 test porting**: Tests are written from scratch against v2 interfaces.
4. **Table-driven**: Multiple scenarios in a single test function where applicable.

### Critical Test Paths

| Priority | Test Area | What to Test | Traces To |
|----------|----------|-------------|-----------|
| P0 | Runtime lifecycle | `New()` with minimal options succeeds; `Close()` is idempotent; double-close returns same error | Story 2, A2 |
| P0 | Single-prompt run | `Run()` with stub model returns expected output; history contains user + assistant messages | Story 2, A14 |
| P0 | Streaming run | `RunStream()` emits deltas and final event; channel closes after completion | Story 2 |
| P0 | Tool execution | Model returns tool call; tool executes; result fed back to model; model produces final output | Story 7, A9 |
| P0 | Compaction trigger | History exceeds threshold; compaction invokes prompt compression; preserved tail kept verbatim | Story 3, A4, A5 |
| P0 | Compaction strips tool I/O | Compression input excludes tool-call/tool-result content | Story 3, A4 |
| P0 | Middleware chain | 4-stage execution in correct order; error in BeforeAgent short-circuits | Story 6, A8 |
| P0 | Event dispatch | PreToolUse/PostToolUse hooks fire with correct payloads | Story 5, A7 |
| P0 | Context cancellation | Cancelled context stops agent loop; returns context error | General |
| P0 | Safety hook | `rm -rf /` blocked; `ls` allowed; custom hook overrides default | FR-9 |
| P1 | Max iterations | Loop stops at MaxIterations; returns ErrMaxIterations | General |
| P1 | Session isolation | Two sessions have independent histories | General |
| P1 | MCP tool registration | MCP tools appear in registry; execute correctly | General |
| P1 | Config hot-reload | Settings change triggers tool registry update | General |

### Mock Model Implementation

```go
type stubModel struct {
    responses []model.Response
    index     int
}

func (m *stubModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
    if m.index >= len(m.responses) {
        return &model.Response{Message: model.Message{Content: "done"}}, nil
    }
    resp := m.responses[m.index]
    m.index++
    return &resp, nil
}

func (m *stubModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
    resp, err := m.Complete(ctx, req)
    if err != nil {
        return err
    }
    return cb(model.StreamResult{Final: true, Response: resp})
}
```

### Test File Organization

Tests are co-located with implementation:
- `pkg/api/agent_test.go` -- Runtime lifecycle, run, streaming
- `pkg/api/compact_test.go` -- Compaction logic
- `pkg/middleware/chain_test.go` -- Middleware chain execution
- `pkg/hooks/executor_test.go` -- Hook execution, event dispatch
- `pkg/tool/registry_test.go` -- Tool registration, lookup
- `pkg/model/anthropic_test.go` -- Anthropic provider
- `pkg/model/openai_test.go` -- OpenAI provider

### Verification Commands

```bash
# All tests pass
go test ./pkg/...

# No v1 references
grep -r "agent.Model" pkg/     # expect 0 matches
grep -r "agent.Context" pkg/   # expect 0 matches

# Package count
find pkg -type d | wc -l       # expect <= 11

# Line count
find pkg -name '*.go' ! -name '*_test.go' | xargs wc -l | tail -1  # expect <= 20000

# Build
go build ./...
```

---

## Migration Sequence (Phase Ordering)

### Phase 1: Core Structural Changes

```
Step 1: Create v2 branch
Step 2: Merge agent.Model into model.Model
    - Delete pkg/agent/
    - Inline agent loop into pkg/api/agent.go
    - Delete conversationModel bridge adapter
    - Update all references
Step 3: Slim Runtime struct
    - Remove 10 fields
    - Simplify New() constructor
Step 4: Replace compaction
    - Rewrite compact.go (prompt compression, <=200 lines)
    - Strip tool I/O from compression input
    - No fallback/retry/rollout writer
    - Keep toolTransactionSpans() for tail boundary safety
Step 5: Merge packages
    - core/events + core/hooks → pkg/hooks
    - core/middleware → pkg/middleware
    - Delete pkg/core/ entirely
Step 6: Reduce events 16 → 7
Step 7: Reduce middleware 6 → 4 stages

Gate: go build ./... passes
```

### Phase 2: Tool & Feature Cleanup

```
Step 8: Delete todo_write tool
Step 9: Delete task tools + `pkg/runtime/tasks/`
Step 10: Delete `pkg/acp/` and remove CLI ACP mode
Step 11: Remove slashcommand, askuserquestion, webfetch, websearch, bash_output, bash_status from core tools
Step 12: Delete pkg/runtime/commands/ (slash commands removed)
Step 13: Merge prompts: skill templates → runtime/skills
Step 14: Absorb security → hooks/safety.go
Step 15: Implement safety hook

Gate: go build ./... passes
```

### Phase 3: Testing & Validation

```
Step 16: Rewrite core tests (Runtime, run, streaming, tools, compaction)
Step 17: Rewrite middleware/hooks tests
Step 18: Update all 12 examples
Step 19: Verify line count and package count targets
Step 20: Final go test ./pkg/... pass

Gate: All acceptance criteria from PRD pass
```

---

## Handoff Notes

### For Implementation Agent (harness)

- **Start with Step 2** (Model merge). Everything else depends on eliminating the dual interface. Do not attempt parallel execution of Phase 1 steps -- they are sequential.
- **Reuse `toolTransactionSpans()`** from `pkg/api/compact.go` lines 399-436. It is correct, well-tested, and ~40 lines. Copy it into the new compact.go.
- **The agent loop is ~120 lines of actual logic** (the rest of `pkg/agent/agent.go` is type definitions and constructor). When inlining, keep the loop structure but replace `a.model.Generate()` with `r.model.CompleteStream()` and manage history inline.
- **Do not create a new `agent.Context` equivalent**. Use `middleware.State` directly. The agent context was thin glue that added no value.
- **Import path changes are mechanical**. After merging `pkg/core/*`, find-and-replace `coreevents "github.com/stellarlinkco/agentsdk-go/pkg/core/events"` with `"github.com/stellarlinkco/agentsdk-go/pkg/hooks"` across all files.

### For Architecture Guardrails

Enforce these rules mechanically (CI checks):
1. `grep -r "pkg/agent" pkg/` returns 0 matches (after Phase 1 Step 2)
2. `rg -n "\\bpkg/acp\\b" pkg cmd -S` returns 0 matches (after Phase 2)
3. `rg -n "\\bpkg/runtime/tasks\\b" pkg cmd -S` returns 0 matches (after Phase 2)
4. `find pkg -type d | wc -l` <= target (after Phase 2)
5. No `BeforeModel` or `AfterModel` references in `pkg/middleware/types.go`
6. Exactly 7 `EventType` constants in `pkg/hooks/`
7. `go build ./...` passes (gate between phases)

### For Testing Agent

- **Most important test**: Runtime + stub model + tool call + compaction. This single end-to-end path covers 60%+ of the codebase.
- **Compaction test must include**: A stub model that records the compression request and asserts tool I/O is excluded from compression input.
- **Middleware test**: Verify 4 stages fire in order: BeforeAgent -> BeforeTool -> AfterTool -> AfterAgent. Verify error in BeforeAgent prevents tool execution.

### Open Decisions

- **OQ1**: Exact naming for the merged Model interface methods -- `Complete`/`CompleteStream` is the current choice and should be kept.
- **OQ2**: Whether `askuserquestion` should remain in core -- currently removed per PRD assumption A3.
- **`pkg/security` absorption**: Resolved -- `security.Validator` patterns move to `pkg/hooks/safety.go` as `DefaultSafetyHook`. No circular dependency risk since hooks is a leaf package.

### Known Risks

1. **Risk**: `pkg/runtime/*` sub-packages push directory count above 11. **Mitigation**: The PRD counts "packages" not "directories". `runtime/skills` and `runtime/subagents` are logically one group (commands is deleted). If the count is strict, flatten to `pkg/skills`, `pkg/subagents`.
2. **Risk**: Prompt compression may change summary wording across runs. **Mitigation**: Keep preserved tail verbatim; bound summary size; make compaction testable with a stub model.
3. **Risk**: Removing `BeforeModel`/`AfterModel` breaks trace middleware that observed model requests/responses. **Mitigation**: Trace middleware uses `BeforeAgent` (sees full request) and `AfterAgent` (sees full response including usage). Model-level detail is available via `State.ModelInput`/`State.ModelOutput` fields populated by the agent loop.

---

*This architecture document is optimized for autonomous agent consumption. Every convention is mechanical. Every decision traces to a PRD requirement.*
