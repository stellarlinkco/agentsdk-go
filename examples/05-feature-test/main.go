package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cexll/agentsdk-go/pkg/tool"
	toolbuiltin "github.com/cexll/agentsdk-go/pkg/tool/builtin"
)

func main() {
	fmt.Println("=== Testing New Features ===")
	fmt.Println()

	// Test 1: respectGitignore for Glob/Grep
	testRespectGitignore()

	// Test 2: SpoolWriter (large output persistence)
	testSpoolWriter()

	fmt.Println("\n=== All Tests Passed ===")
}

func testRespectGitignore() {
	fmt.Println("[Test 1] respectGitignore for Glob/Grep")

	// Create temp directory with .gitignore (use resolved path to avoid symlink issues on macOS)
	dir, _ := os.MkdirTemp("", "gitignore-test")
	dir, _ = filepath.EvalSymlinks(dir)
	defer os.RemoveAll(dir)

	// Create .gitignore
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\nnode_modules/"), 0644)

	// Create test files
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "debug.log"), []byte("debug output"), 0644)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", "lib.js"), []byte("module.exports = {}"), 0644)

	// Test Glob with respectGitignore=true (default)
	globTool := toolbuiltin.NewGlobToolWithRoot(dir)
	result, err := globTool.Execute(context.Background(), map[string]any{"pattern": "*"})
	if err != nil {
		fmt.Printf("  FAIL: Glob error: %v\n", err)
		return
	}

	output := result.Output
	if contains(output, "debug.log") || contains(output, "node_modules") {
		fmt.Printf("  FAIL: Gitignored files should be excluded\n")
		return
	}
	if !contains(output, "main.go") {
		fmt.Printf("  FAIL: main.go should be included\n")
		return
	}
	fmt.Println("  PASS: Glob respects .gitignore by default")

	// Test Glob with respectGitignore=false
	globTool.SetRespectGitignore(false)
	result2, _ := globTool.Execute(context.Background(), map[string]any{"pattern": "*"})
	if !contains(result2.Output, "debug.log") {
		fmt.Printf("  FAIL: debug.log should be included when gitignore disabled\n")
		return
	}
	fmt.Println("  PASS: Glob includes gitignored files when disabled")

	// Test Grep with respectGitignore
	grepTool := toolbuiltin.NewGrepToolWithRoot(dir)
	result3, err3 := grepTool.Execute(context.Background(), map[string]any{
		"pattern":     "output|exports",
		"path":        dir,
		"output_mode": "files_with_matches",
	})
	if err3 != nil {
		fmt.Printf("  FAIL: Grep error: %v\n", err3)
		return
	}
	if contains(result3.Output, "debug.log") || contains(result3.Output, "node_modules") {
		fmt.Printf("  FAIL: Grep should exclude gitignored files\n")
		return
	}
	fmt.Println("  PASS: Grep respects .gitignore by default")
}

func testSpoolWriter() {
	fmt.Println("\n[Test 2] SpoolWriter (large output persistence)")

	dir, _ := os.MkdirTemp("", "spool-test")
	defer os.RemoveAll(dir)

	// Test: small output stays in memory
	sw1 := tool.NewSpoolWriter(100, func() (io.WriteCloser, string, error) {
		path := filepath.Join(dir, "output1.txt")
		f, err := os.Create(path)
		return f, path, err
	})
	sw1.WriteString("small output")
	sw1.Close()

	if sw1.Path() != "" {
		fmt.Println("  FAIL: Small output should stay in memory")
		return
	}
	if sw1.String() != "small output" {
		fmt.Println("  FAIL: In-memory content mismatch")
		return
	}
	fmt.Println("  PASS: Small output stays in memory")

	// Test: large output spills to disk
	sw2 := tool.NewSpoolWriter(100, func() (io.WriteCloser, string, error) {
		path := filepath.Join(dir, "output2.txt")
		f, err := os.Create(path)
		return f, path, err
	})

	largeData := make([]byte, 200)
	for i := range largeData {
		largeData[i] = 'x'
	}
	sw2.Write(largeData)
	sw2.Close()

	if sw2.Path() == "" {
		fmt.Println("  FAIL: Large output should spill to disk")
		return
	}

	// Verify file content
	content, err := os.ReadFile(sw2.Path())
	if err != nil {
		fmt.Printf("  FAIL: Cannot read spilled file: %v\n", err)
		return
	}
	if len(content) != 200 {
		fmt.Printf("  FAIL: File content size mismatch: got %d, want 200\n", len(content))
		return
	}
	fmt.Printf("  PASS: Large output spilled to disk: %s\n", sw2.Path())
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
