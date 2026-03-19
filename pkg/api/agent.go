package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	hooks "github.com/stellarlinkco/agentsdk-go/pkg/hooks"
	"github.com/stellarlinkco/agentsdk-go/pkg/sandbox"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

var newTracer = NewTracer

type streamContextKey string

const streamEmitCtxKey streamContextKey = "agentsdk.stream.emit"

func withStreamEmit(ctx context.Context, emit streamEmitFunc) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if emit == nil {
		return ctx
	}
	return context.WithValue(ctx, streamEmitCtxKey, emit)
}

func streamEmitFromContext(ctx context.Context) streamEmitFunc {
	if ctx == nil {
		return nil
	}
	if emit, ok := ctx.Value(streamEmitCtxKey).(streamEmitFunc); ok {
		return emit
	}
	return nil
}

// Runtime exposes the unified SDK surface that powers CLI/CI/enterprise entrypoints.
type Runtime struct {
	opts      Options
	sbRoot    string
	registry  *tool.Registry
	executor  *tool.Executor
	hooks     *hooks.Executor
	histories *historyStore
	compactor *compactor

	mu sync.RWMutex

	runMu     sync.Mutex
	runWG     sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
	closed    bool
}

// New instantiates a unified runtime bound to the provided options.
func New(ctx context.Context, opts Options) (*Runtime, error) {
	opts = opts.withDefaults()
	opts = opts.frozen()

	// 初始化文件系统抽象层
	fsLayer := config.NewFS(opts.ProjectRoot, opts.EmbedFS)
	opts.fsLayer = fsLayer

	if err := materializeEmbeddedClaudeHooks(opts.ProjectRoot, opts.EmbedFS); err != nil {
		log.Printf("claude hooks materializer warning: %v", err)
	}

	if memory, err := config.LoadAgentsMD(opts.ProjectRoot, fsLayer); err != nil {
		log.Printf("agents.md loader warning: %v", err)
	} else if strings.TrimSpace(memory) != "" {
		if strings.TrimSpace(opts.SystemPrompt) == "" {
			opts.SystemPrompt = fmt.Sprintf("## Memory\n\n%s", strings.TrimSpace(memory))
		} else {
			opts.SystemPrompt = fmt.Sprintf("%s\n\n## Memory\n\n%s", strings.TrimSpace(opts.SystemPrompt), strings.TrimSpace(memory))
		}
	}

	settings, err := loadSettings(opts)
	if err != nil {
		return nil, err
	}
	opts.settingsSnapshot = settings

	mdl, err := resolveModel(ctx, opts)
	if err != nil {
		return nil, err
	}
	opts.Model = mdl

	sbox, sbRoot := buildSandboxManager(opts, settings)

	skReg, skErrs := buildSkillsRegistry(opts)
	for _, err := range skErrs {
		log.Printf("skill loader warning: %v", err)
	}
	opts.skReg = skReg

	subMgr, subErrs := buildSubagentsManager(opts)
	for _, err := range subErrs {
		log.Printf("subagent loader warning: %v", err)
	}
	opts.subMgr = subMgr

	registry := tool.NewRegistry()
	if err := registerTools(registry, opts, settings, opts.skReg); err != nil {
		return nil, err
	}
	mcpServers := collectMCPServers(settings, opts.MCPServers)
	if err := registerMCPServers(ctx, registry, sbox, mcpServers); err != nil {
		return nil, err
	}
	executor := tool.NewExecutor(registry, sbox).WithOutputPersister(tool.NewOutputPersister())

	hooks := newHookExecutor(opts, settings)
	compactor := newCompactor(opts.AutoCompact, opts.TokenLimit)

	tracer, err := newTracer(opts.OTEL)
	if err != nil {
		return nil, fmt.Errorf("otel tracer init: %w", err)
	}
	opts.tracer = tracer

	if opts.RulesEnabled == nil || (opts.RulesEnabled != nil && *opts.RulesEnabled) {
		loader := config.NewRulesLoader(opts.ProjectRoot)
		if _, err := loader.LoadRules(); err != nil {
			log.Printf("rules loader warning: %v", err)
		} else if rules := strings.TrimSpace(loader.GetContent()); rules != "" {
			if strings.TrimSpace(opts.SystemPrompt) == "" {
				opts.SystemPrompt = fmt.Sprintf("## Project Rules\n\n%s", rules)
			} else {
				opts.SystemPrompt = fmt.Sprintf("%s\n\n## Project Rules\n\n%s", strings.TrimSpace(opts.SystemPrompt), rules)
			}
		}
		if err := loader.Close(); err != nil {
			log.Printf("rules loader close warning: %v", err)
		}
	}

	histories := newHistoryStore(opts.MaxSessions)

	rt := &Runtime{
		opts:      opts,
		sbRoot:    sbRoot,
		registry:  registry,
		executor:  executor,
		hooks:     hooks,
		histories: histories,
		compactor: compactor,
	}
	return rt, nil
}

func (rt *Runtime) beginRun() error {
	rt.runMu.Lock()
	defer rt.runMu.Unlock()
	if rt.closed {
		return ErrRuntimeClosed
	}
	rt.runWG.Add(1)
	return nil
}

func (rt *Runtime) endRun() {
	rt.runWG.Done()
}

// Run executes the unified pipeline synchronously.
func (rt *Runtime) Run(ctx context.Context, req Request) (*Response, error) {
	if rt == nil {
		return nil, ErrRuntimeClosed
	}
	if err := rt.beginRun(); err != nil {
		return nil, err
	}
	defer rt.endRun()

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		mode := rt.opts.modeContext()
		sessionID = defaultSessionID(mode.EntryPoint)
	}
	req.SessionID = sessionID

	prep, err := rt.prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	result, err := rt.runAgent(prep)
	if err != nil {
		return nil, err
	}
	return rt.buildResponse(prep, result), nil
}

// RunStream executes the pipeline asynchronously and returns events over a channel.
func (rt *Runtime) RunStream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	if rt == nil {
		return nil, ErrRuntimeClosed
	}
	if strings.TrimSpace(req.Prompt) == "" && len(req.ContentBlocks) == 0 {
		return nil, errors.New("api: prompt is empty")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		mode := rt.opts.modeContext()
		sessionID = defaultSessionID(mode.EntryPoint)
	}
	req.SessionID = sessionID

	if err := rt.beginRun(); err != nil {
		return nil, err
	}

	// 缓冲区增大以吸收前端延迟（逐字符渲染等）导致的背压，避免 progress emit 阻塞工具执行
	out := make(chan StreamEvent, 512)
	progressChan := make(chan StreamEvent, 256)
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	progressMW := newProgressMiddleware(progressChan)
	ctxWithEmit := withStreamEmit(baseCtx, progressMW.streamEmit())
	go func() {
		defer rt.endRun()
		defer close(out)

		prep, err := rt.prepare(ctxWithEmit, req)
		if err != nil {
			isErr := true
			out <- StreamEvent{Type: EventError, Output: err.Error(), IsError: &isErr}
			return
		}

		done := make(chan struct{})
		go func() {
			defer close(done)
			dropping := false
			for event := range progressChan {
				if dropping {
					continue
				}
				select {
				case out <- event:
				case <-ctxWithEmit.Done():
					dropping = true
				}
			}
		}()

		var runErr error
		var result runResult
		defer func() {
			if rt.hooks != nil {
				reason := "completed"
				if runErr != nil {
					reason = "error"
				}
				//nolint:errcheck // session end events are non-critical notifications
				rt.hooks.Publish(hooks.Event{
					Type:      hooks.SessionEnd,
					SessionID: req.SessionID,
					Payload:   hooks.SessionEndPayload{SessionID: req.SessionID, Reason: reason},
				})
			}
		}()

		result, runErr = rt.runAgentWithMiddleware(prep, progressMW)
		close(progressChan)
		<-done

		if runErr != nil {
			isErr := true
			out <- StreamEvent{Type: EventError, Output: runErr.Error(), IsError: &isErr}
			return
		}
		rt.buildResponse(prep, result)
	}()
	return out, nil
}

// Close releases held resources.
func (rt *Runtime) Close() error {
	if rt == nil {
		return nil
	}
	rt.closeOnce.Do(func() {
		rt.runMu.Lock()
		rt.closed = true
		rt.runMu.Unlock()

		rt.runWG.Wait()

		var err error
		if rt.histories != nil {
			for _, sessionID := range rt.histories.SessionIDs() {
				if cleanupErr := cleanupBashOutputSessionDir(sessionID); cleanupErr != nil {
					log.Printf("api: session %q temp cleanup failed: %v", sessionID, cleanupErr)
				}
				if cleanupErr := cleanupToolOutputSessionDir(sessionID); cleanupErr != nil {
					log.Printf("api: session %q tool output cleanup failed: %v", sessionID, cleanupErr)
				}
			}
		}
		if rt.registry != nil {
			rt.registry.Close()
		}
		if rt.opts.tracer != nil {
			if e := rt.opts.tracer.Shutdown(); e != nil {
				err = errors.Join(err, e)
			}
		}
		rt.closeErr = err
	})
	return rt.closeErr
}

// Config returns the last loaded project config.
func (rt *Runtime) Config() *config.Settings {
	if rt == nil {
		return nil
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return projectConfigFromSettings(rt.opts.settingsSnapshot)
}

// Settings exposes the merged settings.json snapshot for callers that need it.
func (rt *Runtime) Settings() *config.Settings {
	if rt == nil {
		return nil
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return config.MergeSettings(nil, rt.opts.settingsSnapshot)
}

// Sandbox exposes the sandbox manager.
func (rt *Runtime) Sandbox() *sandbox.Manager {
	if rt == nil || rt.executor == nil {
		return nil
	}
	return rt.executor.Sandbox()
}

// ----------------- internal helpers -----------------
