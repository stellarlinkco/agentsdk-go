package closedloop_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

type scriptedModel struct {
	step int
}

func (m *scriptedModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	var resp *model.Response
	if err := m.CompleteStream(ctx, req, func(sr model.StreamResult) error {
		if sr.Final {
			resp = sr.Response
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("scripted model: no final response")
	}
	return resp, nil
}

func (m *scriptedModel) CompleteStream(_ context.Context, _ model.Request, cb model.StreamHandler) error {
	if cb == nil {
		return errors.New("scripted model: callback required")
	}
	m.step++
	switch m.step {
	case 1:
		return cb(model.StreamResult{
			Final: true,
			Response: &model.Response{
				Message: model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{{
						ID:        "tool_1",
						Name:      "echo",
						Arguments: map[string]any{"text": "hello"},
					}},
				},
				Usage:      model.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
				StopReason: "tool_calls",
			},
		})
	case 2:
		return cb(model.StreamResult{
			Final: true,
			Response: &model.Response{
				Message:    model.Message{Role: "assistant", Content: "done"},
				Usage:      model.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
				StopReason: "stop",
			},
		})
	default:
		return cb(model.StreamResult{
			Final:    true,
			Response: &model.Response{Message: model.Message{Role: "assistant", Content: "done"}},
		})
	}
}

type echoTool struct {
	calls int
}

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "echo input text" }
func (e *echoTool) Schema() *tool.JSONSchema {
	return &tool.JSONSchema{
		Type:     "object",
		Required: []string{"text"},
		Properties: map[string]any{
			"text": map[string]any{"type": "string"},
		},
	}
}

func (e *echoTool) Execute(_ context.Context, params map[string]any) (*tool.ToolResult, error) {
	e.calls++
	text, ok := params["text"].(string)
	if !ok {
		return &tool.ToolResult{Success: false, Output: ""}, nil
	}
	return &tool.ToolResult{Success: true, Output: strings.TrimSpace(text)}, nil
}

func TestClosedLoop_SDKClosedLoopV1_EmitsArtifactsAndVerdict(t *testing.T) {
	runID := "sdk_closed_loop_v1_" + strings.ReplaceAll(t.Name(), "/", "_")

	repoRoot := findRepoRoot(t)
	artifactRoot := t.TempDir()
	if dir := strings.TrimSpace(os.Getenv("CLOSED_LOOP_ARTIFACT_DIR")); dir != "" {
		artifactRoot = dir
	}
	runDir := filepath.Join(artifactRoot, "run")
	if err := os.MkdirAll(filepath.Join(runDir, "request-response"), 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "trace"), 0o755); err != nil {
		t.Fatalf("mkdir trace: %v", err)
	}

	echo := &echoTool{}
	rt, err := api.New(context.Background(), api.Options{
		EntryPoint:   api.EntryPointCLI,
		ProjectRoot:  repoRoot,
		ModelFactory: api.ModelFactoryFunc(func(context.Context) (model.Model, error) { return &scriptedModel{}, nil }),
		Tools:        []tool.Tool{echo},
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	defer rt.Close()

	req := api.Request{
		Prompt:    "trigger",
		SessionID: "closedloop",
		Mode:      api.ModeContext{EntryPoint: api.EntryPointCLI},
	}
	resp, err := rt.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || resp.Result == nil || strings.TrimSpace(resp.Result.Output) != "done" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if echo.calls != 1 {
		t.Fatalf("expected tool to be called once, calls=%d", echo.calls)
	}

	type verdictCheck struct {
		Name     string `json:"name"`
		Blocking bool   `json:"blocking"`
		Pass     bool   `json:"pass"`
		Detail   string `json:"detail,omitempty"`
	}
	type verdict struct {
		Status    string         `json:"status"`
		Slice     string         `json:"slice"`
		RunID     string         `json:"run_id"`
		CreatedAt string         `json:"created_at"`
		Checks    []verdictCheck `json:"checks"`
	}

	v := verdict{
		Status:    "pass",
		Slice:     "sdk_closed_loop_v1",
		RunID:     runID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Checks: []verdictCheck{
			{Name: "run_success", Blocking: true, Pass: true},
			{Name: "tool_call_executed", Blocking: true, Pass: true},
		},
	}

	reqPath := filepath.Join(runDir, "request-response", "request.json")
	respPath := filepath.Join(runDir, "request-response", "response.json")
	if data, err := json.MarshalIndent(req, "", "  "); err == nil {
		if err := os.WriteFile(reqPath, data, 0o600); err != nil {
			t.Fatalf("write request: %v", err)
		}
	}
	if data, err := json.MarshalIndent(resp, "", "  "); err == nil {
		if err := os.WriteFile(respPath, data, 0o600); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}

	verdictPath := filepath.Join(runDir, "verdict.json")
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal verdict: %v", err)
	}
	if err := os.WriteFile(verdictPath, data, 0o600); err != nil {
		t.Fatalf("write verdict: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "REPORT.md"), []byte("closed-loop ok\n"), 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}

	if _, err := os.Stat(verdictPath); err != nil {
		t.Fatalf("verdict missing: %v", err)
	}
	if _, err := os.Stat(reqPath); err != nil {
		t.Fatalf("request missing: %v", err)
	}
	if _, err := os.Stat(respPath); err != nil {
		t.Fatalf("response missing: %v", err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 32 {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("repo root not found from wd")
	return ""
}
