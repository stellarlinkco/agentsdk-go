package guardrails_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

type goListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

func TestImportGuardrails(t *testing.T) {
	t.Parallel()

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok || buildInfo == nil || buildInfo.Main.Path == "" {
		t.Fatalf("ReadBuildInfo: missing module path")
	}
	modulePath := buildInfo.Main.Path

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list -json ./...: %v\n%s", err, stderr.String())
	}

	dec := json.NewDecoder(&stdout)

	var violations []string
	for {
		var p goListPackage
		if err := dec.Decode(&p); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode go list json: %v", err)
		}
		if !strings.HasPrefix(p.ImportPath, modulePath+"/") {
			continue
		}

		fromLayer := layerOf(modulePath, p.ImportPath)
		if fromLayer == "" {
			continue
		}

		for _, imp := range p.Imports {
			if !strings.HasPrefix(imp, modulePath+"/") {
				continue
			}
			toLayer := layerOf(modulePath, imp)
			if toLayer == "" {
				continue
			}
			if isForbiddenEdge(fromLayer, toLayer) {
				violations = append(violations, fmt.Sprintf("%s -> %s (%s -> %s)", p.ImportPath, imp, fromLayer, toLayer))
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("import guardrails violated:\n%s", strings.Join(violations, "\n"))
	}
}

func layerOf(modulePath, importPath string) string {
	rel := strings.TrimPrefix(importPath, modulePath+"/")
	switch {
	case strings.HasPrefix(rel, "pkg/"):
		return "pkg"
	case strings.HasPrefix(rel, "cmd/"):
		return "cmd"
	case strings.HasPrefix(rel, "examples/"):
		return "examples"
	default:
		return ""
	}
}

func isForbiddenEdge(from, to string) bool {
	switch from {
	case "pkg":
		return to == "cmd" || to == "examples"
	case "cmd":
		return to == "examples"
	default:
		return false
	}
}
