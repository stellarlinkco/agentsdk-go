package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/workflow"
)

type harness struct {
	req           *PurchaseRequest
	delay         time.Duration
	trace         *trace
	graph         *workflow.Graph
	score         float64
	branch        string
	summaryID     string
	summarySource string
	notifyBarrier *fanInBarrier
}

func newHarness(t *testing.T, req *PurchaseRequest, delay time.Duration) *harness {
	t.Helper()
	h := &harness{req: req, delay: delay, trace: &trace{}, graph: workflow.NewGraph()}
	notifyBranches := []string{"notify_finance", "notify_owner"}
	h.notifyBarrier = newFanInBarrier(len(notifyBranches))
	add := func(n workflow.Node) {
		if err := h.graph.AddNode(n); err != nil {
			t.Fatalf("add %s: %v", n.Name(), err)
		}
	}
	for _, node := range []workflow.Node{
		h.collectNode(),
		h.decisionNode(),
		h.resolutionAction("manual_review", "Approved manually by ops-duty", "human"),
		h.resolutionAction("auto_fulfill", "Auto-fulfilled via policy AUTO-42", "policy-engine"),
		workflow.NewParallel("notify_all", notifyBranches...),
		h.notifier("notify_finance", func(r *PurchaseRequest) string { return "Finance logged " + r.ID }),
		h.notifier("notify_owner", func(r *PurchaseRequest) string { return "Owner " + r.Owner + " notified" }),
		h.summaryNode(),
	} {
		add(node)
	}
	if err := h.graph.SetStart("collect_request"); err != nil {
		t.Fatalf("set start: %v", err)
	}
	for _, edge := range [][2]string{
		{"collect_request", "route_request"},
		{"manual_review", "notify_all"},
		{"auto_fulfill", "notify_all"},
		{"notify_finance", "summarize"},
		{"notify_owner", "summarize"},
	} {
		if err := h.graph.AddTransition(edge[0], edge[1], workflow.Always()); err != nil {
			t.Fatalf("edge %s->%s: %v", edge[0], edge[1], err)
		}
	}
	h.graph.Close()
	return h
}

func (h *harness) collectNode() workflow.Node {
	return workflow.NewAction("collect_request", func(ec *workflow.ExecutionContext) error {
		h.trace.step("collect_request")
		r, err := requestFromContext(ec)
		if err != nil {
			return err
		}
		score := r.Amount/100 + float64(len(r.Priority))*1.5
		ec.Set(ctxKeyScore, score)
		h.score = score
		return nil
	})
}

func (h *harness) decisionNode() workflow.Node {
	return workflow.NewDecision("route_request", func(ec *workflow.ExecutionContext) (string, error) {
		h.trace.step("route_request")
		raw, ok := ec.Get(ctxKeyScore)
		if !ok {
			return "", fmt.Errorf("score missing")
		}
		score, ok := raw.(float64)
		if !ok {
			return "", fmt.Errorf("score type %T", raw)
		}
		r, err := requestFromContext(ec)
		if err != nil {
			return "", err
		}
		next := "auto_fulfill"
		if score >= 25 || strings.EqualFold(r.Priority, "high") {
			next = "manual_review"
		}
		h.branch = next
		return next, nil
	})
}

func (h *harness) resolutionAction(name, resolution, source string) workflow.Node {
	return workflow.NewAction(name, func(ec *workflow.ExecutionContext) error {
		h.trace.step(name)
		h.trace.resolve(resolution)
		ec.Set("resolution_source", source)
		return nil
	})
}

func (h *harness) notifier(name string, build func(*PurchaseRequest) string) workflow.Node {
	return workflow.NewAction(name, func(ec *workflow.ExecutionContext) error {
		h.trace.step(name)
		if h.delay > 0 {
			time.Sleep(h.delay)
		}
		r, err := requestFromContext(ec)
		if err != nil {
			return err
		}
		h.trace.note(build(r))
		return nil
	})
}

func (h *harness) summaryNode() workflow.Node {
	return workflow.NewAction("summarize", func(ec *workflow.ExecutionContext) error {
		h.notifyBarrier.arriveAndWait()
		if !h.trace.completeOnce() {
			return nil
		}
		h.trace.step("summarize")
		r, err := requestFromContext(ec)
		if err != nil {
			return err
		}
		src := "unknown"
		if raw, ok := ec.Get("resolution_source"); ok {
			if v, ok := raw.(string); ok {
				src = v
			}
		}
		h.summaryID = r.ID
		h.summarySource = src
		return nil
	})
}

func (h *harness) run(t *testing.T) {
	t.Helper()
	ex := workflow.NewExecutor(h.graph, workflow.WithInitialData(map[string]any{ctxKeyRequest: h.req}))
	if err := ex.Run(context.Background()); err != nil {
		t.Fatalf("executor run: %v", err)
	}
}

func (h *harness) metrics() (float64, string, string, string) {
	return h.score, h.branch, h.summaryID, h.summarySource
}
