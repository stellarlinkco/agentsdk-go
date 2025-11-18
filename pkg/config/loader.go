package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cexll/agentsdk-go/pkg/plugins"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

const (
	claudeDirName  = ".claude"
	pluginsDirName = "plugins"
)

// ProjectConfig captures the declarative agent definition under .claude/.
type ProjectConfig struct {
	Version     string            `json:"version" yaml:"version"`
	Description string            `json:"description" yaml:"description"`
	Environment map[string]string `json:"environment" yaml:"environment"`
	Plugins     []PluginRef       `json:"plugins" yaml:"plugins"`
	Sandbox     SandboxBlock      `json:"sandbox" yaml:"sandbox"`

	ClaudeDir  string              `json:"-" yaml:"-"`
	SourcePath string              `json:"-" yaml:"-"`
	SourceHash string              `json:"-" yaml:"-"`
	Manifests  []*plugins.Manifest `json:"-" yaml:"-"`
}

// PluginRef declares where a plugin lives and how it should be validated.
type PluginRef struct {
	Name       string `json:"name" yaml:"name"`
	Path       string `json:"path" yaml:"path"`
	Optional   bool   `json:"optional" yaml:"optional"`
	Disabled   bool   `json:"disabled" yaml:"disabled"`
	MinVersion string `json:"min_version" yaml:"min_version"`
}

// SandboxBlock constrains runtime IO.
type SandboxBlock struct {
	AllowedPaths []string `json:"allowed_paths" yaml:"allowed_paths"`
}

// Normalize trims whitespace and coerces relative paths.
func (c *ProjectConfig) Normalize() {
	if c == nil {
		return
	}
	if c.Environment == nil {
		c.Environment = map[string]string{}
	} else {
		for k, v := range c.Environment {
			c.Environment[k] = strings.TrimSpace(v)
		}
	}
	for i := range c.Plugins {
		c.Plugins[i].Name = strings.TrimSpace(c.Plugins[i].Name)
		if c.Plugins[i].Path != "" {
			c.Plugins[i].Path = filepath.Clean(c.Plugins[i].Path)
		}
	}
	for i := range c.Sandbox.AllowedPaths {
		c.Sandbox.AllowedPaths[i] = filepath.Clean(c.Sandbox.AllowedPaths[i])
	}
}

// Loader loads, validates, and caches config state.
type Loader struct {
	root string

	validator  Validator
	trustStore *plugins.TrustStore

	explicitClaude string

	mu   sync.Mutex
	last atomic.Pointer[ProjectConfig]
}

// LoaderOption customizes loader behaviour.
type LoaderOption func(*Loader)

// WithValidator injects a custom Validator.
func WithValidator(v Validator) LoaderOption {
	return func(l *Loader) {
		l.validator = v
	}
}

// WithTrustStore overrides the trust store used for plugin verification.
func WithTrustStore(store *plugins.TrustStore) LoaderOption {
	return func(l *Loader) {
		l.trustStore = store
	}
}

// WithClaudeDir forces the loader to use a specific .claude directory.
func WithClaudeDir(path string) LoaderOption {
	return func(l *Loader) {
		l.explicitClaude = path
	}
}

// NewLoader wires a loader for the provided project root.
func NewLoader(root string, opts ...LoaderOption) (*Loader, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("project root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	loader := &Loader{
		root: absRoot,
	}
	loader.validator = NewDefaultValidator(absRoot)
	loader.trustStore = plugins.NewTrustStore()
	loader.trustStore.AllowUnsigned(true)
	for _, opt := range opts {
		opt(loader)
	}
	if loader.validator == nil {
		loader.validator = NewDefaultValidator(absRoot)
	}
	if loader.trustStore == nil {
		loader.trustStore = plugins.NewTrustStore()
		loader.trustStore.AllowUnsigned(true)
	}
	if loader.explicitClaude != "" {
		dir, err := filepath.Abs(loader.explicitClaude)
		if err != nil {
			return nil, fmt.Errorf("resolve claude dir: %w", err)
		}
		loader.explicitClaude = dir
	}
	return loader, nil
}

// Root returns the absolute project root.
func (l *Loader) Root() string {
	return l.root
}

// Last returns the most recent valid configuration.
func (l *Loader) Last() (*ProjectConfig, bool) {
	cfg := l.last.Load()
	if cfg == nil {
		return nil, false
	}
	return cfg, true
}

// Load resolves .claude/, parses config, discovers plugins, and validates both.
func (l *Loader) Load() (*ProjectConfig, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cfg, err := l.loadOnce()
	if err != nil {
		return nil, err
	}
	l.last.Store(cfg)
	return cfg, nil
}

// Reload attempts to refresh configuration keeping the last good state on error.
func (l *Loader) Reload() (*ProjectConfig, error) {
	prev, _ := l.Last()
	cfg, err := l.Load()
	if err != nil {
		if prev != nil {
			return prev, fmt.Errorf("reload failed, keeping last good config: %w", err)
		}
		return nil, err
	}
	return cfg, nil
}

func (l *Loader) loadOnce() (*ProjectConfig, error) {
	claudeDir, err := l.locateClaudeDir()
	if err != nil {
		return nil, err
	}
	cfgPath, raw, err := readConfigPayload(claudeDir)
	var cfg *ProjectConfig
	switch {
	case err == nil:
		cfg, err = decodeProjectConfig(raw)
		if err != nil {
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist):
		cfg = &ProjectConfig{Environment: map[string]string{}}
		raw = []byte{}
	default:
		return nil, err
	}
	cfg.ClaudeDir = claudeDir
	cfg.SourcePath = cfgPath

	if err := l.populatePlugins(cfg); err != nil {
		return nil, err
	}
	if l.validator != nil {
		if err := l.validator.Validate(cfg); err != nil {
			return nil, err
		}
	}
	cfg.SourceHash = computeConfigHash(raw, cfg.Manifests)
	return cfg, nil
}

func (l *Loader) locateClaudeDir() (string, error) {
	if l.explicitClaude != "" {
		if info, err := os.Stat(l.explicitClaude); err == nil && info.IsDir() {
			return l.explicitClaude, nil
		}
		return "", fmt.Errorf(".claude override %s not found", l.explicitClaude)
	}
	for _, dir := range l.claudeCandidates() {
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if info.IsDir() {
			abs, err := filepath.Abs(dir)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
	}
	return "", fmt.Errorf(".claude directory not found under %s", l.root)
}

func (l *Loader) claudeCandidates() []string {
	var dirs []string
	seen := map[string]struct{}{}
	current := l.root
	for {
		candidate := filepath.Join(current, claudeDirName)
		if _, ok := seen[candidate]; !ok {
			dirs = append(dirs, candidate)
			seen[candidate] = struct{}{}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, claudeDirName)
		if _, ok := seen[candidate]; !ok {
			dirs = append(dirs, candidate)
		}
	}
	return dirs
}

func readConfigPayload(dir string) (string, []byte, error) {
	candidates := []string{"config.yaml", "config.yml", "config.json"}
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return path, nil, err
		}
		return path, data, nil
	}
	return filepath.Join(dir, "config.yaml"), nil, fs.ErrNotExist
}

func decodeProjectConfig(raw []byte) (*ProjectConfig, error) {
	cfg := &ProjectConfig{}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, errors.New("config payload is empty")
	}
	if err := decodeMixedYAMLJSON(raw, cfg); err != nil {
		return nil, err
	}
	cfg.Normalize()
	return cfg, nil
}

func (l *Loader) populatePlugins(cfg *ProjectConfig) error {
	pluginsRoot := filepath.Join(cfg.ClaudeDir, pluginsDirName)
	specs := cfg.Plugins
	if len(specs) == 0 {
		auto, err := discoverPluginRefs(pluginsRoot)
		if err != nil {
			return err
		}
		cfg.Plugins = auto
		specs = auto
	}
	manifests := make([]*plugins.Manifest, 0, len(specs))
	for _, spec := range specs {
		if spec.Disabled {
			continue
		}
		manifest, err := l.loadPlugin(spec, cfg.ClaudeDir)
		if err != nil {
			if spec.Optional && (errors.Is(err, plugins.ErrManifestNotFound) || errors.Is(err, fs.ErrNotExist)) {
				continue
			}
			return err
		}
		manifests = append(manifests, manifest)
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Name < manifests[j].Name
	})
	cfg.Manifests = manifests
	return nil
}

func (l *Loader) loadPlugin(spec PluginRef, claudeDir string) (*plugins.Manifest, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return nil, errors.New("plugin name is required")
	}
	rel := spec.Path
	if rel == "" {
		rel = filepath.Join(pluginsDirName, spec.Name)
	}
	if filepath.IsAbs(rel) {
		return nil, fmt.Errorf("plugin %s path must be relative", spec.Name)
	}
	rel = filepath.Clean(rel)
	if strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("plugin %s path escapes claude dir", spec.Name)
	}
	abs := filepath.Join(claudeDir, rel)
	abs, err := filepath.Abs(abs)
	if err != nil {
		return nil, err
	}
	pluginsRoot := filepath.Join(claudeDir, pluginsDirName)
	if within, err := withinDir(pluginsRoot, abs); err != nil || !within {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("plugin %s path escapes .claude/plugins", spec.Name)
	}
	manifestPath, err := plugins.FindManifest(abs)
	if err != nil {
		return nil, err
	}
	manifest, err := plugins.LoadManifest(manifestPath, plugins.WithRoot(abs), plugins.WithTrustStore(l.trustStore))
	if err != nil {
		return nil, err
	}
	if spec.MinVersion != "" {
		if !plugins.IsSemVer(spec.MinVersion) {
			return nil, fmt.Errorf("plugin %s min_version invalid", spec.Name)
		}
		if semver.Compare(normalizeSemver(manifest.Version), normalizeSemver(spec.MinVersion)) < 0 {
			return nil, fmt.Errorf("plugin %s version %s below required %s", manifest.Name, manifest.Version, spec.MinVersion)
		}
	}
	return manifest, nil
}

func discoverPluginRefs(dir string) ([]PluginRef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	refs := make([]PluginRef, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		refs = append(refs, PluginRef{
			Name: entry.Name(),
			Path: filepath.Join(pluginsDirName, entry.Name()),
		})
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Name < refs[j].Name
	})
	return refs, nil
}

func withinDir(base, target string) (bool, error) {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false, err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)), nil
}

func computeConfigHash(raw []byte, manifests []*plugins.Manifest) string {
	h := sha256.New()
	h.Write(raw)
	entries := make([]string, 0, len(manifests))
	for _, m := range manifests {
		entries = append(entries, fmt.Sprintf("%s@%s:%s", m.Name, m.Version, m.Digest))
	}
	sort.Strings(entries)
	for _, entry := range entries {
		h.Write([]byte(entry))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ParseProjectConfig parses yaml or json into ProjectConfig.
func ParseProjectConfig(data []byte) (*ProjectConfig, error) {
	return decodeProjectConfig(data)
}

func decodeMixedYAMLJSON(data []byte, out any) error {
	if err := yaml.Unmarshal(data, out); err == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err == nil {
		return nil
	}
	return errors.New("config decode failed: unsupported format")
}

func normalizeSemver(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
