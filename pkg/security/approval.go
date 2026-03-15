package security

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ApprovalState is the human approval lifecycle.
type ApprovalState string

const (
	ApprovalPending  ApprovalState = "pending"
	ApprovalApproved ApprovalState = "approved"
	ApprovalDenied   ApprovalState = "denied"
)

// ApprovalRecord captures one approval decision.
type ApprovalRecord struct {
	ID           string        `json:"id"`
	SessionID    string        `json:"session_id"`
	Command      string        `json:"command"`
	Paths        []string      `json:"paths"`
	State        ApprovalState `json:"state"`
	RequestedAt  time.Time     `json:"requested_at"`
	ApprovedAt   *time.Time    `json:"approved_at,omitempty"`
	Approver     string        `json:"approver,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	ExpiresAt    *time.Time    `json:"expires_at,omitempty"`
	AutoApproved bool          `json:"auto_approved"`
}

// IsExpired checks if the approval record has expired
func (r *ApprovalRecord) IsExpired(now time.Time) bool {
	if r.ExpiresAt == nil {
		return false
	}
	return r.ExpiresAt.Before(now)
}

// ApprovalQueue persists approvals and session-level whitelists.
type ApprovalQueue struct {
	mu        sync.Mutex
	cond      *sync.Cond
	storePath string
	records   map[string]*ApprovalRecord
	whitelist map[string]time.Time
	clock     func() time.Time
}

// NewApprovalQueue restores queue state from disk or creates a fresh one.
func NewApprovalQueue(storePath string) (*ApprovalQueue, error) {
	q := &ApprovalQueue{
		storePath: storePath,
		records:   make(map[string]*ApprovalRecord),
		whitelist: make(map[string]time.Time),
		clock:     time.Now,
	}
	q.cond = sync.NewCond(&q.mu)
	if err := q.load(); err != nil {
		return nil, err
	}
	return q, nil
}

// Request enqueues a command for approval.
// Logic:
// 1. If session is in whitelist (and not expired), auto-approve
// 2. Check records for existing session_id + command combination
//   - If found approved (and not expired), auto-approve
//   - If found approved but expired, create new pending record
//   - If found denied, return error
//   - If found pending, return existing record
//
// 3. Otherwise, create new pending record
func (q *ApprovalQueue) Request(sessionID, command string, paths []string) (*ApprovalRecord, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("security: session id required")
	}
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("security: command required")
	}

	sanitized := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		sanitized = append(sanitized, normalizePath(p))
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureCondLocked()

	now := q.clock()

	// 1. Check if session is whitelisted
	if expiry, ok := q.whitelist[sessionID]; ok {
		if expiry.After(now) {
			// Whitelist is valid, auto-approve
			exp := expiry
			record := &ApprovalRecord{
				ID:           newApprovalID(),
				SessionID:    sessionID,
				Command:      command,
				Paths:        sanitized,
				State:        ApprovalApproved,
				RequestedAt:  now,
				ApprovedAt:   &now,
				ExpiresAt:    &exp,
				AutoApproved: true,
				Reason:       "session whitelisted",
			}
			q.records[record.ID] = record
			if err := q.persistLocked(); err != nil {
				return nil, err
			}
			return cloneRecord(record), nil
		}
		// Whitelist expired, clean it up (best-effort persist).
		delete(q.whitelist, sessionID)
		if err := q.persistLocked(); err != nil {
			_ = err
		}
	}

	// 2. Check records for existing session_id + command combination
	for id, rec := range q.records {
		if rec.SessionID == sessionID && rec.Command == command {
			// Auto-approved records are only valid while the session whitelist is active.
			// Since we are here, the session is not currently whitelisted, so drop them.
			if rec.State == ApprovalApproved && rec.AutoApproved {
				delete(q.records, id)
				if err := q.persistLocked(); err != nil {
					_ = err
				}
				continue
			}
			switch rec.State {
			case ApprovalApproved:
				// Check if approval is still valid (not expired)
				if rec.ExpiresAt == nil || rec.ExpiresAt.After(now) {
					// Return existing approved record
					return cloneRecord(rec), nil
				}
				// Expired approval - delete old record (best-effort persist) and create new pending.
				delete(q.records, id)
				if err := q.persistLocked(); err != nil {
					_ = err
				}
				// Continue to create new pending record

			case ApprovalDenied:
				// Command was previously denied - check if we should allow retry
				// Denied commands stay denied permanently
				return nil, fmt.Errorf("security: command '%s' was previously denied for session %s", command, sessionID)

			case ApprovalPending:
				// Return existing pending record
				return cloneRecord(rec), nil
			}
			break // Found matching record, exit loop
		}
	}

	// 3. Create new pending record
	record := &ApprovalRecord{
		ID:          newApprovalID(),
		SessionID:   sessionID,
		Command:     command,
		Paths:       sanitized,
		State:       ApprovalPending,
		RequestedAt: now,
	}

	q.records[record.ID] = record
	if err := q.persistLocked(); err != nil {
		return nil, err
	}
	return cloneRecord(record), nil
}

// Approve marks a pending record as approved.
// Note: This only updates the record state, does NOT add session to whitelist.
// Use AddSessionToWhitelist() to whitelist an entire session.
// TTL parameter sets the expiration time for this specific command approval.
func (q *ApprovalQueue) Approve(id, approver string, ttl time.Duration) (*ApprovalRecord, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureCondLocked()

	rec, ok := q.records[id]
	if !ok {
		return nil, fmt.Errorf("security: approval %s not found", id)
	}
	if rec.State == ApprovalDenied {
		return nil, fmt.Errorf("security: approval %s already denied", id)
	}

	now := q.clock()
	rec.State = ApprovalApproved
	rec.Approver = approver
	rec.Reason = "manual approval"
	rec.AutoApproved = false
	rec.ApprovedAt = &now

	// Set expiration for this specific command approval
	if ttl > 0 {
		expiry := now.Add(ttl)
		rec.ExpiresAt = &expiry
	} else {
		rec.ExpiresAt = nil // No expiration
	}

	if err := q.persistLocked(); err != nil {
		return nil, err
	}
	q.cond.Broadcast()
	return cloneRecord(rec), nil
}

// Deny rejects a pending record.
func (q *ApprovalQueue) Deny(id, approver, reason string) (*ApprovalRecord, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureCondLocked()

	rec, ok := q.records[id]
	if !ok {
		return nil, fmt.Errorf("security: approval %s not found", id)
	}
	if rec.State == ApprovalApproved {
		return nil, fmt.Errorf("security: approval %s already approved", id)
	}

	rec.State = ApprovalDenied
	rec.Approver = approver
	rec.Reason = reason
	rec.ApprovedAt = nil
	rec.ExpiresAt = nil // Denied records don't expire

	if err := q.persistLocked(); err != nil {
		return nil, err
	}
	q.cond.Broadcast()
	return cloneRecord(rec), nil
}

// ListPending returns outstanding approvals for review.
func (q *ApprovalQueue) ListPending() []*ApprovalRecord {
	q.mu.Lock()
	defer q.mu.Unlock()

	var pending []*ApprovalRecord
	for _, rec := range q.records {
		if rec.State == ApprovalPending {
			pending = append(pending, cloneRecord(rec))
		}
	}
	return pending
}

// IsWhitelisted reports whether the session currently bypasses manual review.
func (q *ApprovalQueue) IsWhitelisted(sessionID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureCondLocked()

	expiry, ok := q.whitelist[sessionID]
	if !ok {
		return false
	}
	if expiry.Before(q.clock()) {
		delete(q.whitelist, sessionID)
		if err := q.persistLocked(); err != nil {
			_ = err // best-effort cleanup; in-memory whitelist already expired
		}
		return false
	}
	return true
}

// AddSessionToWhitelist adds a session to the whitelist with optional TTL.
// When TTL is 0, the session is whitelisted indefinitely.
// Once whitelisted, all commands for this session will auto-approve.
func (q *ApprovalQueue) AddSessionToWhitelist(sessionID string, ttl time.Duration) error {
	if sessionID == "" {
		return fmt.Errorf("security: session id required")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureCondLocked()

	now := q.clock()
	if ttl > 0 {
		q.whitelist[sessionID] = now.Add(ttl)
	} else {
		// Use far future time for indefinite whitelist
		q.whitelist[sessionID] = now.Add(365 * 24 * time.Hour * 100) // 100 years
	}

	return q.persistLocked()
}

// RemoveSessionFromWhitelist removes a session from the whitelist.
func (q *ApprovalQueue) RemoveSessionFromWhitelist(sessionID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureCondLocked()

	delete(q.whitelist, sessionID)
	return q.persistLocked()
}

// GetSessionWhitelistExpiry returns the expiry time for a whitelisted session.
// Returns zero time if session is not whitelisted or expired.
func (q *ApprovalQueue) GetSessionWhitelistExpiry(sessionID string) (time.Time, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	expiry, ok := q.whitelist[sessionID]
	if !ok {
		return time.Time{}, false
	}
	if expiry.Before(q.clock()) {
		return time.Time{}, false
	}
	return expiry, true
}

// IsCommandApproved checks if a specific command for a session has been approved.
// Returns the approval record if found and still valid (not expired).
func (q *ApprovalQueue) IsCommandApproved(sessionID, command string) (*ApprovalRecord, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.clock()
	for _, rec := range q.records {
		if rec.SessionID == sessionID && rec.Command == command {
			if rec.AutoApproved {
				return nil, false
			}
			if rec.State == ApprovalApproved {
				// Check if approval is still valid
				if rec.ExpiresAt == nil || rec.ExpiresAt.After(now) {
					return cloneRecord(rec), true
				}
			}
			return nil, false
		}
	}
	return nil, false
}

// Wait blocks until the approval is resolved or the context is cancelled.
func (q *ApprovalQueue) Wait(ctx context.Context, id string) (*ApprovalRecord, error) {
	if q == nil {
		return nil, fmt.Errorf("security: approval queue is nil")
	}
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("security: approval id required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	q.mu.Lock()
	q.ensureCondLocked()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			q.mu.Lock()
			q.ensureCondLocked()
			q.cond.Broadcast()
			q.mu.Unlock()
		case <-done:
		}
	}()
	defer close(done)
	defer q.mu.Unlock()

	for {
		rec, ok := q.records[id]
		if !ok {
			return nil, fmt.Errorf("security: approval %s not found", id)
		}
		if rec.State != ApprovalPending {
			return cloneRecord(rec), nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q.cond.Wait()
	}
}

// CleanupExpired removes expired whitelist entries and approved records.
// This should be called periodically to prevent memory growth.
func (q *ApprovalQueue) CleanupExpired() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ensureCondLocked()

	now := q.clock()
	modified := false

	// Clean up expired whitelist entries
	for sessionID, expiry := range q.whitelist {
		if expiry.Before(now) {
			delete(q.whitelist, sessionID)
			modified = true
		}
	}

	// Clean up expired approved records
	for id, rec := range q.records {
		if rec.State == ApprovalApproved && rec.ExpiresAt != nil && rec.ExpiresAt.Before(now) {
			delete(q.records, id)
			modified = true
		}
	}

	if modified {
		return q.persistLocked()
	}
	return nil
}

func (q *ApprovalQueue) load() error {
	if q.storePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(q.storePath), 0o755); err != nil {
		return fmt.Errorf("security: create approval dir: %w", err)
	}

	data, err := os.ReadFile(q.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("security: load approvals: %w", err)
	}

	var snapshot approvalSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("security: parse approvals: %w", err)
	}

	now := q.clock()

	// Load records, filtering out expired approved records
	for _, rec := range snapshot.Records {
		// Skip expired approved records
		if rec.State == ApprovalApproved && rec.ExpiresAt != nil && rec.ExpiresAt.Before(now) {
			continue
		}
		q.records[rec.ID] = rec
	}

	// Load whitelist, filtering out expired entries
	for session, expiry := range snapshot.Whitelist {
		if expiry.After(now) {
			q.whitelist[session] = expiry
		}
	}

	return nil
}

func (q *ApprovalQueue) persistLocked() error {
	if q.storePath == "" {
		return nil
	}
	snapshot := approvalSnapshot{
		Records:   make([]*ApprovalRecord, 0, len(q.records)),
		Whitelist: make(map[string]time.Time, len(q.whitelist)),
	}
	for _, rec := range q.records {
		snapshot.Records = append(snapshot.Records, rec)
	}
	for session, expiry := range q.whitelist {
		snapshot.Whitelist[session] = expiry
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("security: encode approvals: %w", err)
	}

	tmp := q.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("security: write approvals: %w", err)
	}
	if err := os.Rename(tmp, q.storePath); err != nil {
		return fmt.Errorf("security: atomically replace approvals: %w", err)
	}
	return nil
}

func (q *ApprovalQueue) ensureCondLocked() {
	if q.cond == nil {
		q.cond = sync.NewCond(&q.mu)
	}
}

type approvalSnapshot struct {
	Records   []*ApprovalRecord    `json:"records"`
	Whitelist map[string]time.Time `json:"whitelist"`
}

func newApprovalID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("failover-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func cloneRecord(rec *ApprovalRecord) *ApprovalRecord {
	if rec == nil {
		return nil
	}
	cp := *rec
	if rec.Paths != nil {
		cp.Paths = append([]string(nil), rec.Paths...)
	}
	return &cp
}
