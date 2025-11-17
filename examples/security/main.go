package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/cexll/agentsdk-go/pkg/security"
)

// This example focuses purely on the security primitives: sandbox boundaries,
// path resolution, and command vetting without involving any AI runtime.
func main() {
	logger := log.New(os.Stdout, "[security-demo] ", log.LstdFlags|log.Lmicroseconds)

	workDir := filepath.Join(os.TempDir(), "agentsdk-security-demo")
	prepareWorkspace(logger, workDir)

	sandbox := security.NewSandbox(workDir)
	logger.Printf("Sandbox allow list initialised with root %q\n", workDir)

	runPathResolverDemo(logger, sandbox, workDir)
	runValidatorDemo(logger)
	runSandboxCommandDemo(logger, sandbox, workDir)
	runWhitelistDemo(logger, sandbox)
}

// prepareWorkspace ensures our fake workspace exists so the resolver can walk it.
func prepareWorkspace(logger *log.Logger, dir string) {
	if err := os.MkdirAll(filepath.Join(dir, "reports"), 0o755); err != nil {
		logger.Fatalf("failed to build sandbox root: %v", err)
	}
}

// runPathResolverDemo shows how PathResolver canonicalises safe paths and
// blocks traversal attempts before the sandbox even inspects them.
func runPathResolverDemo(logger *log.Logger, sandbox *security.Sandbox, root string) {
	logger.Println("---- Filesystem guard rails ----")

	resolver := security.NewPathResolver()
	safePath := filepath.Join(root, "reports", "weekly.txt")
	resolved, err := resolver.Resolve(safePath)
	logResult(logger, fmt.Sprintf("PathResolver.Resolve(%q)", safePath), err)
	if err == nil {
		logger.Printf("Resolved path stays inside sandbox: %s\n", resolved)
	}

	escapePath := filepath.Join(root, "..", "..", "etc", "passwd")
	_, err = resolver.Resolve(escapePath)
	logResult(logger, fmt.Sprintf("PathResolver.Resolve(%q)", escapePath), err)

	logResult(logger, fmt.Sprintf("Sandbox.ValidatePath(%q)", safePath), sandbox.ValidatePath(safePath))
	logResult(logger, fmt.Sprintf("Sandbox.ValidatePath(%q)", escapePath), sandbox.ValidatePath(escapePath))
}

// runValidatorDemo highlights raw validator usage so callers can pre-flight
// commands even without a Sandbox instance.
func runValidatorDemo(logger *log.Logger) {
	logger.Println("---- Command validation primitives ----")
	validator := security.NewValidator()

	safeCmd := `cat "/etc/hosts"`
	logResult(logger, fmt.Sprintf("Validator.Validate(%q)", safeCmd), validator.Validate(safeCmd))

	dangerCmd := `rm -rf /tmp/rogue`
	logResult(logger, fmt.Sprintf("Validator.Validate(%q)", dangerCmd), validator.Validate(dangerCmd))
}

// runSandboxCommandDemo shows how sandbox wraps validator checks and reports
// the result alongside path validation to form a coherent audit trail.
func runSandboxCommandDemo(logger *log.Logger, sandbox *security.Sandbox, root string) {
	logger.Println("---- Sandbox command checks ----")

	listCmd := fmt.Sprintf("ls %s", filepath.Join(root, "reports"))
	logResult(logger, fmt.Sprintf("Sandbox.ValidateCommand(%q)", listCmd), sandbox.ValidateCommand(listCmd))

	eraseCmd := "rm --no-preserve-root /"
	logResult(logger, fmt.Sprintf("Sandbox.ValidateCommand(%q)", eraseCmd), sandbox.ValidateCommand(eraseCmd))
}

// runWhitelistDemo adds an external path after confirming it would otherwise fail,
// demonstrating how to extend the sandbox without sacrificing safety.
func runWhitelistDemo(logger *log.Logger, sandbox *security.Sandbox) {
	logger.Println("---- Allow list configuration ----")

	sharedCache := filepath.Join(os.TempDir(), "agentsdk-shared-cache")
	logResult(logger, fmt.Sprintf("Sandbox.ValidatePath(%q)", sharedCache), sandbox.ValidatePath(sharedCache))

	logger.Printf("Registering extra prefix %q via Sandbox.Allow\n", sharedCache)
	sandbox.Allow(sharedCache)
	logResult(logger, fmt.Sprintf("Sandbox.ValidatePath(%q)", sharedCache), sandbox.ValidatePath(sharedCache))
}

// logResult prints consistent PASS/BLOCKED messages for each guard.
func logResult(logger *log.Logger, action string, err error) {
	if err != nil {
		logger.Printf("%s => BLOCKED: %v\n", action, err)
		return
	}
	logger.Printf("%s => PASS\n", action)
}
