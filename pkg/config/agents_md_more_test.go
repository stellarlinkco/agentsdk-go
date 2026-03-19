package config

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadAgentsMDEmptyProjectRootUsesDot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("hello"), 0o600))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { require.NoError(t, os.Chdir(cwd)) })

	content, err := LoadAgentsMD("   ", nil)
	require.NoError(t, err)
	require.Equal(t, "hello", content)
}

func TestAgentsMDLoaderSkipsEmptyIncludeAndHandlesCycles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root\n@\n@a.md"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("A\n@AGENTS.md\nA2"), 0o600))

	content, err := LoadAgentsMD(root, nil)
	require.NoError(t, err)
	require.Contains(t, content, "root")
	require.Contains(t, content, "A")
	require.Contains(t, content, "A2")
	// Cycle should not duplicate "root" through recursive include.
	require.Equal(t, 1, strings.Count(content, "root"))
}

func TestAgentsMDLoaderDepthLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("@d1.md"), 0o600))
	for i := 1; i <= includedMDMaxDepth+2; i++ {
		name := filepath.Join(root, "d"+itoa(i)+".md")
		next := ""
		if i < includedMDMaxDepth+2 {
			next = "@d" + itoa(i+1) + ".md"
		}
		require.NoError(t, os.WriteFile(name, []byte(next), 0o600))
	}
	_, err := LoadAgentsMD(root, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "include depth exceeds")
}

func TestAgentsMDLoaderRejectsBinaryAndInvalidUTF8(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("@bin.md"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "bin.md"), []byte{'a', 0, 'b'}, 0o600))
	_, err := LoadAgentsMD(root, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "appears to be binary")

	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("@badutf8.md"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "badutf8.md"), []byte{0xff, 0xfe, 0xfd}, 0o600))
	_, err = LoadAgentsMD(root, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not valid UTF-8")
}

func TestAgentsMDLoaderTotalSizeLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "AGENTS.md")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

	loader := includedMDLoader{
		root:    root,
		visited: map[string]struct{}{},
		total:   includedMDMaxTotal,
		label:   "agents.md",
	}
	_, err := loader.load(path, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "total included content exceeds")
}

func TestReadFileLimitedReadErrorAndLateSizeCheck(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dirPath := filepath.Join(root, "dir")
	require.NoError(t, os.MkdirAll(dirPath, 0o755))
	_, err := readFileLimited(nil, dirPath, 10, "agents.md")
	require.Error(t, err)

	embed := &fakeEmbedFS{
		statSize: 1,
		data:     bytes.Repeat([]byte("a"), 2),
	}
	layer := NewFS(root, embed)
	nonexistentOSPath := filepath.Join(root, "embedded.md")
	_, err = readFileLimited(layer, nonexistentOSPath, 1, "agents.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

type fakeEmbedFS struct {
	statSize int64
	data     []byte
}

func (f *fakeEmbedFS) Open(name string) (fs.File, error) {
	if strings.TrimLeft(name, "/") == "" {
		return nil, fs.ErrNotExist
	}
	return &fakeEmbedFile{r: bytes.NewReader(f.data), data: f.data}, nil
}

func (f *fakeEmbedFS) Stat(name string) (fs.FileInfo, error) {
	if strings.TrimLeft(name, "/") == "" {
		return nil, fs.ErrNotExist
	}
	return fakeFileInfo{name: filepath.Base(name), size: f.statSize}, nil
}

type fakeEmbedFile struct {
	r    *bytes.Reader
	data []byte
}

func (f *fakeEmbedFile) Stat() (fs.FileInfo, error) {
	return fakeFileInfo{name: "x", size: int64(len(f.data))}, nil
}
func (f *fakeEmbedFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *fakeEmbedFile) Close() error               { return nil }

type fakeFileInfo struct {
	name string
	size int64
}

func (i fakeFileInfo) Name() string       { return i.name }
func (i fakeFileInfo) Size() int64        { return i.size }
func (i fakeFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (i fakeFileInfo) IsDir() bool        { return false }
func (i fakeFileInfo) Sys() any           { return nil }

func itoa(v int) string {
	var b [32]byte
	n := len(b)
	for {
		n--
		b[n] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	return string(b[n:])
}
