package middleware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
)

type fakeTempFile struct {
	name     string
	writeErr error
	closeErr error
	closed   bool
}

func (f *fakeTempFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeTempFile) Close() error {
	f.closed = true
	return f.closeErr
}

func (f *fakeTempFile) Name() string { return f.name }

func TestWriteAtomicWith_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tmp        *fakeTempFile
		mkdirErr   error
		createErr  error
		renameErr  error
		wantRemove bool
	}{
		{
			name:       "mkdir error",
			tmp:        &fakeTempFile{name: "tmp"},
			mkdirErr:   errors.New("mkdir failed"),
			wantRemove: false,
		},
		{
			name:       "create temp error",
			tmp:        &fakeTempFile{name: "tmp"},
			createErr:  errors.New("create failed"),
			wantRemove: false,
		},
		{
			name:       "write error",
			tmp:        &fakeTempFile{name: "tmp", writeErr: errors.New("write failed")},
			wantRemove: true,
		},
		{
			name:       "close error",
			tmp:        &fakeTempFile{name: "tmp", closeErr: errors.New("close failed")},
			wantRemove: true,
		},
		{
			name:       "rename error",
			tmp:        &fakeTempFile{name: "tmp"},
			renameErr:  errors.New("rename failed"),
			wantRemove: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			removed := []string{}
			err := writeAtomicWith(
				"out/trace.html",
				[]byte("hello"),
				func(string, os.FileMode) error { return tt.mkdirErr },
				func(string, string) (tempFile, error) {
					if tt.createErr != nil {
						return nil, tt.createErr
					}
					return tt.tmp, nil
				},
				func(string, string) error { return tt.renameErr },
				func(path string) error {
					removed = append(removed, path)
					return nil
				},
			)
			if err == nil {
				t.Fatalf("expected error")
			}

			if tt.wantRemove {
				if len(removed) != 1 || removed[0] != tt.tmp.name {
					t.Fatalf("expected temp file removal of %q, got %v", tt.tmp.name, removed)
				}
			} else if len(removed) != 0 {
				t.Fatalf("expected no removals, got %v", removed)
			}
		})
	}
}

func TestTraceMiddleware_NewSessionLocked_OpenFileError(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()
	mw := NewTraceMiddleware(outDir)

	jsonPath := filepath.Join(outDir, "log-bad.jsonl")
	if err := os.Mkdir(jsonPath, 0o755); err != nil {
		t.Fatalf("mkdir placeholder: %v", err)
	}
	if _, err := mw.newSessionLocked("bad"); err == nil {
		t.Fatalf("expected openfile error")
	}
}

func TestTraceMiddleware_RenderHTML_NilSession(t *testing.T) {
	t.Parallel()

	mw := NewTraceMiddleware(t.TempDir())
	if err := mw.renderHTML(nil); err != nil {
		t.Fatalf("renderHTML(nil) error: %v", err)
	}
}

func TestTraceMiddleware_Append_LogsWriteAndRenderErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}

	jsonFile, err := os.CreateTemp(dir, "json-*.jsonl")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	jsonPath := jsonFile.Name()
	if err := jsonFile.Close(); err != nil {
		t.Fatalf("close json file: %v", err)
	}

	mw := NewTraceMiddleware(dir)
	sess := &traceSession{
		id:        "s",
		createdAt: time.Now(),
		updatedAt: time.Now(),
		jsonPath:  jsonPath,
		htmlPath:  filepath.Join(blocked, "trace.html"),
		jsonFile:  jsonFile,
	}
	sess.append(TraceEvent{Timestamp: time.Now(), Stage: "stage"}, mw)
}

func TestTraceMiddleware_RenderHTML_MarshalFallback_DoubleFailure(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()
	mw := NewTraceMiddleware(outDir)
	mw.tmpl = nil

	sess := &traceSession{
		id:        "s",
		createdAt: time.Now(),
		updatedAt: time.Now(),
		jsonPath:  filepath.Join(outDir, "log.jsonl"),
		htmlPath:  filepath.Join(outDir, "log.html"),
		events: []TraceEvent{
			{
				Timestamp: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
				Stage:     "after_tool",
				Iteration: 1,
				SessionID: "s",
			},
		},
	}
	if err := mw.renderHTML(sess); err != nil {
		t.Fatalf("renderHTML error: %v", err)
	}
}

func TestTraceHelpers_MissedBranches(t *testing.T) {
	t.Parallel()

	if in, out := stageIO(StageBeforeAgent, nil); in != nil || out != nil {
		t.Fatalf("stageIO(nil) = %v,%v, want nil,nil", in, out)
	}

	st := &State{Agent: "agent", ModelInput: "input"}
	in, out := stageIO(StageBeforeAgent, st)
	if in != "input" || out != nil {
		t.Fatalf("stageIO(before_agent) = %v,%v", in, out)
	}

	mw := NewTraceMiddleware(t.TempDir())
	ctxTrace := stringValueContext{Context: context.Background(), key: "trace.session_id", value: "trace-string"}
	if got := mw.resolveSessionID(ctxTrace, nil); got != "trace-string" {
		t.Fatalf("resolveSessionID trace string = %q", got)
	}

	ctxSess := stringValueContext{Context: context.Background(), key: "session_id", value: "sess-string"}
	if got := mw.resolveSessionID(ctxSess, nil); got != "sess-string" {
		t.Fatalf("resolveSessionID session string = %q", got)
	}
}

type stringValueContext struct {
	context.Context
	key   string
	value string
}

func (c stringValueContext) Value(key any) any {
	if k, ok := key.(string); ok && k == c.key {
		return c.value
	}
	return c.Context.Value(key)
}

func TestTraceSkillsSnapshot_MissedBranches(t *testing.T) {
	t.Parallel()

	mw := NewTraceMiddleware(t.TempDir(), WithSkillTracing(true))

	// names empty
	mw.traceSkillsSnapshot(context.Background(), &State{Values: map[string]any{}}, true)

	// registry nil
	mw.traceSkillsSnapshot(context.Background(), &State{Values: map[string]any{forceSkillsValue: []string{"a"}}}, true)

	// beforeSnapshot empty
	mw.traceSkillsSnapshot(context.Background(), &State{Values: map[string]any{
		forceSkillsValue:    []string{"a"},
		skillsRegistryValue: skills.NewRegistry(),
		traceSkillBeforeKey: map[string]int{},
	}}, false)

	// skillBodies key == ""
	_ = skillBodies(skills.NewRegistry(), []string{" ", "\n"})

	// skillBodySize handler nil
	_ = skillBodySize(nil)

	// orderedSkillNames duplicate
	names := orderedSkillNames([]string{"a", "a"}, map[string]int{}, map[string]int{})
	if len(names) != 1 || names[0] != "a" {
		t.Fatalf("orderedSkillNames = %v", names)
	}
}
