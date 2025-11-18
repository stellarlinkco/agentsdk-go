package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

var (
	// ErrManifestNotFound indicates that the plugin directory is missing a manifest file.
	ErrManifestNotFound = errors.New("plugin manifest not found")

	manifestFilenames = []string{"manifest.yaml", "manifest.yml", "manifest.json"}

	pluginNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)
)

// Manifest describes a plugin bundle published under .claude/plugins.
type Manifest struct {
	Name         string            `json:"name" yaml:"name"`
	Version      string            `json:"version" yaml:"version"`
	Entrypoint   string            `json:"entrypoint" yaml:"entrypoint"`
	Capabilities []string          `json:"capabilities" yaml:"capabilities"`
	Metadata     map[string]string `json:"metadata" yaml:"metadata"`
	Digest       string            `json:"digest" yaml:"digest"`
	Signer       string            `json:"signer" yaml:"signer"`
	Signature    string            `json:"signature" yaml:"signature"`

	ManifestPath  string `json:"-" yaml:"-"`
	PluginDir     string `json:"-" yaml:"-"`
	EntrypointAbs string `json:"-" yaml:"-"`
	Trusted       bool   `json:"-" yaml:"-"`
}

// ManifestOption mutates manifest loading behaviour.
type ManifestOption func(*manifestOptions)

type manifestOptions struct {
	trust *TrustStore
	root  string
}

// WithTrustStore requests signature validation.
func WithTrustStore(store *TrustStore) ManifestOption {
	return func(opts *manifestOptions) {
		opts.trust = store
	}
}

// WithRoot constrains manifests/entrypoints to live under the provided root.
func WithRoot(root string) ManifestOption {
	return func(opts *manifestOptions) {
		opts.root = root
	}
}

// LoadManifest parses and validates a manifest file.
func LoadManifest(path string, opts ...ManifestOption) (*Manifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("manifest path %s is a directory", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var opt manifestOptions
	for _, fn := range opts {
		fn(&opt)
	}

	manifestDir := filepath.Dir(path)
	if opt.root == "" {
		opt.root = manifestDir
	}
	rootAbs, err := filepath.Abs(opt.root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	var mf Manifest
	if err := yaml.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	if err := validateManifestFields(&mf); err != nil {
		return nil, err
	}

	pluginDirAbs, err := filepath.Abs(manifestDir)
	if err != nil {
		return nil, fmt.Errorf("resolve plugin dir: %w", err)
	}
	entryAbs, err := secureJoin(pluginDirAbs, mf.Entrypoint)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(entryAbs, rootAbs) {
		return nil, fmt.Errorf("entrypoint escapes trusted root: %s", mf.Entrypoint)
	}

	digest, err := computeDigest(entryAbs)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(digest, mf.Digest) {
		return nil, fmt.Errorf("digest mismatch for %s", mf.Entrypoint)
	}
	payload, err := CanonicalManifestBytes(&mf)
	if err != nil {
		return nil, err
	}
	if opt.trust != nil {
		if err := opt.trust.Verify(&mf, payload); err != nil {
			return nil, err
		}
		mf.Trusted = true
	}
	if opt.trust == nil {
		mf.Trusted = true
	}

	mf.ManifestPath = path
	mf.PluginDir = pluginDirAbs
	mf.EntrypointAbs = entryAbs
	mf.Capabilities = normalizeStrings(mf.Capabilities)

	return &mf, nil
}

// DiscoverManifests walks a plugins directory and loads every manifest.
func DiscoverManifests(dir string, store *TrustStore) ([]*Manifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var manifests []*Manifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath, err := FindManifest(filepath.Join(dir, entry.Name()))
		if err != nil {
			if errors.Is(err, ErrManifestNotFound) {
				continue
			}
			return nil, err
		}
		mf, err := LoadManifest(manifestPath, WithRoot(filepath.Join(dir, entry.Name())), WithTrustStore(store))
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, mf)
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Name < manifests[j].Name
	})
	return manifests, nil
}

// FindManifest returns the manifest file path for a plugin directory.
func FindManifest(dir string) (string, error) {
	for _, name := range manifestFilenames {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", err
		}
		if info.IsDir() {
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("%w in %s", ErrManifestNotFound, dir)
}

func validateManifestFields(m *Manifest) error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if !pluginNamePattern.MatchString(m.Name) {
		return fmt.Errorf("invalid plugin name %q", m.Name)
	}
	if !IsSemVer(m.Version) {
		return fmt.Errorf("invalid semver %q", m.Version)
	}
	if strings.TrimSpace(m.Entrypoint) == "" {
		return errors.New("entrypoint is required")
	}
	if len(m.Digest) != 64 {
		return errors.New("digest must be a sha256 hex")
	}
	if _, err := hex.DecodeString(m.Digest); err != nil {
		return fmt.Errorf("invalid digest: %w", err)
	}
	return nil
}

func computeDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read entrypoint: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func secureJoin(base, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", errors.New("entrypoint must be relative")
	}
	joined := filepath.Join(base, clean)
	joinedAbs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(joinedAbs, base) {
		return "", fmt.Errorf("entrypoint escapes plugin dir: %s", rel)
	}
	return joinedAbs, nil
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	uniq := make(map[string]struct{}, len(values))
	for _, val := range values {
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			continue
		}
		uniq[strings.ToLower(trimmed)] = struct{}{}
	}
	result := make([]string, 0, len(uniq))
	for key := range uniq {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

// IsSemVer validates a minimal SemVer string.
func IsSemVer(version string) bool {
	if version == "" {
		return false
	}
	normalized := version
	if !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}
	return semver.IsValid(normalized)
}
