package main

import (
	"math"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/workflow"
)

func TestGraphConstruction(t *testing.T) {
	h := newHarness(t, &PurchaseRequest{ID: "REQ-CONSTRUCT"}, 0)
	if err := h.graph.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if start := h.graph.Start(); start != "collect_request" {
		t.Fatalf("start node %s", start)
	}
	want := map[string]workflow.NodeKind{
		"collect_request": workflow.NodeAction,
		"route_request":   workflow.NodeDecision,
		"manual_review":   workflow.NodeAction,
		"auto_fulfill":    workflow.NodeAction,
		"notify_all":      workflow.NodeParallel,
		"notify_finance":  workflow.NodeAction,
		"notify_owner":    workflow.NodeAction,
		"summarize":       workflow.NodeAction,
	}
	for name, kind := range want {
		node, ok := h.graph.Node(name)
		if !ok || node.Kind() != kind {
			t.Fatalf("node %s missing or wrong kind", name)
		}
	}
}

func TestActionNode(t *testing.T) {
	req := &PurchaseRequest{ID: "REQ-ACTION", Amount: 1400, Priority: "medium", Owner: "ops"}
	h := newHarness(t, req, 0)
	h.run(t)
	score, _, _, _ := h.metrics()
	want := req.Amount/100 + float64(len(req.Priority))*1.5
	if math.Abs(score-want) > 1e-9 {
		t.Fatalf("score got %.2f want %.2f", score, want)
	}
	path, _, _ := h.trace.snapshot()
	if len(path) == 0 || path[0] != "collect_request" {
		t.Fatalf("first step mismatch: %v", path)
	}
}

func TestDecisionNode(t *testing.T) {
	cases := []struct {
		name string
		req  *PurchaseRequest
		want string
	}{
		{"ManualBranch", &PurchaseRequest{ID: "REQ-HIGH", Amount: 1600, Priority: "high", Owner: "ops"}, "manual_review"},
		{"AutoBranch", &PurchaseRequest{ID: "REQ-LOW", Amount: 200, Priority: "low", Owner: "sales"}, "auto_fulfill"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.req, 0)
			h.run(t)
			_, branch, _, _ := h.metrics()
			if branch != tc.want {
				t.Fatalf("branch got %s want %s", branch, tc.want)
			}
		})
	}
}

func TestParallelExecution(t *testing.T) {
	req := &PurchaseRequest{ID: "REQ-PARALLEL", Amount: 400, Priority: "normal", Owner: "growth"}
	h := newHarness(t, req, 40*time.Millisecond)
	start := time.Now()
	h.run(t)
	if elapsed := time.Since(start); elapsed >= 80*time.Millisecond {
		t.Fatalf("parallel branch took %v", elapsed)
	}
	path, _, _ := h.trace.snapshot()
	counts := map[string]int{}
	for _, step := range path {
		counts[step]++
	}
	if counts["notify_finance"] != 1 || counts["notify_owner"] != 1 {
		t.Fatalf("notifications missing: %#v", counts)
	}
}

func TestExecutionContext(t *testing.T) {
	req := &PurchaseRequest{ID: "REQ-CTX", Amount: 500, Priority: "low", Owner: "support"}
	h := newHarness(t, req, 0)
	h.run(t)
	_, _, id, src := h.metrics()
	if id != req.ID || src != "policy-engine" {
		t.Fatalf("summary data lost: id=%s src=%s", id, src)
	}
	path, resolution, notes := h.trace.snapshot()
	if len(path) == 0 || path[len(path)-1] != "summarize" {
		t.Fatalf("path missing summarize: %v", path)
	}
	if resolution == "" || len(notes) == 0 {
		t.Fatalf("trace missing outputs: res=%q notes=%v", resolution, notes)
	}
}
