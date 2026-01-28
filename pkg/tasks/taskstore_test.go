package tasks

import (
	"strings"
	"testing"
)

func TestTaskStoreCreateTaskStoresTrimmedFields(t *testing.T) {
	store := NewTaskStore()
	id, err := store.CreateTask("  Fix flaky tests  ", "  stabilize CI  ", " default ")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if !strings.HasPrefix(id, "task-") {
		t.Fatalf("unexpected id %q", id)
	}

	got, ok := store.GetTask(id)
	if !ok {
		t.Fatalf("expected task %q to exist", id)
	}
	if got.Subject != "Fix flaky tests" {
		t.Fatalf("unexpected subject %q", got.Subject)
	}
	if got.Description != "stabilize CI" {
		t.Fatalf("unexpected description %q", got.Description)
	}
	if got.ActiveForm != "default" {
		t.Fatalf("unexpected activeForm %q", got.ActiveForm)
	}
	if got.ID != id {
		t.Fatalf("unexpected stored id %q", got.ID)
	}
	if store.Len() != 1 {
		t.Fatalf("expected Len=1 got %d", store.Len())
	}
}

func TestTaskStoreCreateTaskValidatesRequiredFields(t *testing.T) {
	store := NewTaskStore()
	cases := []struct {
		name       string
		subject    string
		activeForm string
		want       string
	}{
		{name: "empty subject", subject: "  ", activeForm: "f", want: "subject cannot be empty"},
		{name: "empty activeForm", subject: "x", activeForm: "  ", want: "activeForm cannot be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.CreateTask(tc.subject, "", tc.activeForm); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTaskStoreNilHandling(t *testing.T) {
	var store *TaskStore
	if _, err := store.CreateTask("x", "", "f"); err == nil || !strings.Contains(err.Error(), "task store is nil") {
		t.Fatalf("expected nil store error, got %v", err)
	}
	if store.Len() != 0 {
		t.Fatalf("expected Len=0 for nil store, got %d", store.Len())
	}
	if _, ok := store.GetTask("x"); ok {
		t.Fatalf("expected GetTask false for nil store")
	}
}

func TestTaskStoreGetTaskTrimsAndRejectsEmptyID(t *testing.T) {
	store := NewTaskStore()
	if _, ok := store.GetTask(" "); ok {
		t.Fatalf("expected empty id miss")
	}
	id, err := store.CreateTask("x", "", "f")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	got, ok := store.GetTask(" " + id + " ")
	if !ok {
		t.Fatalf("expected trimmed id lookup to succeed")
	}
	if got.ID != id {
		t.Fatalf("unexpected task id %q", got.ID)
	}
}
