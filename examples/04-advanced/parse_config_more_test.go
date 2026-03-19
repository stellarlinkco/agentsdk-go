package main

import (
	"flag"
	"io"
	"os"
	"testing"
	"time"
)

func TestParseConfig_Flags(t *testing.T) {
	origArgs := os.Args
	origFS := flag.CommandLine
	t.Cleanup(func() {
		os.Args = origArgs
		flag.CommandLine = origFS
	})

	flag.CommandLine = flag.NewFlagSet("advanced", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	os.Args = []string{
		"advanced",
		"--prompt=hi",
		"--session-id=s1",
		"--owner=o1",
		"--project-root=/tmp",
		"--enable-hooks=false",
		"--enable-mcp=true",
		"--mcp-server=stdio://echo mcp",
		"--enable-sandbox=false",
		"--allow-host=example.com",
		"--cpu-limit=12.5",
		"--mem-mb=1",
		"--disk-mb=2",
			"--enable-skills=false",
			"--enable-subagents=false",
			"--enable-trace=false",
			"--trace-dir=trace-out",
			"--trace-skills=true",
		"--slow-threshold=3ms",
		"--tool-latency=4ms",
		"--timeout=5ms",
		"--middleware-timeout=6ms",
		"--hook-timeout=7ms",
		"--max-iterations=9",
		"--rps=10",
		"--burst=11",
		"--concurrent=12",
		"--force-skill=",
		"--target-subagent=",
		"--severity=low",
	}

	cfg := parseConfig()
	if cfg.prompt != "hi" || cfg.sessionID != "s1" || cfg.owner != "o1" {
		t.Fatalf("cfg=%+v", cfg)
	}
	if cfg.projectRoot != "/tmp" {
		t.Fatalf("projectRoot=%q", cfg.projectRoot)
	}
	if cfg.enableMCP != true || cfg.mcpServer == "" {
		t.Fatalf("mcp=%+v", cfg)
	}
	if cfg.enableSandbox != false {
		t.Fatalf("sandbox=%+v", cfg)
	}
	if cfg.cpuLimit != 12.5 {
		t.Fatalf("cpu=%v", cfg.cpuLimit)
	}
	if cfg.memLimit != 1*1024*1024 || cfg.diskLimit != 2*1024*1024 {
		t.Fatalf("limits mem=%d disk=%d", cfg.memLimit, cfg.diskLimit)
	}
	if cfg.slowThreshold != 3*time.Millisecond || cfg.toolLatency != 4*time.Millisecond {
		t.Fatalf("thresholds=%+v", cfg)
	}
	if cfg.runTimeout != 5*time.Millisecond || cfg.middlewareTimeout != 6*time.Millisecond || cfg.hookTimeout != 7*time.Millisecond {
		t.Fatalf("timeouts=%+v", cfg)
	}
	if cfg.maxIterations != 9 || cfg.rps != 10 || cfg.burst != 11 || cfg.concurrent != 12 {
		t.Fatalf("limits=%+v", cfg)
	}
	if cfg.severity != "low" {
		t.Fatalf("severity=%q", cfg.severity)
	}
}
