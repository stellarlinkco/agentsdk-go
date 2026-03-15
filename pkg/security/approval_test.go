package security

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApprovalQueue_Request_WhitelistedSession(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Add session to whitelist
	if err := q.AddSessionToWhitelist("session-1", time.Hour); err != nil {
		t.Fatalf("AddSessionToWhitelist: %v", err)
	}

	// Request should auto-approve for whitelisted session
	rec, err := q.Request("session-1", "ls -la", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec.State != ApprovalApproved {
		t.Errorf("expected approved, got %s", rec.State)
	}
	if !rec.AutoApproved {
		t.Error("expected auto-approved")
	}
	if rec.Reason != "session whitelisted" {
		t.Errorf("expected reason 'session whitelisted', got %s", rec.Reason)
	}
}

func TestApprovalQueue_Request_CommandLevelApproval(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// First request - should be pending
	rec1, err := q.Request("session-1", "ls -la", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec1.State != ApprovalPending {
		t.Errorf("expected pending, got %s", rec1.State)
	}

	// Approve the command
	approved, err := q.Approve(rec1.ID, "admin", time.Hour)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.State != ApprovalApproved {
		t.Errorf("expected approved, got %s", approved.State)
	}

	// Same command again - should reuse approved record
	rec2, err := q.Request("session-1", "ls -la", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.State != ApprovalApproved {
		t.Errorf("expected approved (reused), got %s", rec2.State)
	}
	if rec2.ID != rec1.ID {
		t.Error("expected same record ID for same command")
	}

	// Different command - should be pending
	rec3, err := q.Request("session-1", "cat /etc/passwd", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec3.State != ApprovalPending {
		t.Errorf("expected pending for different command, got %s", rec3.State)
	}

	// Different session with same command - should be pending
	rec4, err := q.Request("session-2", "ls -la", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec4.State != ApprovalPending {
		t.Errorf("expected pending for different session, got %s", rec4.State)
	}
}

func TestApprovalQueue_Request_DeniedCommand(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// First request
	rec1, err := q.Request("session-1", "rm -rf /", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Deny the command
	_, err = q.Deny(rec1.ID, "admin", "too dangerous")
	if err != nil {
		t.Fatalf("Deny: %v", err)
	}

	// Same command again - should be rejected
	_, err = q.Request("session-1", "rm -rf /", nil)
	if err == nil {
		t.Error("expected error for denied command")
	}
}

func TestApprovalQueue_Request_PendingReuse(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// First request
	rec1, err := q.Request("session-1", "deploy production", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec1.State != ApprovalPending {
		t.Errorf("expected pending, got %s", rec1.State)
	}

	// Same command again while pending - should return same record
	rec2, err := q.Request("session-1", "deploy production", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.ID != rec1.ID {
		t.Error("expected same record for pending command")
	}
	if rec2.State != ApprovalPending {
		t.Errorf("expected pending, got %s", rec2.State)
	}
}

func TestApprovalQueue_Approve_WithTTL(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Request and approve with TTL
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	approved, err := q.Approve(rec.ID, "admin", time.Hour)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.ExpiresAt == nil {
		t.Error("expected expiration time")
	}

	// Should be able to reuse within TTL
	rec2, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.State != ApprovalApproved {
		t.Errorf("expected approved, got %s", rec2.State)
	}
}

func TestApprovalQueue_Approve_WithoutTTL(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Request and approve without TTL (indefinite)
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	approved, err := q.Approve(rec.ID, "admin", 0)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.ExpiresAt != nil {
		t.Error("expected no expiration time")
	}

	// Should be able to reuse indefinitely
	rec2, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.State != ApprovalApproved {
		t.Errorf("expected approved, got %s", rec2.State)
	}
}

func TestApprovalQueue_WhitelistExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	q.clock = clock.Now

	// Add session to whitelist with short TTL
	if err := q.AddSessionToWhitelist("session-1", time.Second); err != nil {
		t.Fatalf("AddSessionToWhitelist: %v", err)
	}

	// Should be whitelisted immediately
	if !q.IsWhitelisted("session-1") {
		t.Error("expected session to be whitelisted")
	}

	// Advance time past expiry.
	clock.now = clock.now.Add(2 * time.Second)

	// Should no longer be whitelisted
	if q.IsWhitelisted("session-1") {
		t.Error("expected session whitelist to expire")
	}
}

func TestApprovalQueue_RemoveSessionFromWhitelist(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Add and then remove
	if err := q.AddSessionToWhitelist("session-1", time.Hour); err != nil {
		t.Fatalf("AddSessionToWhitelist: %v", err)
	}
	if !q.IsWhitelisted("session-1") {
		t.Error("expected session to be whitelisted")
	}

	if err := q.RemoveSessionFromWhitelist("session-1"); err != nil {
		t.Fatalf("RemoveSessionFromWhitelist: %v", err)
	}
	if q.IsWhitelisted("session-1") {
		t.Error("expected session to not be whitelisted after removal")
	}
}

func TestApprovalQueue_IsCommandApproved(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Not approved initially
	_, ok := q.IsCommandApproved("session-1", "ls")
	if ok {
		t.Error("expected command to not be approved initially")
	}

	// Request and approve
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Approve(rec.ID, "admin", time.Hour); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Now should be approved
	approvedRec, ok := q.IsCommandApproved("session-1", "ls")
	if !ok {
		t.Error("expected command to be approved")
	}
	if approvedRec.State != ApprovalApproved {
		t.Errorf("expected approved state, got %s", approvedRec.State)
	}
}

func TestApprovalQueue_Wait(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Approve in background
	approveErr := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, err := q.Approve(rec.ID, "admin", 0)
		approveErr <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result, err := q.Wait(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if err := <-approveErr; err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if result.State != ApprovalApproved {
		t.Errorf("expected approved, got %s", result.State)
	}
}

func TestApprovalQueue_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	// Create first queue and add data
	q1, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}
	if err := q1.AddSessionToWhitelist("session-1", time.Hour); err != nil {
		t.Fatalf("AddSessionToWhitelist: %v", err)
	}
	rec, err := q1.Request("session-2", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q1.Approve(rec.ID, "admin", time.Hour); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Create new queue instance with same store
	q2, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Verify whitelist persisted
	if !q2.IsWhitelisted("session-1") {
		t.Error("expected whitelist to persist")
	}

	// Verify command approval persisted
	_, ok := q2.IsCommandApproved("session-2", "ls")
	if !ok {
		t.Error("expected command approval to persist")
	}
}

func TestApprovalQueue_ListPending(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Create pending records
	if _, err := q.Request("session-1", "cmd1", nil); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Request("session-1", "cmd2", nil); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Request("session-2", "cmd3", nil); err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Approve one
	rec, err := q.Request("session-1", "cmd4", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Approve(rec.ID, "admin", 0); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	pending := q.ListPending()
	if len(pending) != 3 {
		t.Errorf("expected 3 pending, got %d", len(pending))
	}
}

func TestApprovalQueue_Deny_AlreadyApproved(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Approve(rec.ID, "admin", 0); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	_, err = q.Deny(rec.ID, "admin", "changed mind")
	if err == nil {
		t.Error("expected error when denying already approved record")
	}
}

func TestApprovalQueue_Approve_AlreadyDenied(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Deny(rec.ID, "admin", "no"); err != nil {
		t.Fatalf("Deny: %v", err)
	}

	_, err = q.Approve(rec.ID, "admin", 0)
	if err == nil {
		t.Error("expected error when approving already denied record")
	}
}

func TestApprovalQueue_ApprovalExpiration(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	clock := &testClock{now: time.Unix(1_700_000_000, 0)}
	q.clock = clock.Now

	// Approve with short TTL
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Approve(rec.ID, "admin", time.Second); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Should be approved immediately
	_, ok := q.IsCommandApproved("session-1", "ls")
	if !ok {
		t.Error("expected command to be approved")
	}

	// Advance time past expiration.
	clock.now = clock.now.Add(2 * time.Second)

	// Should no longer be approved
	_, ok = q.IsCommandApproved("session-1", "ls")
	if ok {
		t.Error("expected command approval to expire")
	}

	// New request should create pending record
	rec2, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.State != ApprovalPending {
		t.Errorf("expected pending after expiration, got %s", rec2.State)
	}
}

func TestApprovalQueue_NoStorePath(t *testing.T) {
	q, err := NewApprovalQueue("")
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Should work without persistence
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec.State != ApprovalPending {
		t.Errorf("expected pending, got %s", rec.State)
	}
}

func TestApprovalQueue_InvalidInput(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Empty session ID
	_, err = q.Request("", "ls", nil)
	if err == nil {
		t.Error("expected error for empty session ID")
	}

	// Empty command
	_, err = q.Request("session-1", "", nil)
	if err == nil {
		t.Error("expected error for empty command")
	}

	// Whitespace-only command
	_, err = q.Request("session-1", "   ", nil)
	if err == nil {
		t.Error("expected error for whitespace command")
	}

	// Empty session ID for whitelist
	err = q.AddSessionToWhitelist("", time.Hour)
	if err == nil {
		t.Error("expected error for empty session ID in whitelist")
	}
}

func TestApprovalQueue_Wait_ContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = q.Wait(ctx, rec.ID)
	if err == nil {
		t.Error("expected error when context cancelled")
	}
}

func TestApprovalQueue_NilQueue(t *testing.T) {
	var q *ApprovalQueue

	_, err := q.Wait(context.Background(), "id")
	if err == nil {
		t.Error("expected error for nil queue")
	}
}

func TestApprovalQueue_InvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	_, err = q.Wait(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty ID")
	}

	_, err = q.Wait(context.Background(), "   ")
	if err == nil {
		t.Error("expected error for whitespace ID")
	}
}

func TestApprovalQueue_NonExistentRecord(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	_, err = q.Approve("non-existent", "admin", 0)
	if err == nil {
		t.Error("expected error for non-existent record")
	}

	_, err = q.Deny("non-existent", "admin", "reason")
	if err == nil {
		t.Error("expected error for non-existent record")
	}
}

func TestApprovalQueue_CorruptedStore(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	// Write invalid JSON
	if err := os.WriteFile(storePath, []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := NewApprovalQueue(storePath)
	if err == nil {
		t.Error("expected error for corrupted store")
	}
}

func TestApprovalQueue_GetSessionWhitelistExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Not whitelisted
	_, ok := q.GetSessionWhitelistExpiry("session-1")
	if ok {
		t.Error("expected not found for non-whitelisted session")
	}

	// Add to whitelist
	if err := q.AddSessionToWhitelist("session-1", time.Hour); err != nil {
		t.Fatalf("AddSessionToWhitelist: %v", err)
	}

	expiry, ok := q.GetSessionWhitelistExpiry("session-1")
	if !ok {
		t.Error("expected to find whitelisted session")
	}
	if expiry.IsZero() {
		t.Error("expected non-zero expiry")
	}
}

func TestApprovalQueue_SessionIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "approvals.json")

	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}

	// Approve command for session-1
	rec1, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := q.Approve(rec1.ID, "admin", 0); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Same command for session-2 should still be pending
	rec2, err := q.Request("session-2", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.State != ApprovalPending {
		t.Errorf("expected pending for different session, got %s", rec2.State)
	}

	// Approve for session-2
	if _, err := q.Approve(rec2.ID, "admin", 0); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Both should now be approved independently
	_, ok1 := q.IsCommandApproved("session-1", "ls")
	_, ok2 := q.IsCommandApproved("session-2", "ls")
	if !ok1 || !ok2 {
		t.Error("expected both sessions to have approved command")
	}
}
