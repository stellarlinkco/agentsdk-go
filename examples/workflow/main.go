package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/workflow"
)

const (
	ctxKeyRequest = "request"
	ctxKeyScore   = "risk_score"
)

func main() {
	log.SetFlags(0)
	ctx := context.Background()
	req := &PurchaseRequest{ID: "REQ-2025-11-16", Amount: 1800, Priority: "high", Owner: "platform-team"}
	tr := &trace{}

	graph := workflow.NewGraph()

	// Action node: gather input and seed ExecutionContext data for downstream steps.
	collect := workflow.NewAction("collect_request", func(ec *workflow.ExecutionContext) error {
		tr.step("collect_request")
		r, err := requestFromContext(ec)
		if err != nil {
			return err
		}
		score := r.Amount/100 + float64(len(r.Priority))*1.5
		ec.Set(ctxKeyScore, score)
		log.Printf("[collect_request] id=%s amount=%.2f priority=%s score=%.1f", r.ID, r.Amount, r.Priority, score)
		return nil
	})

	// Decision node: use ExecutionContext data to pick the next Action explicitly.
	route := workflow.NewDecision("route_request", func(ec *workflow.ExecutionContext) (string, error) {
		tr.step("route_request")
		raw, ok := ec.Get(ctxKeyScore)
		if !ok {
			return "", fmt.Errorf("risk score missing")
		}
		score, ok := raw.(float64)
		if !ok {
			return "", fmt.Errorf("risk score type %T", raw)
		}
		r, err := requestFromContext(ec)
		if err != nil {
			return "", err
		}
		if score >= 25 || strings.EqualFold(r.Priority, "high") {
			log.Printf("[route_request] score=%.1f -> manual_review", score)
			return "manual_review", nil
		}
		log.Printf("[route_request] score=%.1f -> auto_fulfill", score)
		return "auto_fulfill", nil
	})

	manual := workflow.NewAction("manual_review", func(ec *workflow.ExecutionContext) error {
		tr.step("manual_review")
		time.Sleep(120 * time.Millisecond)
		tr.resolve("Approved manually by ops-duty")
		ec.Set("resolution_source", "human")
		log.Println("[manual_review] escalation handled by ops-duty")
		return nil
	})

	auto := workflow.NewAction("auto_fulfill", func(ec *workflow.ExecutionContext) error {
		tr.step("auto_fulfill")
		tr.resolve("Auto-fulfilled via policy AUTO-42")
		ec.Set("resolution_source", "policy-engine")
		log.Println("[auto_fulfill] policy AUTO-42 satisfied request")
		return nil
	})

	// Parallel node: split the flow so notifications happen concurrently.
	notifyBranches := []string{"notify_finance", "notify_owner"}
	notifyHub := workflow.NewParallel("notify_all", notifyBranches...)
	notifyBarrier := newFanInBarrier(len(notifyBranches))

	makeNotifier := func(name string, build func(*PurchaseRequest) string) workflow.Node {
		return workflow.NewAction(name, func(ec *workflow.ExecutionContext) error {
			tr.step(name)
			r, err := requestFromContext(ec)
			if err != nil {
				return err
			}
			msg := build(r)
			tr.note(msg)
			log.Printf("[%s] %s", name, msg)
			return nil
		})
	}
	notifyFinance := makeNotifier("notify_finance", func(r *PurchaseRequest) string {
		return fmt.Sprintf("Finance logged %s", r.ID)
	})
	notifyOwner := makeNotifier("notify_owner", func(r *PurchaseRequest) string {
		return fmt.Sprintf("Owner %s notified", r.Owner)
	})

	summary := workflow.NewAction("summarize", func(ec *workflow.ExecutionContext) error {
		notifyBarrier.arriveAndWait()
		if !tr.completeOnce() {
			return nil
		}
		tr.step("summarize")
		if _, err := requestFromContext(ec); err != nil {
			return err
		}
		path, resolution, notes := tr.snapshot()
		fmt.Printf("\n===== Execution Summary =====\nPath: %s\nResolution: %s\nNotifications: %s\n",
			strings.Join(path, " -> "), resolution, strings.Join(notes, ", "))
		return nil
	})

	for _, node := range []workflow.Node{collect, route, manual, auto, notifyHub, notifyFinance, notifyOwner, summary} {
		if err := graph.AddNode(node); err != nil {
			log.Fatalf("add node %s: %v", node.Name(), err)
		}
	}
	if err := graph.SetStart("collect_request"); err != nil {
		log.Fatalf("set start: %v", err)
	}
	// Transition edges express default routing between Action/Parallel nodes.
	add := func(from, to string) {
		if err := graph.AddTransition(from, to, workflow.Always()); err != nil {
			log.Fatalf("transition %s->%s: %v", from, to, err)
		}
	}
	add("collect_request", "route_request")
	add("manual_review", "notify_all")
	add("auto_fulfill", "notify_all")
	add("notify_finance", "summarize")
	add("notify_owner", "summarize")
	graph.Close()

	executor := workflow.NewExecutor(
		graph,
		workflow.WithInitialData(map[string]any{ctxKeyRequest: req}),
	)

	log.Println("starting workflow run...")
	if err := executor.Run(ctx); err != nil {
		log.Fatalf("workflow failed: %v", err)
	}
}
