package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

const (
	defaultAddr       = "127.0.0.1:0"
	defaultModel      = "claude-3-5-sonnet-20241022"
	defaultRunTimeout = 60 * time.Minute
)

var (
	httpFatal          = log.Fatal
	resolveProjectRoot = api.ResolveProjectRoot
	netListen          = net.Listen
)

type runConfig struct {
	addr        string
	modelName   string
	projectRoot string
	staticDir   string
	serve       bool
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		httpFatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	cfg, opts, err := buildConfigAndOptions(ctx, args, out)
	if err != nil {
		return err
	}
	opts.EntryPoint = api.EntryPointPlatform
	opts.Timeout = defaultRunTimeout

	runtime, err := api.New(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer runtime.Close()

	sdir := strings.TrimSpace(cfg.staticDir)
	if sdir == "" {
		sdir = filepath.Join(cfg.projectRoot, "examples", "03-http", "static")
	}
	srv := &httpServer{
		runtime:        runtime,
		defaultTimeout: defaultRunTimeout,
		staticDir:      sdir,
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := netListen("tcp", strings.TrimSpace(cfg.addr))
	if err != nil {
		return fmt.Errorf("listen %s: %w", strings.TrimSpace(cfg.addr), err)
	}
	defer ln.Close()

	baseURL := baseURLFromListener(ln)
	fmt.Fprintf(out, "HTTP agent server listening on %s\n", baseURL)

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ln) }()

	var servedErr error
	servedRead := false

	if !cfg.serve {
		client := &http.Client{Timeout: 2 * time.Second}
		res, err := client.Get(baseURL + "/health")
		if err != nil {
			_ = server.Close()
			return fmt.Errorf("self-test health: %w", err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			_ = server.Close()
			return fmt.Errorf("self-test health: unexpected status %d", res.StatusCode)
		}
	}

	if cfg.serve {
		select {
		case <-ctx.Done():
		case servedErr = <-serveErr:
			servedRead = true
			if servedErr != nil && !errorsIsServerClosed(servedErr) {
				return fmt.Errorf("server stopped unexpectedly: %w", servedErr)
			}
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if !servedRead {
		servedErr = <-serveErr
	}
	if servedErr != nil && !errorsIsServerClosed(servedErr) {
		return fmt.Errorf("server stopped unexpectedly: %w", servedErr)
	}
	return nil
}

func buildConfigAndOptions(ctx context.Context, args []string, out io.Writer) (runConfig, api.Options, error) {
	fs := flag.NewFlagSet("03-http", flag.ContinueOnError)
	fs.SetOutput(out)

	var cfg runConfig
	fs.StringVar(&cfg.addr, "addr", envOr("AGENTSDK_HTTP_ADDR", defaultAddr), "listen address (use 127.0.0.1:0 for random port)")
	fs.StringVar(&cfg.modelName, "model", envOr("AGENTSDK_MODEL", defaultModel), "model name for online provider")
	fs.StringVar(&cfg.projectRoot, "project-root", "", "project root (default: auto-resolve)")
	fs.StringVar(&cfg.staticDir, "static-dir", "", "static files dir (default: <project-root>/examples/03-http/static)")
	fs.BoolVar(&cfg.serve, "serve", false, "serve until ctx cancel (default: self-test and exit)")
	if err := fs.Parse(args); err != nil {
		return runConfig{}, api.Options{}, err
	}

	root := strings.TrimSpace(cfg.projectRoot)
	if root == "" {
		resolved, err := resolveProjectRoot()
		if err != nil {
			return runConfig{}, api.Options{}, fmt.Errorf("resolve project root: %w", err)
		}
		root = resolved
	}
	cfg.projectRoot = root

	apiKey := demomodel.AnthropicAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return runConfig{}, api.Options{}, fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}

	opts := api.Options{
		ProjectRoot: root,
		ModelFactory: &modelpkg.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: strings.TrimSpace(cfg.modelName),
		},
	}
	_ = ctx
	return cfg, opts, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func baseURLFromListener(ln net.Listener) string {
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil || strings.TrimSpace(port) == "" {
		return "http://127.0.0.1"
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "::" || host == "[::]" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return "http://" + host + ":" + port
}

func errorsIsServerClosed(err error) bool {
	return err == http.ErrServerClosed
}
