package main

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
)

func TestServeCommandHealthAndRun(t *testing.T) {
	stub := &fakeAgent{
		runFunc: func(ctx context.Context, input string) (*agent.RunResult, error) {
			return &agent.RunResult{Output: input, StopReason: "complete"}, nil
		},
	}
	useAgentFactory(t, stub)
	buf := &syncBuffer{}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serveCommand(ctx, []string{"--host=127.0.0.1", "--port=0"}, cfgPath, ioStreams{out: buf, err: io.Discard})
	}()
	addr := waitForAddress(t, buf, 3*time.Second)
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		cancel()
		t.Fatalf("health request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected health status: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	body := strings.NewReader(`{"input":"demo"}`)
	runResp, err := http.Post("http://"+addr+"/api/run", "application/json", body)
	if err != nil {
		cancel()
		t.Fatalf("run request: %v", err)
	}
	data, _ := io.ReadAll(runResp.Body)
	_ = runResp.Body.Close()
	if runResp.StatusCode != http.StatusOK {
		t.Fatalf("run status %d body %s", runResp.StatusCode, data)
	}
	if !strings.Contains(string(data), "demo") {
		t.Fatalf("missing run output: %s", data)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveCommand error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveCommand did not exit after cancel")
	}
}
