package packager

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/plugins"
)

var (
	ErrUnsafeArchive     = errors.New("packager: unsafe path in archive")
	ErrDestinationExists = errors.New("packager: destination already exists")
)

// Packager handles plugin packaging/import/export for the .claude/plugins dir.
type Packager struct {
	root  string
	trust *plugins.TrustStore
}

// NewPackager builds a packager rooted at dir. When store is nil a permissive
// trust store that allows unsigned manifests is used.
func NewPackager(root string, store *plugins.TrustStore) (*Packager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("packager: root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("packager: resolve root: %w", err)
	}
	if store == nil {
		store = plugins.NewTrustStore()
		store.AllowUnsigned(true)
	}
	return &Packager{root: absRoot, trust: store}, nil
}

// Root returns the absolute directory the packager operates on.
func (p *Packager) Root() string {
	if p == nil {
		return ""
	}
	return p.root
}

// Export packages the plugin directory identified by name into writer.
func (p *Packager) Export(name string, w io.Writer) (*plugins.Manifest, error) {
	if p == nil {
		return nil, errors.New("packager: instance is nil")
	}
	dir := filepath.Join(p.root, filepath.Clean(name))
	return p.PackageDir(dir, w)
}

// PackageDir compresses an arbitrary plugin directory under root.
func (p *Packager) PackageDir(dir string, w io.Writer) (*plugins.Manifest, error) {
	if p == nil {
		return nil, errors.New("packager: instance is nil")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(absDir, p.root) {
		return nil, fmt.Errorf("packager: directory %s outside root", dir)
	}
	manifestPath, err := plugins.FindManifest(absDir)
	if err != nil {
		return nil, err
	}
	manifest, err := plugins.LoadManifest(manifestPath, plugins.WithRoot(absDir), plugins.WithTrustStore(p.trust))
	if err != nil {
		return nil, err
	}

	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	err = filepath.WalkDir(absDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
			return ErrUnsafeArchive
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := io.Copy(tw, file); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// Import extracts a plugin archive into root/name. Any existing directory makes
// the operation fail to avoid overwriting installed plugins.
func (p *Packager) Import(r io.Reader, name string) (*plugins.Manifest, error) {
	if p == nil {
		return nil, errors.New("packager: instance is nil")
	}
	dest := filepath.Join(p.root, filepath.Clean(name))
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	if err := ensureEmptyDir(destAbs); err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := p.restoreEntry(destAbs, header, tr); err != nil {
			return nil, err
		}
	}
	manifestPath, err := plugins.FindManifest(destAbs)
	if err != nil {
		return nil, err
	}
	manifest, err := plugins.LoadManifest(manifestPath, plugins.WithRoot(destAbs), plugins.WithTrustStore(p.trust))
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

func (p *Packager) restoreEntry(dest string, header *tar.Header, r io.Reader) error {
	clean := filepath.Clean(header.Name)
	if clean == "." {
		return nil
	}
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return ErrUnsafeArchive
	}
	target := filepath.Join(dest, clean)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(targetAbs, dest) {
		return ErrUnsafeArchive
	}
	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(targetAbs, fs.FileMode(header.Mode))
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(targetAbs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(header.Mode))
		if err != nil {
			return err
		}
		if _, err := io.Copy(file, r); err != nil {
			file.Close()
			return err
		}
		return file.Close()
	default:
		return nil
	}
}

func ensureEmptyDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return os.MkdirAll(path, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return ErrDestinationExists
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return ErrDestinationExists
	}
	return nil
}
