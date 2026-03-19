package api

import (
	"context"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/message"
	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

func TestRunLoopRejectsNilModel(t *testing.T) {
	rt := &Runtime{}
	prep := preparedRun{
		ctx:        context.Background(),
		prompt:     "hi",
		history:    message.NewHistory(),
		normalized: Request{SessionID: "s"},
	}
	chain := middleware.NewChain(nil)
	_, err := rt.runLoop(prep, nil, &runtimeHookAdapter{}, nil, chain, false)
	if err == nil {
		t.Fatal("expected nil model error")
	}
}

func TestRunLoopPopulatesMiddlewareStateAndHistory(t *testing.T) {
	reg := tool.NewRegistry()
	echo := &echoTool{}
	if err := reg.Register(echo); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	exec := tool.NewExecutor(reg, nil)

	mdl := &stubModel{responses: []*model.Response{
		{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "t1", Name: "echo", Arguments: map[string]any{"text": "hi"}}}}},
		{Message: model.Message{Role: "assistant", Content: "done"}},
	}}

	hist := message.NewHistory()
	hist.Append(message.Message{Role: "system", Content: "intro"})

	rt := &Runtime{registry: reg, executor: exec}
	rtExec := &runtimeToolExecutor{executor: exec, hooks: &runtimeHookAdapter{}, history: hist, host: "localhost"}

	var (
		gotReq  *model.Request
		gotResp *model.Response
	)
	chain := middleware.NewChain([]middleware.Middleware{middleware.Funcs{
		Identifier: "capture",
		OnAfterAgent: func(_ context.Context, st *middleware.State) error {
			if req, ok := st.ModelInput.(*model.Request); ok && req != nil {
				c := *req
				gotReq = &c
			}
			if resp, ok := st.ModelOutput.(*model.Response); ok && resp != nil {
				gotResp = resp
			}
			return nil
		},
	}})

	prep := preparedRun{
		ctx:        context.Background(),
		prompt:     "user",
		history:    hist,
		normalized: Request{SessionID: "s"},
	}
	resp, err := rt.runLoop(prep, mdl, &runtimeHookAdapter{}, rtExec, chain, false)
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if resp == nil || resp.Message.Content != "done" {
		t.Fatalf("resp=%+v", resp)
	}
	if echo.calls != 1 {
		t.Fatalf("tool calls=%d", echo.calls)
	}
	if gotReq == nil || len(gotReq.Messages) == 0 {
		t.Fatalf("expected model request captured, got=%+v", gotReq)
	}
	if gotResp == nil {
		t.Fatal("expected model response captured")
	}
	if hist.Len() < 4 {
		t.Fatalf("expected history to grow, len=%d msgs=%+v", hist.Len(), hist.All())
	}
}

func TestRunLoopEnableCachePassthrough(t *testing.T) {
	hist := message.NewHistory()
	mdl := &stubModel{responses: []*model.Response{{Message: model.Message{Role: "assistant", Content: "ok"}}}}
	rt := &Runtime{}
	chain := middleware.NewChain(nil)
	prep := preparedRun{
		ctx:        context.Background(),
		prompt:     "test",
		history:    hist,
		normalized: Request{SessionID: "s"},
	}

	_, err := rt.runLoop(prep, mdl, &runtimeHookAdapter{}, &runtimeToolExecutor{executor: tool.NewExecutor(nil, nil), hooks: &runtimeHookAdapter{}, host: "localhost"}, chain, true)
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	if len(mdl.requests) == 0 {
		t.Fatal("expected at least one model request")
	}
	if got := mdl.requests[0].EnablePromptCache; !got {
		t.Fatalf("EnablePromptCache=%v", got)
	}
}
