package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/sandbox"
)

type config struct {
	root      string
	allowHost string
	denyHost  string
	cpuLimit  float64
	memLimit  uint64
	diskLimit uint64
}

func main() {
	cfg := parseConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	root, shared, safeFile, cleanup, err := prepareRoots(cfg.root, logger)
	if err != nil {
		logger.Error("bootstrap workdir failed", "err", err)
		os.Exit(1)
	}
	defer cleanup()

	fsPolicy := sandbox.NewFileSystemAllowList(root)
	fsPolicy.Allow(shared)

	netPolicy := sandbox.NewDomainAllowList(cfg.allowHost)
	netPolicy.Allow("*.svc.local")

	limiter := sandbox.NewResourceLimiter(sandbox.ResourceLimits{
		MaxCPUPercent:  cfg.cpuLimit,
		MaxMemoryBytes: cfg.memLimit,
		MaxDiskBytes:   cfg.diskLimit,
	})

	manager := sandbox.NewManager(fsPolicy, netPolicy, limiter)
	logger.Info("sandbox policies ready",
		"fs_roots", fsPolicy.Roots(),
		"net_allow", netPolicy.Allowed(),
		"limits", manager.Limits(),
	)

	if err := demonstrateFileSystem(manager, logger, root, shared, safeFile); err != nil {
		logger.Error("filesystem policy demo failed", "err", err)
		os.Exit(1)
	}

	if err := demonstrateNetwork(manager, logger, cfg.allowHost, cfg.denyHost); err != nil {
		logger.Error("network policy demo failed", "err", err)
		os.Exit(1)
	}

	if err := demonstrateResources(manager, logger, cfg, safeFile); err != nil {
		logger.Error("resource limiter demo failed", "err", err)
		os.Exit(1)
	}

	logger.Info("sandbox demo completed")
}

func parseConfig() config {
	var cfg config
	flag.StringVar(&cfg.root, "root", "", "optional workspace root to protect; defaults to a temp dir")
	flag.StringVar(&cfg.allowHost, "allow-host", "example.com", "domain that should be allowed")
	flag.StringVar(&cfg.denyHost, "deny-host", "github.com", "domain that should be blocked")
	flag.Float64Var(&cfg.cpuLimit, "cpu-limit", 50, "maximum CPU percent")
	memMB := flag.Int("mem-mb", 128, "memory limit in MB")
	diskMB := flag.Int("disk-mb", 16, "disk limit in MB")
	flag.Parse()
	cfg.memLimit = uint64(*memMB) * 1024 * 1024
	cfg.diskLimit = uint64(*diskMB) * 1024 * 1024
	return cfg
}

func prepareRoots(userRoot string, logger *slog.Logger) (root string, shared string, safeFile string, cleanup func(), err error) {
	cleanup = func() {}
	var steps []func()

	root = strings.TrimSpace(userRoot)
	if root == "" {
		root, err = os.MkdirTemp("", "agentsdk-sandbox-root-*")
		if err != nil {
			return "", "", "", cleanup, fmt.Errorf("create temp root: %w", err)
		}
		steps = append(steps, func() { _ = os.RemoveAll(root) })
		logger.Info("created temp root", "path", root)
	} else if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", "", cleanup, fmt.Errorf("ensure root %s: %w", root, err)
	}

	safeFile = filepath.Join(root, "workspace", "report.txt")
	if err := os.MkdirAll(filepath.Dir(safeFile), 0o755); err != nil {
		return "", "", "", cleanup, fmt.Errorf("mkdir for %s: %w", safeFile, err)
	}
	if err := os.WriteFile(safeFile, []byte("sandbox demo: inside allowlist\n"), 0o644); err != nil {
		return "", "", "", cleanup, fmt.Errorf("seed safe file: %w", err)
	}

	shared, err = os.MkdirTemp("", "agentsdk-sandbox-shared-*")
	if err != nil {
		return "", "", "", cleanup, fmt.Errorf("create shared dir: %w", err)
	}
	steps = append(steps, func() { _ = os.RemoveAll(shared) })
	sharedFile := filepath.Join(shared, "shared.txt")
	if err := os.WriteFile(sharedFile, []byte("sandbox demo: extra allowed path\n"), 0o644); err != nil {
		return "", "", "", cleanup, fmt.Errorf("seed shared file: %w", err)
	}

	cleanup = func() {
		for i := len(steps) - 1; i >= 0; i-- {
			steps[i]()
		}
	}
	return root, shared, safeFile, cleanup, nil
}

func demonstrateFileSystem(manager *sandbox.Manager, logger *slog.Logger, root, shared, safeFile string) error {
	if err := manager.CheckPath(safeFile); err != nil {
		return fmt.Errorf("allowlist rejected root file: %w", err)
	}
	logger.Info("filesystem allowlist allows workspace file", "path", safeFile)

	sharedFile := filepath.Join(shared, "shared.txt")
	if err := manager.CheckPath(sharedFile); err != nil {
		return fmt.Errorf("allowlist rejected shared path: %w", err)
	}
	logger.Info("filesystem allowlist permits extra root", "path", sharedFile)

	escape := filepath.Join(root, "..", "etc", "passwd")
	if err := manager.CheckPath(escape); err == nil {
		return fmt.Errorf("expected path escape to be denied: %s", escape)
	} else {
		logger.Info("filesystem blocked escape", "path", escape, "err", err)
	}

	return nil
}

func demonstrateNetwork(manager *sandbox.Manager, logger *slog.Logger, allowed, denied string) error {
	if err := manager.CheckNetwork(allowed); err != nil {
		return fmt.Errorf("network allowlist rejected %s: %w", allowed, err)
	}
	logger.Info("network allowlist permits host", "host", allowed)

	if err := manager.CheckNetwork(denied); err == nil {
		return fmt.Errorf("expected network check to block %s", denied)
	} else {
		logger.Info("network allowlist blocked host", "host", denied, "err", err)
	}
	return nil
}

func demonstrateResources(manager *sandbox.Manager, logger *slog.Logger, cfg config, safeFile string) error {
	steady := sandbox.ResourceUsage{
		CPUPercent:  cfg.cpuLimit * 0.5,
		MemoryBytes: cfg.memLimit / 4,
		DiskBytes:   cfg.diskLimit / 8,
	}
	if err := manager.Enforce(safeFile, cfg.allowHost, steady); err != nil {
		return fmt.Errorf("steady workload rejected: %w", err)
	}
	logger.Info("resource limiter allows steady load",
		"cpu_percent", steady.CPUPercent,
		"mem_bytes", steady.MemoryBytes,
		"disk_bytes", steady.DiskBytes,
	)

	spike := sandbox.ResourceUsage{
		CPUPercent:  cfg.cpuLimit * 1.5,
		MemoryBytes: cfg.memLimit,
		DiskBytes:   cfg.diskLimit * 2,
	}
	if err := manager.Enforce(safeFile, cfg.allowHost, spike); err == nil {
		return fmt.Errorf("resource spike should be blocked")
	} else {
		logger.Info("resource limiter blocked spike", "err", err)
	}

	return nil
}
