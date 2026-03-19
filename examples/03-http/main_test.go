package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRun_SelfTestExits(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var out bytes.Buffer
	if err := run(ctx, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Len() == 0 {
		t.Fatalf("expected output")
	}
}

func TestRun_ServeStopsOnCancel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"--serve=true", "--addr=127.0.0.1:0"}, &out)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out")
	}
}

func TestBuildConfigAndOptions_ParseError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var out bytes.Buffer
	if _, _, err := buildConfigAndOptions(ctx, []string{"--nope"}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildConfigAndOptions_WithProjectRootAndStaticDir(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	root := t.TempDir()
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	var out bytes.Buffer
	cfg, opts, err := buildConfigAndOptions(ctx, []string{
		"--project-root", root,
		"--static-dir", staticDir,
		"--addr", "127.0.0.1:0",
	}, &out)
	if err != nil {
		t.Fatalf("buildConfigAndOptions: %v", err)
	}
	if cfg.projectRoot != root {
		t.Fatalf("projectRoot=%q", cfg.projectRoot)
	}
	if got := opts.ProjectRoot; got != root {
		t.Fatalf("opts.ProjectRoot=%q", got)
	}
	if opts.ModelFactory == nil {
		t.Fatalf("expected ModelFactory")
	}
}

func TestBaseURLFromListener_UsesLocalhostFallback(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	_ = ln.Close()
	got := baseURLFromListener(ln)
	if got == "" || got[:7] != "http://" {
		t.Fatalf("unexpected base url %q", got)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("AGENTSDK_HTTP_ADDR", "")
	if got := envOr("AGENTSDK_HTTP_ADDR", "x"); got != "x" {
		t.Fatalf("got=%q", got)
	}
	t.Setenv("AGENTSDK_HTTP_ADDR", "y")
	if got := envOr("AGENTSDK_HTTP_ADDR", "x"); got != "y" {
		t.Fatalf("got=%q", got)
	}
}

func TestMain_OfflineDoesNotFatal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	oldFatal := httpFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	t.Cleanup(func() {
		httpFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
	})

	called := false
	httpFatal = func(...any) { called = true }

	tmp := t.TempDir()
	os.Args = []string{"03-http.test", "--project-root", tmp, "--addr", "127.0.0.1:0"}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w

	main()

	_ = w.Close()
	_, _ = io.ReadAll(r)
	_ = r.Close()

	if called {
		t.Fatalf("unexpected fatal")
	}
}

func TestBuildConfigAndOptions_ResolveProjectRootError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	old := resolveProjectRoot
	t.Cleanup(func() { resolveProjectRoot = old })
	resolveProjectRoot = func() (string, error) { return "", errors.New("no root") }

	var out bytes.Buffer
	if _, _, err := buildConfigAndOptions(ctx, []string{"--project-root="}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_ListenError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var out bytes.Buffer
	if err := run(ctx, []string{"--addr=bad"}, &out); err == nil {
		t.Fatalf("expected error")
	}
}

func TestMain_FatalsOnError(t *testing.T) {
	oldFatal := httpFatal
	oldArgs := os.Args
	oldStdout := os.Stdout
	t.Cleanup(func() {
		httpFatal = oldFatal
		os.Args = oldArgs
		os.Stdout = oldStdout
	})

	called := false
	httpFatal = func(...any) { called = true }

	tmp := t.TempDir()
	os.Args = []string{"03-http.test", "--project-root", tmp, "--addr=bad"}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w

	main()

	_ = w.Close()
	_, _ = io.ReadAll(r)
	_ = r.Close()

	if !called {
		t.Fatalf("expected fatal")
	}
}

type stubAddr string

func (a stubAddr) Network() string { return "stub" }
func (a stubAddr) String() string  { return string(a) }

type stubListener struct {
	addr net.Addr
}

func (l stubListener) Accept() (net.Conn, error) { return nil, errors.New("accept boom") }
func (l stubListener) Close() error              { return nil }
func (l stubListener) Addr() net.Addr            { return l.addr }

func TestRun_SelfTestHealthError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	old := netListen
	t.Cleanup(func() { netListen = old })
	netListen = func(string, string) (net.Listener, error) {
		return stubListener{addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}}, nil
	}

	var out bytes.Buffer
	err := run(ctx, []string{"--project-root", t.TempDir(), "--addr=127.0.0.1:0"}, &out)
	if err == nil || !strings.Contains(err.Error(), "self-test health:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_ServeUnexpectedStop(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	old := netListen
	t.Cleanup(func() { netListen = old })
	netListen = func(string, string) (net.Listener, error) {
		return stubListener{addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}}, nil
	}

	var out bytes.Buffer
	err := run(ctx, []string{"--serve=true", "--project-root", t.TempDir(), "--addr=127.0.0.1:0"}, &out)
	if err == nil || !strings.Contains(err.Error(), "server stopped unexpectedly:") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_ServeClosedListenerUnexpectedStop(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	old := netListen
	t.Cleanup(func() { netListen = old })
	netListen = func(network, address string) (net.Listener, error) {
		ln, err := net.Listen(network, "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		_ = ln.Close()
		return ln, nil
	}

	var out bytes.Buffer
	err := run(ctx, []string{"--serve=true", "--project-root", t.TempDir(), "--addr=127.0.0.1:0"}, &out)
	if err == nil || !strings.Contains(err.Error(), "server stopped unexpectedly:") {
		t.Fatalf("err=%v", err)
	}
}

func TestBaseURLFromListener_SplitHostPortError(t *testing.T) {
	ln := stubListener{addr: stubAddr("nope")}
	if got := baseURLFromListener(ln); got != "http://127.0.0.1" {
		t.Fatalf("got=%q", got)
	}
}

type lyingListener struct {
	inner      net.Listener
	advertised net.Addr
}

func (l lyingListener) Accept() (net.Conn, error) { return l.inner.Accept() }
func (l lyingListener) Close() error              { return l.inner.Close() }
func (l lyingListener) Addr() net.Addr            { return l.advertised }

func TestRun_SelfTestHealthUnexpectedStatus(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	parsed, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	host, portStr, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi: %v", err)
	}
	adv := &net.TCPAddr{IP: net.ParseIP(host), Port: port}

	old := netListen
	t.Cleanup(func() { netListen = old })
	netListen = func(network, address string) (net.Listener, error) {
		realLn, err := net.Listen(network, "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		return lyingListener{inner: realLn, advertised: adv}, nil
	}

	var out bytes.Buffer
	err = run(ctx, []string{"--project-root", t.TempDir(), "--addr=127.0.0.1:0"}, &out)
	if err == nil || !strings.Contains(err.Error(), "self-test health: unexpected status 500") {
		t.Fatalf("err=%v", err)
	}
}
