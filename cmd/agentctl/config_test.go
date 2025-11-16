package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCommandLifecycle(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := configCommand([]string{"--config", cfgPath, "init"}, cfgPath, ioStreams{out: io.Discard, err: io.Discard}); err != nil {
		t.Fatalf("config init: %v", err)
	}
	if err := configCommand([]string{"--config", cfgPath, "set", "default_model", "demo"}, cfgPath, ioStreams{out: io.Discard, err: io.Discard}); err != nil {
		t.Fatalf("config set default_model: %v", err)
	}
	if err := configCommand([]string{"--config", cfgPath, "set", "mcp_servers", "http://a,http://b"}, cfgPath, ioStreams{out: io.Discard, err: io.Discard}); err != nil {
		t.Fatalf("config set mcp_servers: %v", err)
	}
	var out bytes.Buffer
	if err := configCommand([]string{"--config", cfgPath, "get", "default_model"}, cfgPath, ioStreams{out: &out, err: io.Discard}); err != nil {
		t.Fatalf("config get: %v", err)
	}
	if strings.TrimSpace(out.String()) != "demo" {
		t.Fatalf("unexpected default_model: %s", out.String())
	}
	var list bytes.Buffer
	if err := configCommand([]string{"--config", cfgPath, "list"}, cfgPath, ioStreams{out: &list, err: io.Discard}); err != nil {
		t.Fatalf("config list: %v", err)
	}
	if !strings.Contains(list.String(), "mcp_servers=http://a,http://b") {
		t.Fatalf("list missing mcp servers: %s", list.String())
	}
}
