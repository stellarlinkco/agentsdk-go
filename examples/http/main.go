package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/middleware"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

const (
	defaultAddr        = ":8080"
	defaultModel       = "claude-3-5-sonnet-20241022"
	defaultRunTimeout  = 45 * time.Second
	defaultMaxSessions = 500
)

func main() {
	projectRoot, err := resolveProjectRoot()
	if err != nil {
		log.Fatalf("init project root: %v", err)
	}

	addr := getEnv("AGENTSDK_HTTP_ADDR", defaultAddr)
	modelName := getEnv("AGENTSDK_MODEL", defaultModel)
	defaultTimeout := getDuration("AGENTSDK_DEFAULT_TIMEOUT", defaultRunTimeout)
	maxSessions := getInt("AGENTSDK_MAX_SESSIONS", defaultMaxSessions)
	settingsPath := resolveSettingsPath(projectRoot)

	mode := api.ModeContext{
		EntryPoint: api.EntryPointPlatform,
		Platform: &api.PlatformContext{
			Organization: "agentsdk-go",
			Project:      "http-example",
			Environment:  "dev",
		},
	}

	staticDir := filepath.Join(filepath.Dir(os.Args[0]), "static")
	handler, srv, traceMW, traceDir := buildMux(mode, defaultTimeout, staticDir, projectRoot)
	sessionMW := newSessionStateMiddleware()

	opts := api.Options{
		EntryPoint:   api.EntryPointPlatform,
		ProjectRoot:  projectRoot,
		Mode:         mode,
		ModelFactory: &modelpkg.AnthropicProvider{ModelName: modelName},
		MaxSessions:  maxSessions,
		Middleware: []middleware.Middleware{
			sessionMW,
			traceMW,
		},
	}

	if settingsPath != "" {
		opts.SettingsPath = settingsPath
	}

	rt, err := api.New(context.Background(), opts)
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}
	defer rt.Close()
	srv.runtime = rt

	log.Printf("Trace middleware enabled, writing traces to %s", traceDir)

	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("HTTP agent server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server stopped unexpectedly: %v", err)
		}
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}
	log.Println("server exited cleanly")
}

func buildMux(mode api.ModeContext, defaultTimeout time.Duration, staticDir, projectRoot string) (http.Handler, *httpServer, middleware.Middleware, string) {
	resolvedStaticDir := resolveStaticDir(staticDir)
	log.Printf("Using static directory: %s", resolvedStaticDir)
	srv := &httpServer{
		mode:           mode,
		defaultTimeout: defaultTimeout,
		staticDir:      resolvedStaticDir,
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	traceDir := filepath.Join(projectRoot, ".trace")
	traceMW := middleware.NewTraceMiddleware(traceDir)
	handler := http.Handler(mux)
	httpTraceDir := filepath.Join(projectRoot, ".claude-trace")
	if httpTraceWriter, err := middleware.NewFileHTTPTraceWriter(httpTraceDir); err != nil {
		log.Printf("HTTP trace disabled: %v", err)
	} else {
		httpTraceMW := middleware.NewHTTPTraceMiddleware(httpTraceWriter)
		handler = httpTraceMW.Wrap(mux)
		log.Printf("HTTP trace middleware enabled, writing HTTP traces to %s", httpTraceWriter.Path())
	}
	return handler, srv, traceMW, traceDir
}

// 静态目录优先使用二进制同级目录，不存在则退回源码路径
func resolveStaticDir(staticDir string) string {
	if strings.TrimSpace(staticDir) == "" {
		staticDir = filepath.Join(filepath.Dir(os.Args[0]), "static")
	}
	if abs, err := filepath.Abs(staticDir); err == nil {
		staticDir = abs
	}
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		return staticDir
	}
	// 优先尝试当前工作目录的 static
	if cwd, err := os.Getwd(); err == nil {
		cwdStatic := filepath.Join(cwd, "static")
		if info, err := os.Stat(cwdStatic); err == nil && info.IsDir() {
			return cwdStatic
		}
	}
	// 回退到项目根目录下的 examples/http/static
	fallback := filepath.Join("examples", "http", "static")
	if abs, err := filepath.Abs(fallback); err == nil {
		fallback = abs
	}
	if info, err := os.Stat(fallback); err == nil && info.IsDir() {
		return fallback
	}
	log.Printf("WARNING: static directory not found, using: %s", staticDir)
	return staticDir
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val <= 0 {
		return fallback
	}
	return val
}

func getDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if dur, err := time.ParseDuration(raw); err == nil {
		return dur
	}
	if ms, err := strconv.Atoi(raw); err == nil {
		return time.Duration(ms) * time.Millisecond
	}
	return fallback
}

func resolveProjectRoot() (string, error) {
	// 优先使用显式指定的项目根目录，便于容器或 CI 注入路径
	if root := strings.TrimSpace(os.Getenv("AGENTSDK_PROJECT_ROOT")); root != "" {
		return root, nil
	}

	// 回退到 SDK 自带的智能解析逻辑，确保返回真实项目目录
	return api.ResolveProjectRoot()
}

// resolveSettingsPath ensures the example keeps running even when the
// repository lacks a tracked .claude/settings.json. It falls back to the
// bundled example config if neither project nor local settings are present.
func resolveSettingsPath(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}

	projectSettings := filepath.Join(projectRoot, ".claude", "settings.json")
	localSettings := filepath.Join(projectRoot, ".claude", "settings.local.json")
	if fileExists(projectSettings) || fileExists(localSettings) {
		return ""
	}

	fallback := filepath.Join(projectRoot, "examples", "http", ".claude", "settings.json")
	if fileExists(fallback) {
		log.Printf(".claude/settings.json not found in project root, using bundled example: %s", fallback)
		return fallback
	}

	log.Printf("WARNING: no .claude settings found; continuing with built-in defaults")
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func newSessionStateMiddleware() middleware.Middleware {
	return middleware.Funcs{
		Identifier: "session-state",
		OnBeforeAgent: func(ctx context.Context, st *middleware.State) error {
			if st == nil {
				return nil
			}
			id := sessionIDFromContext(ctx)
			if id == "" {
				return nil
			}
			if st.Values == nil {
				st.Values = map[string]any{}
			}
			st.Values["trace.session_id"] = id
			st.Values["session_id"] = id
			return nil
		},
	}
}

func sessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, _ := ctx.Value("trace.session_id").(string); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	if id, _ := ctx.Value("session_id").(string); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return ""
}
