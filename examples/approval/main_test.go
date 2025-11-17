package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/approval"
	security "github.com/cexll/agentsdk-go/pkg/security"
)

type toolInvocation struct {
	session string
	tool    string
	params  map[string]any
}

func newInvocation(session, tool string, params map[string]any) toolInvocation {
	cp := make(map[string]any, len(params))
	for k, v := range params {
		cp[k] = v
	}
	return toolInvocation{session: session, tool: tool, params: cp}
}

func (inv toolInvocation) request(t *testing.T, q *approval.Queue) (approval.Record, bool) {
	t.Helper()
	rec, auto, err := q.Request(inv.session, inv.tool, inv.params)
	if err != nil {
		t.Fatalf("request %s: %v", inv.tool, err)
	}
	return rec, auto
}

func setupQueue(t *testing.T) (*approval.Queue, approval.Store) {
	t.Helper()
	store := approval.NewMemoryStore()
	queue := approval.NewQueue(store, approval.NewWhitelist())
	t.Cleanup(func() { _ = queue.Close() })
	return queue, store
}

func require(t *testing.T, cond bool, format string, args ...any) {
	t.Helper()
	if !cond {
		t.Fatalf(format, args...)
	}
}

func TestApprovalQueueCreation(t *testing.T) {
	queue, store := setupQueue(t)
	req := newInvocation("session-create", "builtin.bash", map[string]any{"command": "echo ok"})
	rec, auto := req.request(t, queue)
	require(t, !auto, "queue creation should start pending")
	require(t, rec.Decision == approval.DecisionPending, "unexpected decision %s", rec.Decision)
	pending := queue.Pending(req.session)
	require(t, len(pending) == 1 && pending[0].ID == rec.ID, "pending mismatch: %+v", pending)
	history := store.Query(approval.Filter{SessionID: req.session})
	require(t, len(history) == 1 && history[0].ID == rec.ID, "store missing queued record: %+v", history)
}

func TestApprovalFlow(t *testing.T) {
	queue, store := setupQueue(t)
	session := "session-flow"
	approveReq := newInvocation(session, "builtin.rm", map[string]any{"command": "rm -rf /tmp"})
	first, auto := approveReq.request(t, queue)
	require(t, !auto, "dangerous request must wait for approval")
	approved, err := queue.Approve(first.ID, "reviewed once")
	require(t, err == nil, "approve: %v", err)
	require(t, approved.Decision == approval.DecisionApproved, "approve mismatch: %+v", approved)
	rejectReq := newInvocation(session, "builtin.curl", map[string]any{"command": "curl https://example.com"})
	second, auto := rejectReq.request(t, queue)
	require(t, !auto, "rejection scenario should be pending first")
	denied, err := queue.Reject(second.ID, "network block")
	require(t, err == nil, "reject: %v", err)
	require(t, denied.Decision == approval.DecisionRejected, "reject mismatch: %+v", denied)
	require(t, len(queue.Pending(session)) == 0, "pending queue should be empty")
	require(t, len(store.Query(approval.Filter{SessionID: session})) == 2, "history should hold two records")
}

func TestWhitelistAutoPass(t *testing.T) {
	queue, _ := setupQueue(t)
	session := "session-whitelist"
	req := newInvocation(session, "builtin.exec", map[string]any{"command": "cat /etc/passwd"})
	first, auto := req.request(t, queue)
	require(t, !auto, "first request must require review")
	_, err := queue.Approve(first.ID, "allowed once")
	require(t, err == nil, "approve: %v", err)
	repeat, auto := req.request(t, queue)
	require(t, auto, "repeat should auto-approve")
	require(t, repeat.Auto && repeat.Comment == "whitelisted", "whitelist metadata mismatch: %+v", repeat)
	require(t, len(queue.Pending(session)) == 0, "no pending entries expected")
}

func TestRecordStore(t *testing.T) {
	queue, store := setupQueue(t)
	session := "session-history"
	approvedRec, _ := newInvocation(session, "builtin.echo", map[string]any{"command": "echo audit"}).request(t, queue)
	_, err := queue.Approve(approvedRec.ID, "audit ok")
	require(t, err == nil, "approve audit: %v", err)
	rejectedRec, _ := newInvocation(session, "builtin.net", map[string]any{"command": "curl http://example"}).request(t, queue)
	_, err = queue.Reject(rejectedRec.ID, "blocked")
	require(t, err == nil, "reject audit: %v", err)
	autoRec, auto := newInvocation(session, "builtin.echo", map[string]any{"command": "echo audit"}).request(t, queue)
	require(t, auto && autoRec.Auto, "auto approval missing")
	history := store.Query(approval.Filter{SessionID: session})
	require(t, len(history) == 3, "expected 3 records, got %d", len(history))
	var haveApproved, haveRejected, haveAuto bool
	for _, rec := range history {
		if rec.Auto {
			haveAuto = true
		}
		if rec.Decision == approval.DecisionApproved && !rec.Auto {
			haveApproved = true
		}
		if rec.Decision == approval.DecisionRejected {
			haveRejected = true
		}
	}
	require(t, haveApproved && haveRejected && haveAuto, "store missing decision states")
}

func TestApprovalDecisions(t *testing.T) {
	dir := t.TempDir()
	logStore, err := approval.NewRecordLog(dir)
	require(t, err == nil, "record log: %v", err)
	t.Cleanup(func() { _ = logStore.Close() })
	queue := approval.NewQueue(logStore, approval.NewWhitelist())
	approvedRec, _ := newInvocation("session-decisions", "builtin.rm", map[string]any{"command": "rm -rf /tmp/demo"}).request(t, queue)
	approved, err := queue.Approve(approvedRec.ID, "safe once")
	require(t, err == nil, "approve: %v", err)
	rejectedRec, _ := newInvocation("session-decisions", "builtin.net", map[string]any{"command": "curl http://blocked"}).request(t, queue)
	denied, err := queue.Reject(rejectedRec.ID, "policy reject")
	require(t, err == nil, "reject: %v", err)
	timeoutRec, _ := newInvocation("session-decisions", "builtin.inspect", map[string]any{"command": "cat /etc/shadow"}).request(t, queue)
	timedOut, err := queue.Timeout(timeoutRec.ID)
	require(t, err == nil, "timeout: %v", err)
	require(t, approved.Decision == approval.DecisionApproved, "approve decision missing")
	require(t, denied.Decision == approval.DecisionRejected, "reject decision missing")
	require(t, timedOut.Decision == approval.DecisionTimeout, "timeout decision missing")
	stats, err := logStore.GC(approval.WithRetentionCount(2))
	require(t, err == nil, "gc: %v", err)
	require(t, stats.Dropped > 0, "expected GC drop")
	require(t, len(logStore.All()) == 2, "GC should retain 2 records")
	t.Run("security TTL and whitelist", func(t *testing.T) {
		secQueue, err := security.NewApprovalQueue(filepath.Join(t.TempDir(), "approvals.json"))
		require(t, err == nil, "security queue: %v", err)
		session := "sec-session"
		first, err := secQueue.Request(session, "docker run alpine", []string{"/tmp/data"})
		require(t, err == nil, "security request: %v", err)
		manual, err := secQueue.Approve(first.ID, "alice", 30*time.Millisecond)
		require(t, err == nil, "security approve: %v", err)
		require(t, manual.State == security.ApprovalApproved && !manual.AutoApproved, "manual approval mismatch: %+v", manual)
		autoRec, err := secQueue.Request(session, "docker run alpine", []string{"/tmp/data"})
		require(t, err == nil, "security repeat: %v", err)
		require(t, autoRec.AutoApproved, "TTL whitelist should auto approve")
		require(t, secQueue.IsWhitelisted(session), "whitelist should be active")
		time.Sleep(50 * time.Millisecond)
		require(t, !secQueue.IsWhitelisted(session), "whitelist TTL should expire")
	})
}
