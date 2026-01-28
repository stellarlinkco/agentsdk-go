package subagents

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
)

func taskDispatchCtx() context.Context {
	return WithTaskDispatch(context.Background())
}

func TestManagerRegisterAndDispatchTarget(t *testing.T) {
	m := NewManager()
	handler := HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
		if subCtx.Model != "sonnet" {
			t.Fatalf("unexpected model propagation: %+v", subCtx)
		}
		return Result{Output: subCtx.SessionID, Metadata: map[string]any{"tools": subCtx.ToolList()}}, nil
	})
	if err := m.Register(Definition{Name: "code", DefaultModel: "sonnet", BaseContext: Context{SessionID: "child"}}, handler); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := m.Register(Definition{Name: "code"}, handler); !errors.Is(err, ErrDuplicateSubagent) {
		t.Fatalf("expected duplicate error")
	}

	res, err := m.Dispatch(taskDispatchCtx(), Request{Target: "code", Instruction: "run", ToolWhitelist: []string{"bash"}})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	tools, ok := res.Metadata["tools"].([]string)
	if !ok {
		t.Fatalf("expected tools metadata slice, got %T", res.Metadata["tools"])
	}
	if res.Subagent != "code" || res.Output != "child" || tools[0] != "bash" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestManagerDispatchRequiresTaskToolSource(t *testing.T) {
	m := NewManager()
	if err := m.Register(Definition{Name: "code"}, HandlerFunc(func(context.Context, Context, Request) (Result, error) {
		return Result{Output: "ok"}, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := m.Dispatch(context.Background(), Request{Target: "code", Instruction: "run"}); !errors.Is(err, ErrDispatchUnauthorized) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if _, err := m.Dispatch(taskDispatchCtx(), Request{Target: "code", Instruction: "run"}); err != nil {
		t.Fatalf("authorized dispatch failed: %v", err)
	}
}

func TestManagerAutoMatchPriorityAndMutex(t *testing.T) {
	m := NewManager()
	errorHandler := HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
		return Result{}, errors.New("boom")
	})
	matcher := skills.KeywordMatcher{Any: []string{"deploy", "ops"}}
	if err := m.Register(Definition{Name: "low", Priority: 1, Matchers: []skills.Matcher{matcher}}, errorHandler); err != nil {
		t.Fatalf("register low: %v", err)
	}
	if err := m.Register(Definition{Name: "high", Priority: 2, MutexKey: "env", Matchers: []skills.Matcher{matcher}}, HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
		return Result{Output: "ok"}, nil
	})); err != nil {
		t.Fatalf("register high: %v", err)
	}
	if err := m.Register(Definition{Name: "other", Priority: 3, MutexKey: "env", Matchers: []skills.Matcher{skills.KeywordMatcher{Any: []string{"other"}}}}, HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
		return Result{Output: "other"}, nil
	})); err != nil {
		t.Fatalf("register other: %v", err)
	}

	res, err := m.Dispatch(taskDispatchCtx(), Request{Instruction: "deploy", Activation: skills.ActivationContext{Prompt: "deploy prod"}})
	if err != nil {
		t.Fatalf("dispatch match failed: %v", err)
	}
	if res.Subagent != "high" {
		t.Fatalf("expected high priority selection, got %s", res.Subagent)
	}

	_, err = m.Dispatch(taskDispatchCtx(), Request{Instruction: "deploy", Activation: skills.ActivationContext{Prompt: "missing"}})
	if !errors.Is(err, ErrNoMatchingSubagent) {
		t.Fatalf("expected no match error, got %v", err)
	}

	_, err = m.Dispatch(taskDispatchCtx(), Request{Instruction: "", Activation: skills.ActivationContext{Prompt: "deploy"}})
	if !errors.Is(err, ErrEmptyInstruction) {
		t.Fatalf("expected empty instruction error")
	}
}

func TestManagerUnknownTarget(t *testing.T) {
	m := NewManager()
	if _, err := m.Dispatch(taskDispatchCtx(), Request{Target: "missing", Instruction: "run"}); !errors.Is(err, ErrUnknownSubagent) {
		t.Fatalf("expected unknown target error")
	}

	// coverage for selectTarget manual path
	handler := HandlerFunc(func(context.Context, Context, Request) (Result, error) {
		return Result{Output: "ok"}, nil
	})
	if err := m.Register(Definition{Name: "direct"}, handler); err != nil {
		t.Fatalf("register direct: %v", err)
	}
	res, err := m.Dispatch(taskDispatchCtx(), Request{Target: "direct", Instruction: "run"})
	if err != nil || res.Subagent != "direct" {
		t.Fatalf("expected direct dispatch, got %v %v", res, err)
	}
}

func TestManagerListAndDefinitionClone(t *testing.T) {
	m := NewManager()
	base := Context{SessionID: "root", Metadata: map[string]any{"a": 1}, ToolWhitelist: []string{"bash"}}
	handler := HandlerFunc(func(context.Context, Context, Request) (Result, error) {
		return Result{}, nil
	})
	if err := m.Register(Definition{Name: "list", BaseContext: base}, HandlerFunc(func(context.Context, Context, Request) (Result, error) {
		return Result{}, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}
	list := m.List()
	if len(list) != 1 || list[0].Name != "list" {
		t.Fatalf("unexpected list result: %+v", list)
	}
	list[0].BaseContext.Metadata["a"] = 2
	list[0].Matchers = nil
	refreshed := m.List()
	if refreshed[0].BaseContext.Metadata["a"] != 1 {
		t.Fatalf("context clone failed: %+v", refreshed[0])
	}

	// ensure mutex filtering path keeps first entry when same priority
	if err := m.Register(Definition{Name: "mutex-a", Priority: 1, MutexKey: "env"}, handler); err != nil {
		t.Fatalf("register mutex-a: %v", err)
	}
	if err := m.Register(Definition{Name: "mutex-b", Priority: 1, MutexKey: "env"}, handler); err != nil {
		t.Fatalf("register mutex-b: %v", err)
	}
	match := m.matching(skills.ActivationContext{})
	if len(match) == 0 {
		t.Fatalf("expected at least one match")
	}
}

func TestManagerValidationAndGuards(t *testing.T) {
	if err := (Definition{Name: "bad name"}).Validate(); err == nil {
		t.Fatalf("expected validation error for spaces")
	}
	var fn HandlerFunc
	if _, err := fn.Handle(context.Background(), Context{}, Request{}); err == nil {
		t.Fatalf("nil handler func should error")
	}

	m := NewManager()
	if err := m.Register(Definition{Name: "ok"}, nil); err == nil {
		t.Fatalf("expected nil handler rejection")
	}
	if err := m.Register(Definition{Name: "prio-high", Priority: -1}, HandlerFunc(func(context.Context, Context, Request) (Result, error) {
		return Result{}, nil
	})); err != nil {
		t.Fatalf("register prio-high: %v", err)
	}
	if err := m.Register(Definition{Name: "prio-low", Priority: 1}, HandlerFunc(func(context.Context, Context, Request) (Result, error) {
		return Result{}, nil
	})); err != nil {
		t.Fatalf("register prio-low: %v", err)
	}
	list := m.List()
	if len(list) != 2 || list[0].Name != "prio-low" || list[0].Priority != 1 {
		t.Fatalf("expected list order by priority desc, got %+v", list)
	}

	// Dispatch should merge metadata into cloned context
	if err := m.Register(Definition{Name: "meta"}, HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
		if subCtx.Metadata["k"] != "v" {
			t.Fatalf("metadata not merged")
		}
		return Result{}, nil
	})); err != nil {
		t.Fatalf("register meta: %v", err)
	}
	if _, err := m.Dispatch(taskDispatchCtx(), Request{Target: "meta", Instruction: "run", Metadata: map[string]any{"k": "v"}}); err != nil {
		t.Fatalf("dispatch meta: %v", err)
	}
}

func TestManagerDispatchBuiltinTypeContext(t *testing.T) {
	m := NewManager()
	type expectation struct {
		name    string
		model   string
		allowed []string
		blocked []string
	}
	cases := []expectation{
		{
			name:    TypeGeneralPurpose,
			model:   ModelSonnet,
			allowed: []string{"bash", "grep"},
		},
		{
			name:    TypeExplore,
			model:   ModelHaiku,
			allowed: []string{"glob", "grep", "read"},
			blocked: []string{"bash", "write"},
		},
		{
			name:    TypePlan,
			model:   ModelSonnet,
			allowed: []string{"task_create", "web_fetch"},
		},
	}

	captured := map[string]Context{}
	for _, tc := range cases {
		def, ok := BuiltinDefinition(tc.name)
		if !ok {
			t.Fatalf("builtin definition %s missing", tc.name)
		}
		testCase := tc
		if err := m.Register(def, HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
			captured[testCase.name] = subCtx
			return Result{Output: testCase.name}, nil
		})); err != nil {
			t.Fatalf("register %s: %v", tc.name, err)
		}
	}

	for _, tc := range cases {
		res, err := m.Dispatch(taskDispatchCtx(), Request{Target: tc.name, Instruction: "inspect"})
		if err != nil {
			t.Fatalf("dispatch %s: %v", tc.name, err)
		}
		if res.Subagent != tc.name {
			t.Fatalf("expected subagent %s, got %s", tc.name, res.Subagent)
		}
		subCtx, ok := captured[tc.name]
		if !ok {
			t.Fatalf("missing captured context for %s", tc.name)
		}
		if subCtx.Model != tc.model {
			t.Fatalf("expected model %s for %s, got %s", tc.model, tc.name, subCtx.Model)
		}
		for _, tool := range tc.allowed {
			if !subCtx.Allows(tool) {
				t.Fatalf("%s should allow %s", tc.name, tool)
			}
		}
		for _, tool := range tc.blocked {
			if subCtx.Allows(tool) {
				t.Fatalf("%s should block %s", tc.name, tool)
			}
		}
	}
}

func TestManagerDispatchRespectsExplicitWhitelist(t *testing.T) {
	m := NewManager()
	def, ok := BuiltinDefinition(TypeExplore)
	if !ok {
		t.Fatal("missing explore definition")
	}
	var captured Context
	if err := m.Register(def, HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
		captured = subCtx
		return Result{Output: "ok"}, nil
	})); err != nil {
		t.Fatalf("register explore: %v", err)
	}

	_, err := m.Dispatch(taskDispatchCtx(), Request{
		Target:        TypeExplore,
		Instruction:   "scan repo",
		ToolWhitelist: []string{"read", "write", "glob"},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	tools := captured.ToolList()
	want := map[string]struct{}{"glob": {}, "read": {}}
	if len(tools) != len(want) {
		t.Fatalf("expected %d tools after restriction, got %v", len(want), tools)
	}
	for _, tool := range tools {
		if _, ok := want[tool]; !ok {
			t.Fatalf("unexpected tool %s in whitelist %v", tool, tools)
		}
		delete(want, tool)
	}
	if len(want) != 0 {
		t.Fatalf("missing tools after restriction: %v", want)
	}
}

func TestBuiltinDefinitionsCatalog(t *testing.T) {
	defs := BuiltinDefinitions()
	if len(defs) != 3 {
		t.Fatalf("expected three builtin definitions, got %d", len(defs))
	}
	indexed := map[string]Definition{}
	for _, def := range defs {
		indexed[def.Name] = def
	}
	if gp, ok := indexed[TypeGeneralPurpose]; !ok || gp.DefaultModel != ModelSonnet || len(gp.BaseContext.ToolWhitelist) != 0 {
		t.Fatalf("unexpected general-purpose definition: %+v", gp)
	}
	if plan, ok := indexed[TypePlan]; !ok || plan.DefaultModel != ModelSonnet {
		t.Fatalf("unexpected plan definition: %+v", plan)
	}
	explore, ok := BuiltinDefinition(TypeExplore)
	if !ok || explore.DefaultModel != ModelHaiku || len(explore.BaseContext.ToolWhitelist) != 3 {
		t.Fatalf("unexpected explore definition: %+v", explore)
	}
	mutated, _ := BuiltinDefinition(TypeExplore)
	mutated.BaseContext.ToolWhitelist = nil
	snapshot, _ := BuiltinDefinition(TypeExplore)
	if len(snapshot.BaseContext.ToolWhitelist) != 3 {
		t.Fatalf("expected definition cloning to protect catalog, got %+v", snapshot.BaseContext.ToolWhitelist)
	}
}

func TestDispatchHandlerErrorSetsResultAndDefaults(t *testing.T) {
	m := NewManager()
	handlerErr := errors.New("boom")
	if err := m.Register(Definition{Name: "err", DefaultModel: ModelHaiku}, HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
		if subCtx.Model != ModelHaiku {
			t.Fatalf("expected default model %s, got %s", ModelHaiku, subCtx.Model)
		}
		if subCtx.SessionID != "sess" {
			t.Fatalf("session id not propagated: %+v", subCtx)
		}
		return Result{Metadata: map[string]any{"k": "v"}}, handlerErr
	})); err != nil {
		t.Fatalf("register err handler: %v", err)
	}

	ctx := WithDispatchSource(context.TODO(), DispatchSourceTaskTool)
	res, err := m.Dispatch(ctx, Request{
		Target:      "err",
		Instruction: "do work",
		Metadata:    map[string]any{"session_id": "  sess "},
	})
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error, got %v", err)
	}
	if res.Error == "" || res.Subagent != "err" {
		t.Fatalf("unexpected result on error path: %+v", res)
	}
	if res.Metadata["k"] != "v" {
		t.Fatalf("metadata should be cloned back: %+v", res.Metadata)
	}
}

func TestContextHelpersAndFromContext(t *testing.T) {
	var c Context
	if c2 := c.WithMetadata(map[string]any{}); c2.Metadata != nil {
		t.Fatalf("empty metadata should not allocate: %+v", c2.Metadata)
	}
	c = c.WithMetadata(map[string]any{"k": "v"})
	if c.Metadata["k"] != "v" {
		t.Fatalf("metadata merge failed: %+v", c.Metadata)
	}
	if c.WithSession("   ").SessionID != "" {
		t.Fatalf("blank session id should be ignored")
	}
	withSession := c.WithSession(" session ")
	if withSession.SessionID != "session" {
		t.Fatalf("session trimming failed: %q", withSession.SessionID)
	}

	restricted := Context{ToolWhitelist: []string{"bash", "read"}}
	restricted = restricted.RestrictTools(" read ", "", "write")
	tools := restricted.ToolList()
	if len(tools) != 1 || tools[0] != "read" {
		t.Fatalf("unexpected restricted tools: %v", tools)
	}
	toolSet := toToolSet([]string{"", "READ", "read"})
	if _, ok := toolSet["read"]; !ok || len(toolSet) != 1 {
		t.Fatalf("toToolSet should ignore blanks and dedupe: %+v", toolSet)
	}

	if _, ok := FromContext(context.TODO()); ok {
		t.Fatalf("nil context should not return subagent context")
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Fatalf("missing value should not be found")
	}
	injected := WithContext(context.Background(), withSession)
	extracted, ok := FromContext(injected)
	if !ok || extracted.SessionID != "session" || extracted.Metadata["k"] != "v" {
		t.Fatalf("extracted context mismatch: %+v ok=%v", extracted, ok)
	}
	extracted.Metadata["k"] = "mutated"
	recovered, _ := FromContext(injected)
	if recovered.Metadata["k"] != "v" {
		t.Fatalf("context should be cloned on read: %+v", recovered.Metadata)
	}
}

func TestDispatchSourceHelpers(t *testing.T) {
	if dispatchSource(context.TODO()) != "" {
		t.Fatalf("nil context should return empty source")
	}
	ctx := WithDispatchSource(context.Background(), "")
	if dispatchSource(ctx) != "" {
		t.Fatalf("empty source should not be stored")
	}
	ctx = WithDispatchSource(context.TODO(), " task_tool ")
	if dispatchSource(ctx) != DispatchSourceTaskTool {
		t.Fatalf("task source should be normalized")
	}
	ctx = context.WithValue(context.Background(), dispatchSourceKey{}, 123)
	if dispatchSource(ctx) != "" {
		t.Fatalf("non-string source should be ignored")
	}
	if _, ok := BuiltinDefinition("missing"); ok {
		t.Fatalf("unknown builtin should report false")
	}
	if err := (Definition{}).Validate(); err == nil {
		t.Fatalf("blank name should fail validation")
	}
}

func TestManagerDispatchConcurrent(t *testing.T) {
	m := NewManager()
	var counter int32
	if err := m.Register(Definition{Name: "worker"}, HandlerFunc(func(ctx context.Context, subCtx Context, req Request) (Result, error) {
		atomic.AddInt32(&counter, 1)
		return Result{Output: req.Instruction}, nil
	})); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	dispatchCtx := WithTaskDispatch(context.Background())
	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			res, err := m.Dispatch(dispatchCtx, Request{Target: "worker", Instruction: "run"})
			if err != nil {
				errs <- err
				return
			}
			if res.Subagent != "worker" || res.Output != "run" {
				errs <- errors.New("unexpected dispatch result")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent dispatch failed: %v", err)
		}
	}
	if atomic.LoadInt32(&counter) != workers {
		t.Fatalf("expected %d handler invocations, got %d", workers, counter)
	}
}
