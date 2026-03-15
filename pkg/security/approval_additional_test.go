package security

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// testClock allows controlling time in tests
type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time {
	return c.now
}

func newTestQueue(t *testing.T) (*ApprovalQueue, *testClock) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")
	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}
	clock := &testClock{now: time.Now()}
	q.clock = clock.Now
	return q, clock
}

func TestApprovalQueueWaitErrorsAndNil(t *testing.T) {
	var nilQueue *ApprovalQueue
	if _, err := nilQueue.Wait(context.Background(), "id"); err == nil {
		t.Fatalf("expected error for nil queue")
	}

	q, _ := newTestQueue(t)
	if _, err := q.Wait(context.Background(), "missing"); err == nil {
		t.Fatalf("expected missing approval error")
	}
}

func TestApprovalQueueEnsureCondLocked(t *testing.T) {
	q := &ApprovalQueue{records: map[string]*ApprovalRecord{}, whitelist: map[string]time.Time{}}
	q.ensureCondLocked()
	if q.cond == nil {
		t.Fatalf("expected cond to be initialized")
	}
	q.ensureCondLocked()
}

func TestApprovalQueueApproveDoesNotClearWhitelist(t *testing.T) {
	q, clock := newTestQueue(t)

	// Add session to whitelist first
	if err := q.AddSessionToWhitelist("sess", time.Hour); err != nil {
		t.Fatalf("AddSessionToWhitelist: %v", err)
	}

	rec, err := q.Request("sess", "ls", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	// Approve with TTL=0 - should NOT clear whitelist
	approved, err := q.Approve(rec.ID, "ops", 0)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.ExpiresAt != nil {
		t.Fatalf("expected no expiry for command approval")
	}

	// Session should still be whitelisted
	if !q.IsWhitelisted("sess") {
		t.Fatalf("expected session to still be whitelisted")
	}

	// Advance time past whitelist expiry
	clock.now = clock.now.Add(2 * time.Hour)

	// Now whitelist should be expired
	if q.IsWhitelisted("sess") {
		t.Fatalf("expected whitelist to be expired")
	}
}

func TestApprovalQueueCommandLevelVsSessionLevel(t *testing.T) {
	q, _ := newTestQueue(t)

	// Test 1: Command-level approval
	rec1, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Approve(rec1.ID, "admin", time.Hour); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Same session, different command - should be pending
	rec2, err := q.Request("session-1", "cat", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.State != ApprovalPending {
		t.Errorf("expected pending for different command, got %s", rec2.State)
	}

	// Test 2: Session-level whitelist
	if err := q.AddSessionToWhitelist("session-2", time.Hour); err != nil {
		t.Fatalf("AddSessionToWhitelist: %v", err)
	}

	// Any command for whitelisted session should auto-approve
	rec3, err := q.Request("session-2", "rm -rf /", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec3.State != ApprovalApproved {
		t.Errorf("expected approved for whitelisted session, got %s", rec3.State)
	}
	if !rec3.AutoApproved {
		t.Error("expected auto-approved for whitelisted session")
	}
}

func TestApprovalQueueCommandReuseAfterExpiry(t *testing.T) {
	q, clock := newTestQueue(t)

	// Approve command with short TTL
	rec1, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Approve(rec1.ID, "admin", time.Hour); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Advance time past expiry
	clock.now = clock.now.Add(2 * time.Hour)

	// Same command should now create new pending record
	rec2, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.State != ApprovalPending {
		t.Errorf("expected pending after expiry, got %s", rec2.State)
	}
	if rec2.ID == rec1.ID {
		t.Error("expected new record ID after expiry")
	}
}

func TestApprovalQueueMultipleCommandsSameSession(t *testing.T) {
	q, _ := newTestQueue(t)

	// Create multiple pending commands
	rec1, err := q.Request("session-1", "cmd1", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	rec2, err := q.Request("session-1", "cmd2", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	rec3, err := q.Request("session-1", "cmd3", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Approve only cmd2
	if _, err := q.Approve(rec2.ID, "admin", 0); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Check states
	if rec1.State != ApprovalPending {
		t.Errorf("cmd1 should be pending, got %s", rec1.State)
	}

	// cmd2 should be approved
	approved, ok := q.IsCommandApproved("session-1", "cmd2")
	if !ok || approved == nil || approved.State != ApprovalApproved {
		t.Error("cmd2 should be approved")
	}

	if rec3.State != ApprovalPending {
		t.Errorf("cmd3 should be pending, got %s", rec3.State)
	}
}

func TestApprovalQueueIndefiniteSessionWhitelist(t *testing.T) {
	q, clock := newTestQueue(t)

	// Add session to whitelist without TTL (indefinite)
	if err := q.AddSessionToWhitelist("session-1", 0); err != nil {
		t.Fatalf("AddSessionToWhitelist: %v", err)
	}

	// Should be whitelisted
	if !q.IsWhitelisted("session-1") {
		t.Error("expected session to be whitelisted")
	}

	// Advance time far into the future
	clock.now = clock.now.Add(365 * 24 * time.Hour * 10) // 10 years

	// Should still be whitelisted (indefinite)
	if !q.IsWhitelisted("session-1") {
		t.Error("expected indefinite whitelist to persist")
	}
}

func TestApprovalQueueConcurrentAccess(t *testing.T) {
	q, _ := newTestQueue(t)

	type result struct {
		rec *ApprovalRecord
		err error
	}
	results := make(chan result, 3)

	// Request same command multiple times concurrently
	for i := 0; i < 3; i++ {
		go func() {
			rec, err := q.Request("session-1", "concurrent-cmd", nil)
			results <- result{rec: rec, err: err}
		}()
	}

	// Wait for all goroutines
	var firstID string
	for i := 0; i < 3; i++ {
		res := <-results
		if res.err != nil {
			t.Fatalf("Request: %v", res.err)
		}
		if res.rec == nil {
			t.Fatalf("expected record")
		}
		if res.rec.State != ApprovalPending {
			t.Fatalf("expected pending, got %s", res.rec.State)
		}
		if firstID == "" {
			firstID = res.rec.ID
			continue
		}
		if res.rec.ID != firstID {
			t.Fatalf("expected same record id, got %q and %q", firstID, res.rec.ID)
		}
	}
}
