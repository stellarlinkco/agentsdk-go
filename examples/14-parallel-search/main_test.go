package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRunSearchAndFetch(t *testing.T) {
	if parallelEndpoint != "https+http://search.parallel.ai/mcp" {
		t.Fatalf("unexpected production endpoint: %s", parallelEndpoint)
	}
	for _, tc := range []struct {
		name  string
		args  []string
		tool  string
		key   string
		value string
	}{
		{"search", []string{"-query", "Go context cancellation documentation", "-session-id", "test-research"}, "web_search", "search_queries", "Go context cancellation documentation"},
		{"fetch", []string{"-url", "https://go.dev/blog/context", "-session-id", "test-research"}, "web_fetch", "urls", "https://go.dev/blog/context"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture", Version: "1"}, nil)
			var called atomic.Bool
			for _, name := range []string{"web_search", "web_fetch"} {
				properties := map[string]any{"session_id": map[string]any{"type": "string"}}
				required := []string{"urls"}
				properties["urls"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
				if name == "web_search" {
					delete(properties, "urls")
					properties["objective"] = map[string]any{"type": "string"}
					properties["search_queries"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
					required = []string{"objective", "search_queries"}
				}
				server.AddTool(&mcpsdk.Tool{Name: name, InputSchema: map[string]any{"type": "object", "properties": properties, "required": required}}, func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
					if req.Params.Name != tc.tool {
						t.Errorf("tool = %s, want %s", req.Params.Name, tc.tool)
					}
					var params map[string]any
					if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
						t.Error(err)
					}
					values, ok := params[tc.key].([]any)
					if !ok || len(values) != 1 || values[0] != tc.value || params["session_id"] != "test-research" {
						t.Errorf("unexpected arguments: %v", params)
					}
					called.Store(true)
					return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "https://go.dev/blog/context: cancellation documentation"}}}, nil
				})
			}
			handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
			var mu sync.Mutex
			methods := map[string]bool{}
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/mcp" || r.UserAgent() != parallelUserAgent || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected request: %s UA=%q auth=%q", r.URL, r.UserAgent(), r.Header.Get("Authorization"))
				}
				if r.Method == http.MethodPost {
					var buf bytes.Buffer
					if _, err := buf.ReadFrom(r.Body); err != nil {
						t.Error(err)
					}
					_ = r.Body.Close()
					var msg struct {
						Method string `json:"method"`
					}
					if err := json.Unmarshal(buf.Bytes(), &msg); err != nil {
						t.Error(err)
					}
					mu.Lock()
					methods[msg.Method] = true
					mu.Unlock()
					r.Body = io.NopCloser(&buf)
				}
				handler.ServeHTTP(w, r)
			}))
			defer httpServer.Close()
			endpoint := strings.Replace(httpServer.URL, "http://", "http+http://", 1) + "/mcp"
			var out bytes.Buffer
			if err := run(context.Background(), tc.args, &out, endpoint); err != nil {
				t.Fatal(err)
			}
			if !called.Load() || !strings.Contains(out.String(), "https://go.dev/blog/context") {
				t.Fatalf("missing useful output: %s", &out)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, method := range []string{"initialize", "tools/list", "tools/call"} {
				if !methods[method] {
					t.Errorf("missing observed %s request", method)
				}
			}
		})
	}
}

func TestRunCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, nil, &bytes.Buffer{}, parallelEndpoint); err == nil {
		t.Fatal("canceled request succeeded")
	}
}
