package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/plugins"
)

// Validator enforces constraints on ProjectConfig.
type Validator interface {
	Validate(*ProjectConfig) error
}

// DefaultValidator applies structural checks and guards against obvious abuse.
type DefaultValidator struct {
	root       string
	maxPlugins int
	maxEnvVars int
}

// NewDefaultValidator builds a validator anchored to the provided project root.
func NewDefaultValidator(root string) *DefaultValidator {
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return &DefaultValidator{
		root:       abs,
		maxPlugins: 64,
		maxEnvVars: 64,
	}
}

// Validate checks structural integrity, rollback behaviour, and sandbox safety.
func (v *DefaultValidator) Validate(cfg *ProjectConfig) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	if strings.TrimSpace(cfg.Version) == "" {
		return errors.New("config version is required")
	}
	if !plugins.IsSemVer(cfg.Version) {
		return fmt.Errorf("invalid config version %q", cfg.Version)
	}
	if cfg.ClaudeDir == "" {
		return errors.New("claude directory unresolved")
	}
	if len(cfg.Environment) > v.maxEnvVars {
		return fmt.Errorf("too many environment variables: %d > %d", len(cfg.Environment), v.maxEnvVars)
	}
	if err := sanitizeEnv(cfg.Environment); err != nil {
		return err
	}
	manifestIndex := make(map[string]*plugins.Manifest, len(cfg.Manifests))
	for _, mf := range cfg.Manifests {
		if mf == nil {
			return errors.New("nil manifest encountered")
		}
		manifestIndex[mf.Name] = mf
	}

	names := make(map[string]struct{})
	active := 0
	for _, ref := range cfg.Plugins {
		if ref.Disabled {
			continue
		}
		active++
		if strings.TrimSpace(ref.Name) == "" {
			return errors.New("plugin name cannot be empty")
		}
		if _, exists := names[ref.Name]; exists {
			return fmt.Errorf("duplicate plugin %s", ref.Name)
		}
		names[ref.Name] = struct{}{}
		if ref.MinVersion != "" && !plugins.IsSemVer(ref.MinVersion) {
			return fmt.Errorf("plugin %s min_version invalid", ref.Name)
		}
		if ref.Path != "" && (filepath.IsAbs(ref.Path) || strings.HasPrefix(filepath.Clean(ref.Path), "..")) {
			return fmt.Errorf("plugin %s path escapes claude dir", ref.Name)
		}
		mf, ok := manifestIndex[ref.Name]
		if !ok {
			if ref.Optional {
				continue
			}
			return fmt.Errorf("plugin %s manifest missing", ref.Name)
		}
		if ref.MinVersion != "" {
			if compareSemver(mf.Version, ref.MinVersion) < 0 {
				return fmt.Errorf("plugin %s version %s below %s", ref.Name, mf.Version, ref.MinVersion)
			}
		}
	}
	if active > v.maxPlugins {
		return fmt.Errorf("too many active plugins: %d > %d", active, v.maxPlugins)
	}
	if err := v.validateSandbox(cfg.Sandbox.AllowedPaths); err != nil {
		return err
	}
	return nil
}

func (v *DefaultValidator) validateSandbox(paths []string) error {
	projectRoot := v.root
	for _, rel := range paths {
		if rel == "" {
			continue
		}
		if filepath.IsAbs(rel) {
			return fmt.Errorf("sandbox path must be relative: %s", rel)
		}
		clean := filepath.Clean(rel)
		if strings.HasPrefix(clean, "..") {
			return fmt.Errorf("sandbox path escapes project: %s", rel)
		}
		abs := filepath.Join(projectRoot, clean)
		if !strings.HasPrefix(abs, projectRoot) {
			return fmt.Errorf("sandbox path outside project: %s", rel)
		}
	}
	return nil
}

var envKeyPattern = regexp.MustCompile(`^[A-Z0-9_]+$`)

func sanitizeEnv(env map[string]string) error {
	for key, value := range env {
		if !envKeyPattern.MatchString(strings.TrimSpace(key)) {
			return fmt.Errorf("invalid environment key %q", key)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("environment value for %s contains newline", key)
		}
		if len(value) > 1024 {
			return fmt.Errorf("environment value for %s too long", key)
		}
	}
	return nil
}

func compareSemver(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(v, "v")
		parts := strings.SplitN(v, "-", 2)
		nums := strings.Split(parts[0], ".")
		res := []int{0, 0, 0}
		for i := 0; i < len(nums) && i < 3; i++ {
			res[i] = parseInt(nums[i])
		}
		return res
	}
	av := parse(a)
	bv := parse(b)
	for i := 0; i < 3; i++ {
		if av[i] > bv[i] {
			return 1
		}
		if av[i] < bv[i] {
			return -1
		}
	}
	return 0
}

func parseInt(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			break
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
