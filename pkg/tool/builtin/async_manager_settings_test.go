package toolbuiltin

import "testing"

func TestAsyncTaskManagerSetMaxOutputLen(t *testing.T) {
	m := newAsyncTaskManager()
	if m.maxOutputLen != maxAsyncOutputLen {
		t.Fatalf("expected default max output len %d got %d", maxAsyncOutputLen, m.maxOutputLen)
	}

	m.SetMaxOutputLen(123)
	if m.maxOutputLen != 123 {
		t.Fatalf("expected max output len 123 got %d", m.maxOutputLen)
	}

	m.SetMaxOutputLen(0)
	if m.maxOutputLen != maxAsyncOutputLen {
		t.Fatalf("expected reset max output len %d got %d", maxAsyncOutputLen, m.maxOutputLen)
	}

	var nilManager *AsyncTaskManager
	nilManager.SetMaxOutputLen(1)
}
