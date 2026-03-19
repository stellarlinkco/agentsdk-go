package middleware

import (
	"context"
	"errors"
	"html/template"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
)

func TestTraceMiddlewareBranches(t *testing.T) {
	t.Run("NewTraceMiddleware default dir and mkdir error", func(t *testing.T) {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		tmp := t.TempDir()
		if err := os.Chdir(tmp); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		t.Cleanup(func() {
			if err := os.Chdir(wd); err != nil {
				t.Errorf("chdir cleanup: %v", err)
			}
		})

		tm := NewTraceMiddleware("")
		if tm.outputDir != ".trace" {
			t.Fatalf("expected default output dir .trace, got %q", tm.outputDir)
		}
		if _, err := os.Stat(filepath.Join(tmp, ".trace")); err != nil {
			t.Fatalf("expected .trace created under tmp: %v", err)
		}

		filePath := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		tm2 := NewTraceMiddleware(filePath)
		if tm2 == nil || tm2.outputDir != filePath {
			t.Fatalf("expected middleware created with output dir %q", filePath)
		}
	})

	t.Run("NewTraceMiddleware template parse error", func(t *testing.T) {
		prev := parseTraceTemplate
		parseTraceTemplate = func() (*template.Template, error) {
			return nil, errors.New("bad template")
		}
		t.Cleanup(func() { parseTraceTemplate = prev })

		tm := NewTraceMiddleware(t.TempDir())
		if tm == nil || tm.tmpl != nil {
			t.Fatalf("expected nil template on parse error, got %#v", tm)
		}
	})

	t.Run("Close handles nil receiver and open sessions", func(t *testing.T) {
		var nilTM *TraceMiddleware
		nilTM.Close()

		dir := t.TempDir()
		f, err := os.Create(filepath.Join(dir, "trace.jsonl"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		tm := NewTraceMiddleware(dir)
		sess := &traceSession{id: "s", jsonFile: f}
		tm.sessions["s"] = sess

		tm.Close()
		if sess.jsonFile != nil {
			t.Fatalf("expected json file cleared on close")
		}
	})

	t.Run("sessionFor id normalization and error path", func(t *testing.T) {
		tm := NewTraceMiddleware(t.TempDir())
		t.Cleanup(tm.Close)
		sess := tm.sessionFor("")
		if sess == nil || sess.id != "session" {
			t.Fatalf("expected normalized session, got %#v", sess)
		}
		sess2 := tm.sessionFor("session")
		if sess2 != sess {
			t.Fatalf("expected cached session pointer")
		}

		filePath := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		tm.outputDir = filePath
		if got := tm.sessionFor("boom"); got != nil {
			t.Fatalf("expected session creation failure when outputDir is file")
		}
	})

	t.Run("append covers nil/jsonFile branches and writeJSONLine nil", func(t *testing.T) {
		var nilSess *traceSession
		nilSess.append(TraceEvent{}, NewTraceMiddleware(t.TempDir()))

		dir := t.TempDir()
		tm := NewTraceMiddleware(dir)
		sess := &traceSession{
			id:        "s",
			jsonPath:  filepath.Join(dir, "log.jsonl"),
			htmlPath:  filepath.Join(dir, "log.html"),
			createdAt: time.Unix(0, 0).UTC(),
			updatedAt: time.Unix(0, 0).UTC(),
		}
		sess.append(TraceEvent{Timestamp: time.Unix(1, 0).UTC(), Stage: "before_agent", SessionID: "s"}, tm)
		if _, err := os.Stat(sess.htmlPath); err != nil {
			t.Fatalf("expected html output: %v", err)
		}

		if err := writeJSONLine(nil, TraceEvent{}); err != nil {
			t.Fatalf("writeJSONLine(nil) should be nil, got %v", err)
		}
	})

	t.Run("append handles json marshal failure on write", func(t *testing.T) {
		dir := t.TempDir()
		tm := NewTraceMiddleware(dir)
		f, err := os.Create(filepath.Join(dir, "log.jsonl"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Cleanup(func() { _ = f.Close() })

		sess := &traceSession{
			id:        "s",
			jsonPath:  filepath.Join(dir, "log.jsonl"),
			htmlPath:  filepath.Join(dir, "log.html"),
			jsonFile:  f,
			createdAt: time.Unix(0, 0).UTC(),
			updatedAt: time.Unix(0, 0).UTC(),
		}
		sess.append(TraceEvent{Input: func() {}}, tm)
	})

	t.Run("resolveSessionID and stage helpers", func(t *testing.T) {
		tm := NewTraceMiddleware(t.TempDir())
		st := &State{}
		var ctx context.Context
		if got := tm.resolveSessionID(ctx, st); !strings.HasPrefix(got, "session-0x") {
			t.Fatalf("expected pointer-based session id, got %q", got)
		}

		ctx = context.WithValue(context.Background(), TraceSessionIDContextKey, "typed")
		if got := tm.resolveSessionID(ctx, &State{Values: map[string]any{}}); got != "typed" {
			t.Fatalf("expected typed context session id, got %q", got)
		}

		if _, out := stageIO(StageBeforeAgent, &State{Agent: "agent"}); out != nil {
			t.Fatalf("unexpected output for StageBeforeAgent: %#v", out)
		}
		if in, _ := stageIO(StageBeforeAgent, &State{Agent: "agent"}); in != "agent" {
			t.Fatalf("expected StageBeforeAgent to use agent when model input missing")
		}
		if in, out := stageIO(Stage(123), &State{}); in != nil || out != nil {
			t.Fatalf("expected unknown stage to return nil io")
		}
		if got := stageName(Stage(123)); !strings.HasPrefix(got, "stage_") {
			t.Fatalf("expected fallback stage name, got %q", got)
		}

		var nilTM *TraceMiddleware
		if nilTM.now().IsZero() {
			t.Fatalf("expected nil receiver now() to fall back to time.Now")
		}
	})

	t.Run("writeAtomic rename error and sanitizeSessionComponent fallback", func(t *testing.T) {
		dir := t.TempDir()
		destDir := filepath.Join(dir, "existing-dir")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := writeAtomic(destDir, []byte("x")); err == nil {
			t.Fatalf("expected rename error when destination is a directory")
		}
		if got := sanitizeSessionComponent("!!!"); got != "session" {
			t.Fatalf("expected fallback sanitized session id, got %q", got)
		}
	})

	t.Run("writeAtomic create temp error when directory is not writable", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod permissions are not reliable on Windows")
		}
		dir := t.TempDir()
		ro := filepath.Join(dir, "ro")
		if err := os.MkdirAll(ro, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatalf("chmod: %v", err)
		}

		if err := writeAtomic(filepath.Join(ro, "out.html"), []byte("x")); err == nil {
			t.Fatalf("expected create temp error for read-only directory")
		}
	})

	t.Run("record early returns and trace skill body-size loaded=false", func(t *testing.T) {
		var nilTM *TraceMiddleware
		nilTM.record(context.Background(), StageBeforeAgent, &State{})

		tm := NewTraceMiddleware(t.TempDir())
		tm.record(context.Background(), StageBeforeAgent, nil)

		filePath := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		tm.outputDir = filePath
		tm.record(context.Background(), StageBeforeAgent, &State{})

		reg := skills.NewRegistry()
		if err := reg.Register(skills.Definition{Name: "demo"}, unsizedHandler{}); err != nil {
			t.Fatalf("register: %v", err)
		}
		st := &State{Values: map[string]any{
			forceSkillsValue:    []string{"demo"},
			skillsRegistryValue: reg,
		}}
		tm = NewTraceMiddleware(t.TempDir(), WithSkillTracing(true))
		tm.traceSkillsSnapshot(context.Background(), st, true)
		tm.traceSkillsSnapshot(context.Background(), st, false)
	})

	t.Run("renderHTML returns template execution error", func(t *testing.T) {
		tm := NewTraceMiddleware(t.TempDir())
		tm.tmpl = template.Must(template.New("bad").Parse("{{call .SessionID}}"))

		sess := &traceSession{
			id:        "sess",
			createdAt: time.Unix(0, 0).UTC(),
			updatedAt: time.Unix(0, 0).UTC(),
			jsonPath:  filepath.Join(t.TempDir(), "log.jsonl"),
			htmlPath:  filepath.Join(t.TempDir(), "log.html"),
			events:    []TraceEvent{},
		}
		if err := tm.renderHTML(sess); err == nil {
			t.Fatalf("expected template execute error")
		}
	})
}

type unsizedHandler struct{}

func (unsizedHandler) Execute(context.Context, skills.ActivationContext) (skills.Result, error) {
	return skills.Result{}, nil
}

func (unsizedHandler) BodyLength() (int, bool) { return 10, false }
