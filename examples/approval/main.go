package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cexll/agentsdk-go/pkg/approval"
	security "github.com/cexll/agentsdk-go/pkg/security"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	fmt.Println("=== pkg/approval.Queue demo ===")
	runApprovalDemo()

	fmt.Println("\n=== pkg/security.ApprovalQueue demo ===")
	if err := runSecurityDemo(); err != nil {
		log.Fatalf("security demo: %v", err)
	}
}

// runApprovalDemo wires the approval.Queue, demonstrates manual review, whitelisting, and audit queries.
func runApprovalDemo() {
	store := approval.NewMemoryStore()
	queue := approval.NewQueue(store, approval.NewWhitelist())
	defer func() { _ = queue.Close() }()

	sessionID := "session-approval"
	tool := "builtin.bash"
	params := map[string]any{"command": "rm -rf /tmp/demo"}

	// 1) A tool invocation asks for approval. The queue persists the Record via Store.
	rec, auto, err := queue.Request(sessionID, tool, params)
	if err != nil {
		log.Fatalf("request approval: %v", err)
	}
	log.Printf("request %s queued (auto=%v, decision=%s)", rec.ID, auto, rec.Decision)

	// 2) Review all pending items for the session and approve manually.
	pending := queue.Pending(sessionID)
	for _, p := range pending {
		log.Printf("pending %s -> %s %v", p.ID, p.Tool, p.Params)
	}
	approved, err := queue.Approve(rec.ID, "safe once")
	if err != nil {
		log.Fatalf("approve: %v", err)
	}
	log.Printf("record %s approved at %s", approved.ID, approved.Decided.Format(time.RFC3339))

	// 3) The same tool+params now hits the session whitelist and auto-approves.
	repeat, auto, err := queue.Request(sessionID, tool, params)
	if err != nil {
		log.Fatalf("repeat request: %v", err)
	}
	log.Printf("repeat request %s auto=%v comment=%q", repeat.ID, auto, repeat.Comment)

	// 4) A different command is reviewed and explicitly rejected.
	rejectParams := map[string]any{"command": "curl https://example.com/sh"}
	toReject, auto, err := queue.Request(sessionID, tool, rejectParams)
	if err != nil {
		log.Fatalf("request reject: %v", err)
	}
	log.Printf("suspicious request %s auto=%v decision=%s", toReject.ID, auto, toReject.Decision)
	denied, err := queue.Reject(toReject.ID, "network access not allowed")
	if err != nil {
		log.Fatalf("reject: %v", err)
	}
	log.Printf("record %s rejected with comment %q", denied.ID, denied.Comment)

	// 5) Query the in-memory Store to print the approval history.
	history := store.Query(approval.Filter{SessionID: sessionID})
	log.Printf("history for %s (%d events):", sessionID, len(history))
	for _, h := range history {
		decided := "pending"
		if h.Decided != nil {
			decided = h.Decided.Format(time.RFC3339)
		}
		log.Printf("- %s -> %s (auto=%v) comment=%q decided=%s", h.ID, h.Decision, h.Auto, h.Comment, decided)
	}
}

// runSecurityDemo shows the higher-level security.ApprovalQueue that persists to disk and maintains per-session TTL-based whitelists.
func runSecurityDemo() error {
	dir, err := os.MkdirTemp("", "approval-example")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	storePath := filepath.Join(dir, "approvals.json")
	queue, err := security.NewApprovalQueue(storePath)
	if err != nil {
		return err
	}
	log.Printf("security queue storage: %s", storePath)

	session := "sec-session"
	rec, err := queue.Request(session, "docker run alpine", []string{"/tmp/data"})
	if err != nil {
		return fmt.Errorf("queue request: %w", err)
	}
	log.Printf("security request %s pending at %s", rec.ID, rec.RequestedAt.Format(time.RFC3339))

	// Manually approve and whitelist the session for one minute so subsequent commands auto-pass.
	approved, err := queue.Approve(rec.ID, "alice", time.Minute)
	if err != nil {
		return fmt.Errorf("approve security: %w", err)
	}
	log.Printf("security record %s approved by %s auto=%v", approved.ID, approved.Approver, approved.AutoApproved)

	autoRec, err := queue.Request(session, "docker run alpine", []string{"/tmp/data"})
	if err != nil {
		return fmt.Errorf("repeat security: %w", err)
	}
	log.Printf("repeat security request %s autoApproved=%v", autoRec.ID, autoRec.AutoApproved)

	// Demonstrate a rejection for another session.
	other, err := queue.Request("sec-session-two", "cat /etc/shadow", nil)
	if err != nil {
		return fmt.Errorf("second session request: %w", err)
	}
	denied, err := queue.Deny(other.ID, "bob", "policy violation")
	if err != nil {
		return fmt.Errorf("deny: %w", err)
	}
	log.Printf("security record %s denied by %s reason=%q", denied.ID, denied.Approver, denied.Reason)

	// ListPending confirms no outstanding records and IsWhitelisted shows TTL enforcement.
	log.Printf("pending count=%d", len(queue.ListPending()))
	log.Printf("session %s whitelisted? %v", session, queue.IsWhitelisted(session))
	log.Printf("session %s whitelisted? %v", other.SessionID, queue.IsWhitelisted(other.SessionID))

	return nil
}
