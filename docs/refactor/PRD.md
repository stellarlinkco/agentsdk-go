# Product Requirements Document: agentsdk-go v2 Simplification

**Version**: 2.0
**Date**: 2026-03-17
**Author**: PRD Compiler
**Quality Score**: 93/100
**Status**: Final

## Quick Reference (Agent Context)

> **Goal**: Reduce agentsdk-go from ~34K lines / 24 packages to ~15-20K lines / ~11 packages via big-bang rewrite on a new branch, producing a simpler, faster, more testable SDK.
> **Non-Goals**: Incremental migration, backward API compatibility, new external dependencies, moving OTel out of middleware/trace.
> **Primary Workflow**: New branch rewrite -> merge single Model interface -> consolidate packages -> prompt-compression compaction (strip tool I/O) -> reduce tools/events/middleware -> delete dead code -> rewrite key tests.
> **Success Metric**: `go test ./...` passes; total non-test LOC in pkg/ <= 20K; package count <= 11; examples require API keys (no offline mode).
> **Key Constraints**: Zero new dependencies; YOLO-default security with safety hook; must preserve gitignore matcher (grep/glob depend on it); message package kept as-is.
> **Verification Path**: `wc -l` on non-test .go files; `go test ./...`; package count via `find pkg -maxdepth 1 -type d | tail -n +2 | wc -l`; manual example smoke test.
> **Domain**: Go SDK, CLI/CI/enterprise agent runtime.

---

## Executive Summary

agentsdk-go v1 grew organically to ~34K non-test lines across 24+ packages. The architecture has two Model interfaces that require a bridge adapter, a compaction system that calls LLMs (~450 lines), 16 event types, 6 middleware stages, 11+ built-in tools, and a Runtime struct carrying 10+ fields that most users never touch. This complexity violates the project's KISS/YAGNI principles and makes the SDK harder to understand, test, and extend.

v2 is a big-bang rewrite on a new branch. It merges the dual Model interface into one, simplifies compaction to a prompt-compression step that strips tool I/O, consolidates 24 packages into ~11, reduces events from 16 to 7, middleware stages from 6 to 4, reduces built-in tools from 11 to 7, and deletes ACP/Tasks from core. The Runtime struct drops 10 fields. Target: 15-20K lines that do the same useful work.

---

## Problem Statement

**Current Situation**: The SDK has accumulated structural debt:
- Two Model interfaces (`agent.Model.Generate` and `model.Model.Complete/CompleteStream`) require a bridge in `pkg/api` that adds ~200 lines of glue.
- `pkg/api/agent.go` is 1804 lines (target <600). The Runtime struct has 17+ fields, 10 of which are rarely used.
- Compaction (`compact.go`, 450 lines) calls an LLM for summarization — an external dependency in what should be a deterministic operation.
- 16 event types when 7 suffice. 6 middleware stages when 4 cover all real use cases.
- `pkg/acp` (2618 lines) and `pkg/runtime/tasks` (543 lines) serve niche use cases but live in core.
- `todo_write` tool is dead code.
- `pkg/prompts` (1105 lines) splits prompt logic between two packages unnecessarily.
- Total: ~34K non-test lines across 24 packages.

**Proposed Outcome**: A rewritten SDK with ~15-20K non-test lines across ~11 packages, a single Model interface, prompt-compression compaction that strips tool I/O, and a clean Runtime struct — while preserving all user-facing capabilities.

**Why Now**: The codebase is at an inflection point where further feature work compounds the structural debt. The project's Linus Torvalds-inspired KISS philosophy demands correction before the surface area grows further.

---

## Goals

- G1: Merge dual Model interface into a single `model.Model` with `Complete` and `CompleteStream`.
- G2: Reduce `pkg/api/agent.go` to <600 lines. Remove 10 fields from Runtime struct.
- G3: Simplify compaction to prompt compression while stripping tool I/O (tool calls/results) from the compressed portion.
- G4: Consolidate 24 packages to ~11 packages.
- G5: Reduce events 16 -> 7, middleware 6 -> 4 stages, tools 11 -> 7.
- G6: Delete ACP and Tasks from core (no `contrib/`).
- G7: Delete dead code (`todo_write` tool, unused event types, deprecated fields).
- G8: Rewrite key tests (not ported from v1) that cover core paths.
- G9: All examples require API keys (no offline mode / no silent fallback).

## Non-Goals

- NG1: Incremental migration path or backward API compatibility with v1.
- NG2: Moving OpenTelemetry out of `middleware/trace` — it stays where it is.
- NG3: Adding new external dependencies.
- NG4: Rewriting or removing `pkg/gitignore` — grep/glob tools depend on it.
- NG5: Changing `pkg/message` — kept as-is.
- NG6: Porting old tests as a safety net — key tests are rewritten from requirements.
- NG7: Achieving specific coverage percentage targets in this PRD (coverage is a separate concern).

---

## Confirmed Facts, Assumptions, and Open Questions

### Confirmed Facts
- Current codebase: ~34K non-test lines, 24+ packages.
- `agent.Model` interface (`Generate`) and `model.Model` interface (`Complete`/`CompleteStream`) both exist with a bridge adapter in `pkg/api`.
- `compact.go` is 450 lines and calls LLM for summarization.
- Runtime struct has fields: `cmdExec`, `taskStore`, `tokens`, `recorder`, `sessionGate`, `historyPersister`, `rulesLoader`, `ownsTaskStore` — all confirmed for removal.
- `todo_write` tool (163 lines) is dead code — confirmed for deletion.
- `pkg/acp` (2618 lines) and `pkg/runtime/tasks` (543 lines) are deleted from core.
- Migration is big-bang on a new branch — no incremental migration needed.
- OTel stays in `middleware/trace`.
- `pkg/gitignore` (267 lines) stays — grep/glob depend on it.
- `pkg/message` (275 lines) stays as-is.
- Skill-related prompt templates merge into `pkg/runtime/skills`; system prompt construction stays in `pkg/api`.

### Working Assumptions
- A1: The 7 surviving events are: `PreToolUse`, `PostToolUse`, `SessionStart`, `SessionEnd`, `Stop`, `SubagentStart`, `SubagentStop`. The 9 removed events are: `PostToolUseFailure`, `PreCompact`, `ContextCompacted`, `UserPromptSubmit`, `PermissionRequest`, `Notification`, `TokenUsage`, `ModelSelected`, `MCPToolsChanged`.
- A2: The 4 surviving middleware stages are: `BeforeAgent`, `BeforeTool`, `AfterTool`, `AfterAgent`. Removed: `BeforeModel`, `AfterModel` (model call is observable via BeforeAgent/AfterAgent and trace).
- A3: The 7 surviving tools are: `bash`, `read`, `write`, `edit`, `glob`, `grep`, `skill`. Removed: `todo_write`, `task*` (4 tools), `slashcommand`, `askuserquestion`, `killtask`, `webfetch`, `websearch`, `bash_output`, `bash_status`. (Task tools are deleted; others are rarely used or can be added as custom tools.)
- A4: Compaction uses LLM prompt compression for old messages, but strips tool-call/tool-result content from the compressed portion to reduce noise and token usage.

### Open Questions (Non-Blocking)
- OQ1: Exact naming for the merged Model interface methods — `Complete`/`CompleteStream` is the likely choice since it already exists.
- OQ2: Whether `askuserquestion` should remain as a core tool — currently assumed removed from core.
- OQ3: Final event naming conventions for v2 — cosmetic, does not block implementation.

### Build Blockers (Must Resolve Before Build or Verification)
- None. All 10 open questions from the previous round have been resolved.

---

## Users and Primary Jobs

### Primary User: SDK Integrator (CLI/CI/Platform Developer)
- **Role**: Go developer embedding agent capabilities into their application.
- **Goal**: Initialize runtime, send prompts, receive streaming responses, execute tools — with minimal boilerplate.
- **Pain Point**: v1 requires understanding two Model interfaces, navigating 24 packages, and configuring fields on a Runtime struct where 10 of 17 fields are irrelevant to most use cases.

### Secondary User: Plugin/Extension Author
- **Role**: Developer adding custom tools, middleware, or event hooks.
- **Goal**: Extend SDK behavior without understanding internal wiring.
- **Pain Point**: 6 middleware stages, 16 event types, and tool registration spread across multiple packages make extension points hard to discover.

---

## User Stories & Acceptance Criteria

### Story 1: Single Model Interface

**As a** SDK integrator
**I want to** implement one Model interface to connect any LLM provider
**So that** I don't need to understand two interfaces and a bridge adapter.

**Acceptance Criteria:**
- [ ] Only `model.Model` with `Complete(ctx, Request) (*Response, error)` and `CompleteStream(ctx, Request, StreamHandler) error` exists.
- [ ] `agent.Model` interface (`Generate`) is deleted.
- [ ] No bridge adapter code exists in `pkg/api` to convert between the two interfaces.
- [ ] `AnthropicProvider` and `OpenAIProvider` implement the single interface directly.
- [ ] `go build ./...` compiles with zero references to the old `agent.Model` interface.

### Story 2: Slim Runtime Struct

**As a** SDK integrator
**I want to** construct a Runtime with only essential fields
**So that** I can understand the system by reading one struct definition.

**Acceptance Criteria:**
- [ ] Runtime struct does not contain: `cmdExec`, `taskStore`, `tokens`, `recorder`, `sessionGate`, `historyPersister`, `rulesLoader`, `ownsTaskStore`.
- [ ] `pkg/api/agent.go` is <= 600 non-test lines.
- [ ] `runtime.New()` succeeds with only `ModelFactory` and `ProjectRoot` provided.

### Story 3: Prompt Compression Compaction (Strip Tool Calls)

**As a** SDK integrator
**I want to** context compaction that reduces prompt size while keeping key intent
**So that** long sessions remain usable, and tool I/O does not dominate context.

**Acceptance Criteria:**
- [ ] Compaction preserves the last N messages (configurable) unmodified.
- [ ] Compaction compresses older messages into a summary using an LLM prompt compression step.
- [ ] The compressed portion strips tool-call/tool-result content (no tool JSON or tool output copied verbatim into the summary input).
- [ ] Compaction logic is <= 200 non-test lines in a single file.
- [ ] Tool transaction boundaries are respected (no orphaned tool-result messages in the preserved tail).
- [ ] Compaction triggers when token count exceeds threshold ratio of context limit.

### Story 4: Package Consolidation

**As a** SDK contributor
**I want to** navigate <= 11 packages to understand the full SDK
**So that** onboarding time is reduced and dependency graph is simple.

**Acceptance Criteria:**
- [ ] `find pkg -type d | wc -l` returns <= 11.
- [ ] `pkg/core/events` and `pkg/core/hooks` merge into a single `pkg/hooks` (or equivalent).
- [ ] `pkg/core/middleware` (24 lines) merges into `pkg/middleware`.
- [ ] `pkg/prompts` skills-related templates merge into `pkg/runtime/skills`.
- [ ] `pkg/prompts` system prompt construction stays in `pkg/api` (or merged appropriately).
- [ ] `pkg/agent` (233 lines) merges into `pkg/api` since the agent loop and runtime are tightly coupled.
- [ ] No circular import exists. `go build ./...` succeeds.

### Story 5: Event Reduction

**As a** extension author
**I want to** hook into <= 7 well-defined events
**So that** the event system is learnable in minutes.

**Acceptance Criteria:**
- [ ] Exactly 7 event types exist: `PreToolUse`, `PostToolUse`, `SessionStart`, `SessionEnd`, `Stop`, `SubagentStart`, `SubagentStop`.
- [ ] Removed events (`PostToolUseFailure`, `PreCompact`, `ContextCompacted`, `UserPromptSubmit`, `PermissionRequest`, `Notification`, `TokenUsage`, `ModelSelected`, `MCPToolsChanged`) have no references in core code.
- [ ] Event payload structs for removed events are deleted.

### Story 6: Middleware Stage Reduction

**As a** extension author
**I want to** implement middleware with 4 stages instead of 6
**So that** the interception model is simpler.

**Acceptance Criteria:**
- [ ] `middleware.Stage` enum has 4 values: `BeforeAgent`, `BeforeTool`, `AfterTool`, `AfterAgent`.
- [ ] `BeforeModel` and `AfterModel` stages are removed.
- [ ] `Middleware` interface has 4 methods (plus `Name()`), not 6.
- [ ] Model-level interception is not needed — model call is observable via request/response boundary middleware.

### Story 7: Tool Reduction

**As a** SDK integrator
**I want to** have 7 focused built-in tools
**So that** the default tool set covers essential operations without bloat.

**Acceptance Criteria:**
- [ ] Core built-in tools: `bash`, `read`, `write`, `edit`, `glob`, `grep`, `skill`.
- [ ] `todo_write` tool source files are deleted.
- [ ] Task-related tools (`task`, `taskcreate`, `taskget`, `tasklist`, `taskupdate`, `killtask`) are deleted.
- [ ] `slashcommand`, `askuserquestion`, `webfetch`, `websearch`, `bash_output`, `bash_status` tools are removed from core.
- [ ] Tool registration in Runtime defaults to the 7 core tools.

### Story 8: Remove ACP and Tasks

**As a** SDK maintainer
**I want to** remove ACP and task management from the SDK
**So that** core remains small and focused.

**Acceptance Criteria:**
- [ ] `pkg/acp` is deleted and there are zero references to it in `pkg/` and `cmd/`.
- [ ] `pkg/runtime/tasks` is deleted and there are zero references to it in `pkg/` and `cmd/`.
- [ ] CLI does not offer an ACP server mode (no `-acp` behavior in core).
- [ ] Root `go.mod` has no dependency on `github.com/coder/acp-go-sdk`.

### Story 9: Test Rewrite

**As a** SDK maintainer
**I want to** fresh tests written against v2 interfaces
**So that** tests validate actual requirements rather than carrying v1 implementation assumptions.

**Acceptance Criteria:**
- [ ] Key tests cover: Runtime creation, single-prompt run, streaming run, tool execution, compaction trigger, middleware chain, event dispatch, context cancellation.
- [ ] Tests use mock/stub Model implementations, not real API calls.
- [ ] `go test ./pkg/...` passes.
- [ ] No v1 test files are copied wholesale — tests are rewritten.

---

## Functional Requirements

### FR-1: Single Model Interface Merge
- **Description**: Eliminate `agent.Model` (Generate-based). The agent core loop calls `model.Model.CompleteStream` directly. Remove all bridge/adapter code.
- **Trigger**: v2 rewrite begins.
- **Expected Result**: One interface in `pkg/model/interface.go`. Zero adapter structs. Agent loop uses `CompleteStream` for streaming and `Complete` for blocking calls.
- **Traces to**: Story 1 / G1.

### FR-2: Runtime Struct Field Removal
- **Description**: Remove 10 fields from Runtime: `cmdExec`, `taskStore`, `tokens`, `recorder`, `sessionGate`, `historyPersister`, `rulesLoader`, `ownsTaskStore`, and associated initialization/cleanup code.
- **Trigger**: v2 rewrite begins.
- **Expected Result**: Runtime struct has ~7 essential fields. `agent.go` shrinks to <= 600 lines.
- **Traces to**: Story 2 / G2.

### FR-3: Prompt Compression Compaction (Strip Tool Calls)
- **Description**: When token count exceeds `threshold * limit`, preserve the last `PreserveCount` messages and compress older messages into a short summary using an LLM prompt compression step. Tool-call/tool-result content is stripped from the compressed portion.
- **Trigger**: Token count exceeds threshold during a run.
- **Expected Result**: Compaction reduces token usage materially while keeping intent. The preserved tail is not modified. Tool transactions are never split in the preserved tail.
- **Traces to**: Story 3 / G3.

### FR-4: Package Consolidation
- **Description**: Merge packages as follows:
  - `pkg/agent` -> merge into `pkg/api` (agent loop becomes internal to runtime).
  - `pkg/core/events` + `pkg/core/hooks` -> single package (e.g., `pkg/hooks`).
  - `pkg/core/middleware` -> merge into `pkg/middleware`.
  - `pkg/prompts` skill templates -> merge into `pkg/runtime/skills`.
  - `pkg/prompts` system prompt -> stays in `pkg/api` or dedicated small file.
  - `pkg/acp` -> deleted.
  - `pkg/runtime/tasks` -> deleted.
- **Trigger**: v2 rewrite begins.
- **Expected Result**: <= 11 directories under `pkg/`. Clean import graph. No circular dependencies.
- **Traces to**: Story 4 / G4.

### FR-5: Event Type Reduction
- **Description**: Keep 7 events: `PreToolUse`, `PostToolUse`, `SessionStart`, `SessionEnd`, `Stop`, `SubagentStart`, `SubagentStop`. Delete all others and their payload structs.
- **Trigger**: v2 rewrite begins.
- **Expected Result**: `hooks/events.go` (or equivalent) defines exactly 7 `EventType` constants and their payload structs.
- **Traces to**: Story 5 / G5.

### FR-6: Middleware Stage Reduction
- **Description**: Remove `BeforeModel` and `AfterModel` stages. Keep 4: `BeforeAgent`, `BeforeTool`, `AfterTool`, `AfterAgent`. Update `Middleware` interface and `Funcs` helper.
- **Trigger**: v2 rewrite begins.
- **Expected Result**: `middleware.Stage` has 4 values: `BeforeAgent`, `BeforeTool`, `AfterTool`, `AfterAgent`. `Middleware` interface has 4 hook methods + `Name()`. Trace middleware, HTTP trace middleware, and all examples updated.
- **Traces to**: Story 6 / G5.

### FR-7: Tool Set Reduction
- **Description**: Core tools: `bash`, `read`, `write`, `edit`, `glob`, `grep`, `skill`. Delete `todo_write`, `webfetch`, `websearch`, `bash_output`, `bash_status`. Delete task tools. Remove `slashcommand`, `askuserquestion`, `killtask` from core.
- **Trigger**: v2 rewrite begins.
- **Expected Result**: `pkg/tool/builtin/` contains files for exactly 7 tools plus shared helpers (`file_sandbox.go`, `grep_helpers.go`, `grep_params.go`, platform files).
- **Traces to**: Story 7 / G5.

### FR-8: Remove ACP and Tasks
- **Description**: Remove ACP (`pkg/acp`) and task management (`pkg/runtime/tasks`) from core. Remove ACP server mode from CLI. Drop ACP SDK dependency from root `go.mod`.
- **Trigger**: v2 rewrite begins.
- **Expected Result**: No ACP/task code remains in core; `go build ./...` and `go test ./...` pass without ACP/task dependencies.
- **Traces to**: Story 8 / G6.

### FR-9: YOLO Security Default + Safety Hook
- **Description**: Default security mode allows all tool executions (YOLO). A Go-native safety hook (not shell, <1ms overhead) blocks catastrophic operations (`rm -rf /`, `dd`, `mkfs`, `shutdown`, `reboot`, `sudo`). The safety hook runs as a built-in `PreToolUse` handler before user-configured shell hooks. Users can disable via `DisableSafetyHook=true`. The full `pkg/security/` package (permission matcher, approval workflow) is absorbed into `pkg/hooks/safety.go`.
- **Trigger**: Any tool execution in default configuration.
- **Expected Result**: Tool calls succeed without permission prompts by default. The safety hook rejects a defined blocklist of destructive patterns. No `pkg/security/` package exists as a separate module.
- **Traces to**: G2 (simplified runtime) / G7 (dead code removal).

---

## Acceptance Matrix

| ID | Requirement | Priority | How to Verify |
|----|-------------|----------|---------------|
| A1 | Single Model interface, zero bridge adapters | P0 | `grep -r "agent.Model" pkg/` returns 0 matches |
| A2 | Runtime struct <= 7 core fields | P0 | Manual inspection of Runtime struct definition |
| A3 | `pkg/api/agent.go` <= 600 non-test lines | P0 | `wc -l pkg/api/agent.go` |
| A4 | Compaction uses LLM prompt compression and strips tool-call/tool-result content | P0 | Compaction test asserts model compression is invoked and tool content is excluded from compression input |
| A5 | Compaction file <= 200 lines | P1 | `wc -l` on compact file |
| A6 | <= 11 package directories in pkg/ | P0 | `find pkg -maxdepth 1 -type d -mindepth 1 \| wc -l` |
| A7 | 7 event types: PreToolUse, PostToolUse, SessionStart, SessionEnd, Stop, SubagentStart, SubagentStop | P0 | Count `EventType` constants |
| A8 | 4 middleware stages: BeforeAgent, BeforeTool, AfterTool, AfterAgent | P0 | Count `Stage` constants |
| A9 | 7 built-in tools: bash, read, write, edit, grep, glob, skill | P0 | Count tool registrations in default setup |
| A10 | ACP removed from core | P0 | `rg -n \"\\bpkg/acp\\b\" pkg cmd -S` returns 0 and `test ! -d pkg/acp` |
| A11 | Tasks removed from core | P0 | `rg -n \"\\bpkg/runtime/tasks\\b\" pkg cmd -S` returns 0 and `test ! -d pkg/runtime/tasks` |
| A12 | `todo_write` deleted | P0 | `find . -name 'todo_write*'` returns 0 in pkg/ |
| A13 | `go build ./...` passes | P0 | CI green |
| A14 | `go test ./pkg/...` passes | P0 | CI green |
| A15 | Examples require API keys | P1 | Run each example without API keys → error message is clear; with keys → no panic/hang |
| A16 | Total non-test LOC in pkg/ <= 20K | P1 | `find pkg -name '*.go' ! -name '*_test.go' \| xargs wc -l \| tail -1` |

---

## Edge Cases & Failure Handling

- **Case**: Prompt compression compaction encounters a tool transaction that spans the cut boundary.
  - Expected behavior: Move the cut point earlier to avoid splitting the transaction. If all messages are in one transaction, skip compaction.

- **Case**: User registers a custom middleware with old 6-stage interface.
  - Expected behavior: Compilation fails with clear type error. No runtime fallback or silent drop.

- **Failure**: Model provider returns error during streaming.
  - Expected behavior: Error propagates through `CompleteStream` return value. No retry logic in the core (retry is a middleware concern).

- **Recovery**: Runtime.Close() called multiple times.
  - Expected behavior: Idempotent via `sync.Once`. No panic. Second call returns same error as first.

---

## Technical Constraints & Non-Functional Requirements

### Performance
- Prompt compression compaction performs an extra model call only when the compaction trigger fires.
- Runtime initialization completes in <100ms excluding model provider setup.
- Zero additional memory allocations in middleware dispatch hot path compared to v1.

### Security
- YOLO default: all tool executions allowed without prompts.
- Safety hook blocks: `rm -rf /`, `dd if=`, `mkfs`, `fdisk`, `shutdown`, `reboot`, `sudo` (same patterns as current `pkg/security/validator.go`).
- Safety hook is Go-native (function call), not shell-based, for <1ms overhead.
- Sandbox (`pkg/sandbox`) remains available for opt-in stricter isolation.

### Testability
- All external boundaries (HTTP, process execution, clock, filesystem) have minimal seams (single-method interfaces or `var`-injection).
- Core loop is testable with a stub `model.Model` that returns canned responses.
- No test depends on network, API keys, or real LLM responses.

### Compatibility
- Go 1.24+ required (matches current `go.mod`).
- No new external dependencies added. Existing dependency list may shrink after deleting ACP/tasks.

### Integration & Dependencies
- **Anthropic SDK** (`github.com/anthropics/anthropic-sdk-go`): Core dependency for Anthropic provider.
- **OpenAI SDK** (`github.com/openai/openai-go`): Core dependency for OpenAI provider.
- **MCP SDK** (`github.com/modelcontextprotocol/go-sdk`): Core dependency for MCP tool integration.
- **OTel** (`go.opentelemetry.io/otel`): Stays in `middleware/trace`, core dependency.
- **ACP SDK** (`github.com/coder/acp-go-sdk`): Not used in v2 core.

---

## MVP Scope & Delivery

### Phase 1: Core Structural Changes (Must Have)
1. Create new branch.
2. Merge `agent.Model` into `model.Model`. Delete `pkg/agent/` or merge into `pkg/api/`.
3. Strip Runtime struct: remove 10 fields.
4. Replace compaction: prompt compression (<=200 lines), strip tool I/O from compression input, delete old `compact.go`.
5. Merge packages: `core/events` + `core/hooks` -> single package; `core/middleware` -> `middleware`.
6. Reduce events 16 -> 7.
7. Reduce middleware 6 -> 4 stages.

### Phase 2: Tool & Feature Cleanup (Must Have)
8. Delete `todo_write`, `webfetch`, `websearch`, `bash_output`, `bash_status`.
9. Delete task tools and `pkg/runtime/tasks/`.
10. Delete `pkg/acp/` and remove CLI ACP mode.
11. Remove `slashcommand`, `askuserquestion`, `killtask` from core tools. Delete `pkg/runtime/commands/`.
12. Merge prompts: skill templates -> `runtime/skills`; system prompt stays in api.
13. Absorb `pkg/security/` into `pkg/hooks/safety.go` (Go-native safety hook).

### Phase 3: Testing & Validation (Must Have)
13. Rewrite key tests for: Runtime lifecycle, single-prompt run, streaming, tool execution, compaction, middleware, events, context cancellation.
14. Verify all examples compile and run with API keys.
15. Verify line count and package count targets.

### Nice to Have (Later)
- NH1: Performance benchmarks comparing v1 vs v2 initialization and compaction.
- NH2: Migration guide documenting v1 -> v2 API changes (for external users, if any).
- NH4: Structured logging replacing `log.Printf` calls.

---

## Examples and Counterexamples

### Good Outcome Example
After v2, a new developer reads `pkg/api/agent.go` (<=600 lines), sees a Runtime struct with ~7 fields, and understands the entire orchestration. They implement `model.Model` with two methods, register it, and call `runtime.Run()`. Compaction keeps the last N messages and compresses older content while stripping tool I/O so it doesn't dominate context.

### Counterexample
Copying v1's bridge adapter pattern into v2 "for compatibility" — creating `type modelAdapter struct` that wraps `model.Model` into `agent.Model.Generate`. This defeats the purpose of the merge and must not happen.

Another counterexample: keeping `BeforeAgent`/`AfterAgent` middleware stages "just in case" — if no middleware in the codebase or examples uses them, they are dead weight. Delete them.

---

## Risks & Dependencies

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Scope creep: discovering hidden dependencies between packages during merge | Medium | Medium | Strict phase ordering. Phase 1 handles structural changes before tool cleanup. Use `go build ./...` as gate between phases. |
| Removing tools that some users rely on (askuserquestion, skill) | Low | Medium | These tools can be re-added by users as custom tools. Document in migration guide. |
| Prompt compression may change summary wording across runs | Medium | Medium | Keep preserved tail unmodified; bound summary size; make compaction behavior testable with a stub model. |
| Big-bang rewrite takes longer than expected | Medium | High | Phase structure allows shipping Phase 1+2 as useful checkpoint even if Phase 3 (testing polish) is incomplete. |

**Dependencies:**
- No external dependency changes needed for core.
- All work happens on a new branch — zero risk to current main.

---

## Handoff Notes for Implementation and Testing

- **Critical path**: FR-1 (Model merge) must happen first because every other change depends on the simplified interface. FR-2 (Runtime field removal) follows immediately as it defines the new struct shape.
- **Compaction (FR-3)**: Keep compaction small and testable: preserve the tail unmodified, strip tool I/O from the compressed portion, and invoke the model with a dedicated compression prompt.
- **Package merge order matters**: Merge `pkg/agent` into `pkg/api` first (removes the dual-interface problem). Then merge `core/*` packages. Then delete ACP/tasks and the remaining removed tools. This order minimizes intermediate broken states.
- **Testing priority**: Test the agent core loop first (Runtime creation + single run + tool call). Then compaction. Then middleware/events. These three paths cover 80%+ of the codebase.
- **Examples**: After structural changes, update all 12 examples. Most changes are import path updates. Examples 08 (askuserquestion) and 09 (task-system) may need rewriting or removal since those tools are removed.
- **What is NOT ambiguous**: The 10 Runtime fields to remove, the 9 events to delete, the 2 middleware stages to remove, and the tools to delete are all explicitly listed. No judgment calls needed.
- **Fastest verification path**: After each phase, run `go build ./...` and `go test ./pkg/...`. Count lines with `find pkg -name '*.go' ! -name '*_test.go' | xargs wc -l | tail -1`. Count packages with `find pkg -type d | wc -l`.

---

## Appendix: Current vs Target Package Map

| Current Package | Lines | Target |
|----------------|-------|--------|
| `pkg/api` | 5,898 | `pkg/api` (~2,000) — absorbs agent loop |
| `pkg/agent` | 233 | Merged into `pkg/api` |
| `pkg/model` | 2,356 | `pkg/model` (~2,000) — single interface |
| `pkg/tool` | 1,960 | `pkg/tool` (~1,500) |
| `pkg/tool/builtin` | 7,856 | `pkg/tool/builtin` (~4,500) — 7 tools |
| `pkg/middleware` | 2,142 | `pkg/middleware` (~1,800) — 4 stages, absorbs core/middleware |
| `pkg/config` | 1,977 | `pkg/config` (~1,500) |
| `pkg/security` | 1,345 | Absorbed into `pkg/hooks/safety.go` (~100) — YOLO default, safety hook only |
| `pkg/core/events` | 549 | Merged into `pkg/hooks` (~500) |
| `pkg/core/hooks` | 732 | Merged into `pkg/hooks` |
| `pkg/core/middleware` | 24 | Merged into `pkg/middleware` |
| `pkg/runtime/skills` | 1,237 | `pkg/runtime/skills` (~1,000) — absorbs prompt templates |
| `pkg/runtime/subagents` | 850 | `pkg/runtime/subagents` (~700) — adds result summary injection |
| `pkg/runtime/commands` | 828 | **Deleted** — slash commands removed |
| `pkg/runtime/tasks` | 543 | Deleted |
| `pkg/acp` | 2,618 | Deleted |
| `pkg/prompts` | 1,105 | Split: skills part -> runtime/skills; system part -> api |
| `pkg/sandbox` | 429 | `pkg/sandbox` (~400) |
| `pkg/message` | 275 | `pkg/message` (as-is) |
| `pkg/gitignore` | 267 | `pkg/gitignore` (as-is) |
| `pkg/mcp` | 502 | `pkg/mcp` (~450) |
| **Total** | **~33,766** | **Target ~15,000-18,000** |

---

*This PRD was created through interactive requirements gathering and is optimized for autonomous agent consumption, implementation handoff, testability, and scope clarity.*
