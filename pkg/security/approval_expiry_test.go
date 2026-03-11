package security

import (
	"testing"
	"time"
)

// TestWhitelistExpiryInRequest 测试 Request 时 whitelist 过期的情况
func TestWhitelistExpiryInRequest(t *testing.T) {
	q, clock := newTestQueue(t)

	// 添加已过期 whitelist
	q.whitelist["session-1"] = clock.now.Add(-time.Hour)

	// Request 应该清理过期 whitelist 并创建 pending
	rec, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec.State != ApprovalPending {
		t.Errorf("expected pending after whitelist expiry, got %s", rec.State)
	}

	// Whitelist 应该被清理
	if q.IsWhitelisted("session-1") {
		t.Error("expired whitelist should be cleaned up")
	}
}

// TestApprovedRecordExpiryInRequest 测试 approved 记录过期后重新创建
func TestApprovedRecordExpiryInRequest(t *testing.T) {
	q, clock := newTestQueue(t)

	// 创建并批准命令（短 TTL）
	rec1, _ := q.Request("session-1", "ls", nil)
	q.Approve(rec1.ID, "admin", time.Hour)

	// 验证已批准
	rec2, _ := q.Request("session-1", "ls", nil)
	if rec2.State != ApprovalApproved {
		t.Errorf("expected approved, got %s", rec2.State)
	}
	if rec2.ID != rec1.ID {
		t.Error("expected same record")
	}

	// 时间前进，使批准过期
	clock.now = clock.now.Add(2 * time.Hour)

	// 再次请求应该创建新的 pending
	rec3, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec3.State != ApprovalPending {
		t.Errorf("expected pending after approval expiry, got %s", rec3.State)
	}
	if rec3.ID == rec1.ID {
		t.Error("expected new record after expiry")
	}
}

// TestLoadFiltersExpiredData 测试加载时过滤过期数据
func TestLoadFiltersExpiredData(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := tmpDir + "/approvals.json"

	// 创建队列并添加数据
	q1, _ := newTestQueueWithPath(t, storePath)
	clock := &testClock{now: time.Now()}
	q1.clock = clock.Now

	// 添加未过期 whitelist
	q1.AddSessionToWhitelist("session-valid", time.Hour)
	// 添加已过期 whitelist（手动设置过去时间）
	q1.whitelist["session-expired"] = clock.now.Add(-time.Hour)

	// 创建未过期批准
	rec1, _ := q1.Request("session-1", "valid-cmd", nil)
	q1.Approve(rec1.ID, "admin", time.Hour)

	// 创建已过期批准（手动设置过去时间）
	rec2, _ := q1.Request("session-1", "expired-cmd", nil)
	q1.Approve(rec2.ID, "admin", time.Hour)
	// 手动修改过期时间到过去
	pastTime := clock.now.Add(-2 * time.Hour)
	q1.records[rec2.ID].ExpiresAt = &pastTime

	// 强制持久化
	q1.mu.Lock()
	q1.persistLocked()
	q1.mu.Unlock()

	// 创建新队列实例加载数据
	q2, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}
	q2.clock = clock.Now

	// 验证过期数据被过滤
	if q2.IsWhitelisted("session-expired") {
		t.Error("expired whitelist should be filtered on load")
	}
	// session-valid 应该仍然有效（未过期）
	if !q2.IsWhitelisted("session-valid") {
		t.Error("session-valid should still be whitelisted")
	}

	// 过期命令应该需要重新审批
	_, ok := q2.IsCommandApproved("session-1", "expired-cmd")
	if ok {
		t.Error("expired command approval should be filtered on load")
	}

	// 未过期命令应该仍然有效
	_, ok = q2.IsCommandApproved("session-1", "valid-cmd")
	if !ok {
		t.Error("valid command approval should persist")
	}
}

// TestCleanupExpired 测试 CleanupExpired 方法
func TestCleanupExpired(t *testing.T) {
	q, clock := newTestQueue(t)

	// 添加各种数据
	q.AddSessionToWhitelist("session-expired", time.Hour)
	q.AddSessionToWhitelist("session-valid", time.Hour*24)

	rec1, _ := q.Request("session-1", "expired-cmd", nil)
	q.Approve(rec1.ID, "admin", time.Hour)

	rec2, _ := q.Request("session-1", "valid-cmd", nil)
	q.Approve(rec2.ID, "admin", time.Hour*24)

	rec3, _ := q.Request("session-1", "pending-cmd", nil)

	rec4, _ := q.Request("session-1", "denied-cmd", nil)
	q.Deny(rec4.ID, "admin", "no")

	// 时间前进使部分数据过期
	clock.now = clock.now.Add(2 * time.Hour)

	// 执行清理
	if err := q.CleanupExpired(); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}

	// 验证过期数据被清理
	if q.IsWhitelisted("session-expired") {
		t.Error("expired whitelist should be cleaned")
	}
	if !q.IsWhitelisted("session-valid") {
		t.Error("valid whitelist should remain")
	}

	_, ok := q.IsCommandApproved("session-1", "expired-cmd")
	if ok {
		t.Error("expired command approval should be cleaned")
	}

	_, ok = q.IsCommandApproved("session-1", "valid-cmd")
	if !ok {
		t.Error("valid command approval should remain")
	}

	// Pending 和 Denied 记录不应该被清理
	_, ok = q.records[rec3.ID]
	if !ok {
		t.Error("pending record should not be cleaned")
	}

	_, ok = q.records[rec4.ID]
	if !ok {
		t.Error("denied record should not be cleaned")
	}
}

// TestApprovedRecordExpiryEdgeCases 测试批准过期的边界情况
func TestApprovedRecordExpiryEdgeCases(t *testing.T) {
	q, clock := newTestQueue(t)

	// 测试无过期时间的批准（永久有效）
	rec1, _ := q.Request("session-1", "forever-cmd", nil)
	q.Approve(rec1.ID, "admin", 0) // TTL = 0，永久有效

	// 时间前进很远
	clock.now = clock.now.Add(365 * 24 * time.Hour * 10) // 10年后

	_, ok := q.IsCommandApproved("session-1", "forever-cmd")
	if !ok {
		t.Error("permanent approval should not expire")
	}

	// 测试刚好在边界的情况
	rec2, _ := q.Request("session-2", "boundary-cmd", nil)
	q.Approve(rec2.ID, "admin", time.Hour)

	// 刚好在过期时间点
	clock.now = clock.now.Add(time.Hour)

	// 此时应该刚好过期（ExpiresAt.Before(now) 为 true）
	_, ok = q.IsCommandApproved("session-2", "boundary-cmd")
	if ok {
		t.Error("approval should be expired at exact boundary")
	}
}

// TestGetSessionWhitelistExpiryWithExpired 测试获取过期 whitelist
func TestGetSessionWhitelistExpiryWithExpired(t *testing.T) {
	q, clock := newTestQueue(t)

	// 添加已过期 whitelist
	q.whitelist["session-1"] = clock.now.Add(-time.Hour)

	// 应该返回 false
	_, ok := q.GetSessionWhitelistExpiry("session-1")
	if ok {
		t.Error("expired whitelist should return false")
	}
}

// TestIsCommandApprovedWithExpired 测试过期命令批准检查
func TestIsCommandApprovedWithExpired(t *testing.T) {
	q, clock := newTestQueue(t)

	// 创建并批准（短 TTL）
	rec, _ := q.Request("session-1", "ls", nil)
	q.Approve(rec.ID, "admin", time.Hour)

	// 验证有效
	_, ok := q.IsCommandApproved("session-1", "ls")
	if !ok {
		t.Error("should be approved")
	}

	// 时间前进使过期
	clock.now = clock.now.Add(2 * time.Hour)

	// 应该返回 false
	_, ok = q.IsCommandApproved("session-1", "ls")
	if ok {
		t.Error("should not be approved after expiry")
	}
}

// TestMultipleCommandsWithDifferentExpiry 测试多个命令不同过期时间
func TestMultipleCommandsWithDifferentExpiry(t *testing.T) {
	q, clock := newTestQueue(t)

	// 创建多个命令，不同过期时间
	rec1, _ := q.Request("session-1", "short-cmd", nil)
	q.Approve(rec1.ID, "admin", time.Hour)

	rec2, _ := q.Request("session-1", "long-cmd", nil)
	q.Approve(rec2.ID, "admin", time.Hour*24)

	rec3, _ := q.Request("session-1", "forever-cmd", nil)
	q.Approve(rec3.ID, "admin", 0)

	// 前进 2 小时
	clock.now = clock.now.Add(2 * time.Hour)

	// short-cmd 应该过期
	_, ok := q.IsCommandApproved("session-1", "short-cmd")
	if ok {
		t.Error("short-cmd should be expired")
	}

	// long-cmd 和 forever-cmd 应该仍然有效
	_, ok = q.IsCommandApproved("session-1", "long-cmd")
	if !ok {
		t.Error("long-cmd should still be valid")
	}

	_, ok = q.IsCommandApproved("session-1", "forever-cmd")
	if !ok {
		t.Error("forever-cmd should still be valid")
	}
}

// TestRequestCreatesNewAfterExpiry 测试过期后 Request 创建新记录
func TestRequestCreatesNewAfterExpiry(t *testing.T) {
	q, clock := newTestQueue(t)

	// 创建并批准
	rec1, _ := q.Request("session-1", "ls", nil)
	q.Approve(rec1.ID, "admin", time.Hour)
	originalID := rec1.ID

	// 过期
	clock.now = clock.now.Add(2 * time.Hour)

	// 再次请求应该创建新的 pending
	rec2, err := q.Request("session-1", "ls", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rec2.State != ApprovalPending {
		t.Errorf("expected pending, got %s", rec2.State)
	}
	if rec2.ID == originalID {
		t.Error("should create new record with different ID")
	}

	// 旧记录应该被删除
	q.mu.Lock()
	_, exists := q.records[originalID]
	q.mu.Unlock()
	if exists {
		t.Error("old expired record should be deleted")
	}
}

// 辅助函数
func newTestQueueWithPath(t *testing.T, storePath string) (*ApprovalQueue, *testClock) {
	q, err := NewApprovalQueue(storePath)
	if err != nil {
		t.Fatalf("NewApprovalQueue: %v", err)
	}
	clock := &testClock{now: time.Now()}
	q.clock = clock.Now
	return q, clock
}
