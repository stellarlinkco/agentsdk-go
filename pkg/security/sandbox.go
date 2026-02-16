package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cexll/agentsdk-go/pkg/config"
)

var (
	// ErrPathNotAllowed is returned when a path escapes the configured sandbox roots.
	ErrPathNotAllowed = errors.New("security: path not in sandbox allowlist")
)

// Sandbox is the first defensive layer: filesystem boundaries and command checks.
type Sandbox struct {
	mu        sync.RWMutex
	allowList []string
	validator *Validator
	resolver  *PathResolver
	disabled  bool // When true, all validation is skipped

	permissionRoot string
	permissions    *PermissionMatcher
	permOnce       sync.Once
	permErr        error
	permLoaded     bool
	auditLog       []PermissionAudit
}

// NewSandbox creates a sandbox rooted at workDir.
func NewSandbox(workDir string) *Sandbox {
	root := normalizePath(workDir)
	if root == "" {
		root = string(filepath.Separator)
	}
	return &Sandbox{
		allowList:      []string{root},
		validator:      NewValidator(),
		resolver:       NewPathResolver(),
		disabled:       false,
		permissionRoot: root,
	}
}

// NewDisabledSandbox creates a sandbox that skips all validation.
// Used when sandbox is explicitly disabled in configuration.
func NewDisabledSandbox() *Sandbox {
	return &Sandbox{
		disabled: true,
	}
}

// AllowShellMetachars enables shell pipes and metacharacters (CLI mode).
func (s *Sandbox) AllowShellMetachars(allow bool) {
	if s != nil && s.validator != nil {
		s.validator.AllowShellMetachars(allow)
	}
}

// SetCommandLimits overrides the maximum command length and argument count.
func (s *Sandbox) SetCommandLimits(maxBytes, maxArgs int) {
	if s != nil && s.validator != nil {
		s.validator.SetMaxCommandBytes(maxBytes)
		s.validator.SetMaxArgs(maxArgs)
	}
}

// Allow registers additional absolute prefixes that commands may touch.
func (s *Sandbox) Allow(path string) {
	normalized := normalizePath(path)
	if normalized == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.allowList {
		if existing == normalized {
			return
		}
	}
	s.allowList = append(s.allowList, normalized)
}

// ValidatePath ensures the path resolves within the sandbox allow list.
func (s *Sandbox) ValidatePath(path string) error {
	if s != nil && s.disabled {
		return nil
	}

	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("security: empty path supplied")
	}

	resolved, err := s.resolver.Resolve(path)
	if err != nil {
		return fmt.Errorf("security: resolve failed: %w", err)
	}

	abs := normalizePath(resolved)

	s.mu.RLock()
	allowCopy := append([]string(nil), s.allowList...)
	s.mu.RUnlock()

	for _, allowed := range allowCopy {
		if withinSandbox(abs, allowed) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrPathNotAllowed, abs)
}

// ValidateCommand is the second defense line, preventing obviously dangerous commands.
func (s *Sandbox) ValidateCommand(cmd string) error {
	if s != nil && s.disabled {
		return nil
	}

	if err := s.validator.Validate(cmd); err != nil {
		return fmt.Errorf("security: %w", err)
	}
	return nil
}

// LoadPermissions parses permissions rules from the layered .claude/settings*.json
// files rooted at projectRoot. Missing files are tolerated. When called multiple
// times the latest rules replace any previously loaded matcher.
func (s *Sandbox) LoadPermissions(projectRoot string) error {
	if s == nil {
		return errors.New("security: sandbox is nil")
	}

	effectiveRoot := strings.TrimSpace(projectRoot)
	if effectiveRoot == "" {
		effectiveRoot = strings.TrimSpace(s.permissionRoot)
	}
	if effectiveRoot == "" {
		if cwd, err := os.Getwd(); err == nil {
			effectiveRoot = cwd
		}
	}

	loader := config.SettingsLoader{ProjectRoot: effectiveRoot}
	settings, err := loader.Load()
	if err != nil {
		s.mu.Lock()
		s.permErr = err
		s.permLoaded = true
		s.mu.Unlock()
		return fmt.Errorf("security: load permissions: %w", err)
	}

	if settings == nil {
		settings = &config.Settings{}
	}
	matcher, err := NewPermissionMatcher(settings.Permissions)
	if err != nil {
		s.mu.Lock()
		s.permErr = err
		s.permLoaded = true
		s.mu.Unlock()
		return fmt.Errorf("security: build permission matcher: %w", err)
	}

	s.mu.Lock()
	s.permissionRoot = effectiveRoot
	s.permissions = matcher
	s.permErr = nil
	s.permLoaded = true
	s.auditLog = nil
	s.mu.Unlock()
	return nil
}

// CheckToolPermission evaluates tool invocation against configured allow/ask/deny
// rules. Denials and prompts are returned to the caller; missing or empty rules
// default to allow to preserve backward compatibility.
func (s *Sandbox) CheckToolPermission(toolName string, params map[string]any) (PermissionDecision, error) {
	if s == nil || s.disabled {
		return PermissionDecision{Action: PermissionAllow}, nil
	}

	if err := s.ensurePermissionsLoaded(); err != nil {
		return PermissionDecision{}, err
	}

	s.mu.RLock()
	matcher := s.permissions
	s.mu.RUnlock()
	if matcher == nil {
		return PermissionDecision{Action: PermissionAllow}, nil
	}

	decision := matcher.Match(toolName, params)
	if decision.Action != PermissionUnknown {
		s.recordAudit(decision)
	}
	return decision, nil
}

// PermissionAudits returns a snapshot of audited permission decisions.
func (s *Sandbox) PermissionAudits() []PermissionAudit {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PermissionAudit, len(s.auditLog))
	copy(out, s.auditLog)
	return out
}

func (s *Sandbox) ensurePermissionsLoaded() error {
	s.mu.RLock()
	loaded := s.permLoaded
	err := s.permErr
	s.mu.RUnlock()
	if loaded {
		return err
	}

	s.permOnce.Do(func() {
		if s.permissions != nil {
			s.mu.Lock()
			s.permLoaded = true
			s.permErr = nil
			s.mu.Unlock()
			return
		}
		s.permErr = s.LoadPermissions(s.permissionRoot)
	})

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.permErr
}

func (s *Sandbox) recordAudit(decision PermissionDecision) {
	entry := PermissionAudit{
		Tool:      decision.Tool,
		Target:    decision.Target,
		Rule:      decision.Rule,
		Action:    decision.Action,
		Timestamp: time.Now(),
	}
	s.mu.Lock()
	s.auditLog = append(s.auditLog, entry)
	s.mu.Unlock()
}

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func withinSandbox(path, prefix string) bool {
	if prefix == "" {
		return false
	}
	path = filepath.Clean(path)
	prefix = filepath.Clean(prefix)

	if path == prefix {
		return true
	}
	if prefix == string(filepath.Separator) {
		return true
	}
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.HasPrefix(path, prefix)
}
