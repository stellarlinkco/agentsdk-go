package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/server"
)

func serveCommand(ctx context.Context, argv []string, cfgPath string, streams ioStreams) error {
	set := flag.NewFlagSet("serve", flag.ContinueOnError)
	set.SetOutput(streams.err)
	host := set.String("host", "0.0.0.0", "Address to bind (default 0.0.0.0).")
	port := set.Int("port", 8080, "Port number for the HTTP server.")
	configFlag := set.String("config", cfgPath, "Path to CLI config file.")
	set.Usage = func() {
		fmt.Fprintln(streams.err, "Usage: agentctl serve [flags]")
		fmt.Fprintln(streams.err, "\nFlags:")
		set.PrintDefaults()
		fmt.Fprintln(streams.err, "\nRoutes:")
		fmt.Fprintln(streams.err, "  POST /api/run        Execute a single run")
		fmt.Fprintln(streams.err, "  GET  /api/run/stream Stream events via SSE")
		fmt.Fprintln(streams.err, "  GET  /health        Health probe")
	}
	if err := set.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cfgPath = *configFlag
	if *port < 0 || *port > 65535 {
		return fmt.Errorf("invalid port %d", *port)
	}
	cfg, err := loadCLIConfig(cfgPath)
	if err != nil {
		return err
	}
	ag, err := agentFactory(agent.Config{
		Name:        "agentctl-serve",
		Description: "agentsdk-go HTTP server",
		DefaultContext: agent.RunContext{
			MaxIterations: 1,
		},
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	if err := attachTools(ag, nil, cfg.MCPServers); err != nil {
		return err
	}
	h := buildMux(ag)
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", strings.TrimSpace(*host), *port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	srv := &http.Server{Handler: h}
	addr := listener.Addr().String()
	if streams.out != nil {
		fmt.Fprintf(streams.out, "agentctl serve listening on http://%s\n", addr)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func buildMux(ag agent.Agent) http.Handler {
	srv := server.New(ag)
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", srv))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		response := map[string]any{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339),
		}
		_ = json.NewEncoder(w).Encode(response)
	})
	return mux
}
