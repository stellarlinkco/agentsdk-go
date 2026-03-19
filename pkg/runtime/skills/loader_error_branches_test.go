package skills

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	"gopkg.in/yaml.v3"
)

type embedFS struct {
	open     func(string) (fs.File, error)
	stat     func(string) (fs.FileInfo, error)
	readDir  func(string) ([]fs.DirEntry, error)
	readFile func(string) ([]byte, error)
}

func (e *embedFS) Open(name string) (fs.File, error) {
	if e.open != nil {
		return e.open(name)
	}
	return nil, fs.ErrNotExist
}

func (e *embedFS) Stat(name string) (fs.FileInfo, error) {
	if e.stat != nil {
		return e.stat(name)
	}
	return nil, fs.ErrNotExist
}

func (e *embedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if e.readDir != nil {
		return e.readDir(name)
	}
	return nil, fs.ErrNotExist
}

func (e *embedFS) ReadFile(name string) ([]byte, error) {
	if e.readFile != nil {
		return e.readFile(name)
	}
	return nil, fs.ErrNotExist
}

type staticDirEntry struct {
	name  string
	isDir bool
}

func (e staticDirEntry) Name() string { return e.name }
func (e staticDirEntry) IsDir() bool  { return e.isDir }
func (e staticDirEntry) Type() fs.FileMode {
	if e.isDir {
		return fs.ModeDir
	}
	return 0
}
func (e staticDirEntry) Info() (fs.FileInfo, error) {
	return mockDirInfo{named: e.name, isDir: e.isDir}, nil
}

type mockDirInfo struct {
	named string
	isDir bool
}

func (m mockDirInfo) Name() string { return m.named }
func (m mockDirInfo) Size() int64  { return 0 }
func (m mockDirInfo) Mode() fs.FileMode {
	if m.isDir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (m mockDirInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (m mockDirInfo) IsDir() bool        { return m.isDir }
func (m mockDirInfo) Sys() any           { return nil }

type errReadFile struct {
	err error
}

func (f *errReadFile) Read([]byte) (int, error) { return 0, f.err }
func (f *errReadFile) Close() error             { return nil }
func (f *errReadFile) Stat() (fs.FileInfo, error) {
	return mockDirInfo{named: "SKILL.md", isDir: false}, nil
}

func TestLoadFromFSNoSkillsDir(t *testing.T) {
	root := t.TempDir()
	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if regs != nil {
		t.Fatalf("expected nil regs, got %v", regs)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no errs, got %v", errs)
	}
}

func TestLoadFromFSRegistersAndExecutesViaRegistry(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, ".agents", "skills", "demo", "SKILL.md")
	writeSkill(t, skillPath, "demo", "hello")

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if len(errs) != 0 || len(regs) != 1 {
		t.Fatalf("unexpected LoadFromFS results regs=%v errs=%v", regs, errs)
	}

	r := NewRegistry()
	if err := r.Register(regs[0].Definition, regs[0].Handler); err != nil {
		t.Fatalf("register: %v", err)
	}
	res, err := r.Execute(context.Background(), "demo", ActivationContext{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok || out["body"] != "hello" {
		t.Fatalf("unexpected output: %v", res.Output)
	}
}

func TestLoadFromFSDuplicateSkillFiles(t *testing.T) {
	prev := loadSkillDirFn
	t.Cleanup(func() { loadSkillDirFn = prev })
	loadSkillDirFn = func(string, *config.FS) ([]SkillFile, []error) {
		return []SkillFile{
			{Path: "/a/SKILL.md", Metadata: SkillMetadata{Name: "dup", Description: "d"}},
			{Path: "/b/SKILL.md", Metadata: SkillMetadata{Name: "dup", Description: "d"}},
		}, nil
	}

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: t.TempDir()})
	if len(regs) != 1 {
		t.Fatalf("expected 1 reg, got %d", len(regs))
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "duplicate skill") {
		t.Fatalf("expected duplicate error, got %v", errs)
	}
}

func TestLoaderRegistryInvalidDefinitionFromSkillFiles(t *testing.T) {
	prev := loadSkillDirFn
	t.Cleanup(func() { loadSkillDirFn = prev })
	loadSkillDirFn = func(string, *config.FS) ([]SkillFile, []error) {
		return []SkillFile{
			{Path: "/x/SKILL.md", Metadata: SkillMetadata{Name: "Bad!", Description: "d"}},
		}, nil
	}

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: t.TempDir()})
	if len(errs) != 0 || len(regs) != 1 {
		t.Fatalf("unexpected LoadFromFS results regs=%v errs=%v", regs, errs)
	}

	r := NewRegistry()
	err := r.Register(regs[0].Definition, regs[0].Handler)
	if err == nil || !strings.Contains(err.Error(), "invalid name") {
		t.Fatalf("expected invalid definition error, got %v", err)
	}
}

func TestLoadSkillDirStatError(t *testing.T) {
	_, errs := loadSkillDir(string([]byte{0}), nil)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "skills: stat") {
		t.Fatalf("expected stat error, got %v", errs)
	}
}

func TestLoadSkillDirReadDirError(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, ".agents", "skills")
	embed := &embedFS{
		stat: func(name string) (fs.FileInfo, error) {
			if name == ".agents/skills" {
				return mockDirInfo{named: "skills", isDir: true}, nil
			}
			return nil, fs.ErrNotExist
		},
		readDir: func(name string) ([]fs.DirEntry, error) {
			if name == ".agents/skills" {
				return nil, errors.New("nope")
			}
			return nil, fs.ErrNotExist
		},
	}

	_, errs := loadSkillDir(projectDir, config.NewFS(root, embed))
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "skills: read dir") {
		t.Fatalf("expected read dir error, got %v", errs)
	}
}

func TestLoadSkillDirSkipsMissingSkillMD(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, ".agents", "skills")
	embed := &embedFS{
		stat: func(name string) (fs.FileInfo, error) {
			if name == ".agents/skills" {
				return mockDirInfo{named: "skills", isDir: true}, nil
			}
			if name == ".agents/skills/demo" {
				return mockDirInfo{named: "demo", isDir: true}, nil
			}
			return nil, fs.ErrNotExist
		},
		readDir: func(name string) ([]fs.DirEntry, error) {
			if name == ".agents/skills" {
				return []fs.DirEntry{staticDirEntry{name: "demo", isDir: true}}, nil
			}
			return nil, fs.ErrNotExist
		},
		open: func(name string) (fs.File, error) {
			if name == ".agents/skills/demo/SKILL.md" {
				return nil, fs.ErrNotExist
			}
			return nil, fs.ErrNotExist
		},
	}

	files, errs := loadSkillDir(projectDir, config.NewFS(root, embed))
	if len(errs) != 0 {
		t.Fatalf("expected no errs, got %v", errs)
	}
	if files != nil {
		t.Fatalf("expected nil files, got %v", files)
	}
}

func TestReadFrontMatterInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	mustWrite(t, path, "---\nname: [\ndescription: d\n---\nbody\n")

	if _, err := readFrontMatter(path, nil); err == nil || !strings.Contains(err.Error(), "decode YAML") {
		t.Fatalf("expected YAML decode error, got %v", err)
	}
}

func TestValidateMetadataCompatibilityTooLong(t *testing.T) {
	meta := SkillMetadata{Name: "ok", Description: "d", Compatibility: strings.Repeat("x", 501)}
	if err := validateMetadata(meta); err == nil || !strings.Contains(err.Error(), "compatibility") {
		t.Fatalf("expected compatibility error, got %v", err)
	}
}

func TestToolListUnmarshalYAMLErrors(t *testing.T) {
	tl := ToolList{"x"}

	// invalid kind
	n := &yaml.Node{Kind: yaml.MappingNode}
	if err := tl.UnmarshalYAML(n); err == nil {
		t.Fatalf("expected kind error")
	}

	// sequence with non-scalar
	n = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		{Kind: yaml.MappingNode},
	}}
	if err := tl.UnmarshalYAML(n); err == nil || !strings.Contains(err.Error(), "expected string") {
		t.Fatalf("expected string error, got %v", err)
	}
}

func TestToolListUnmarshalYAMLNullAndNilNode(t *testing.T) {
	var tl ToolList
	if err := tl.UnmarshalYAML(nil); err != nil || tl != nil {
		t.Fatalf("expected nil list for nil node, got tl=%v err=%v", tl, err)
	}
	if err := tl.UnmarshalYAML(&yaml.Node{Tag: "!!null"}); err != nil || tl != nil {
		t.Fatalf("expected nil list for null tag, got tl=%v err=%v", tl, err)
	}
}

func TestBuildDefinitionMetadataCreatesMap(t *testing.T) {
	meta := buildDefinitionMetadata(SkillFile{
		Path: "/tmp/skill",
		Metadata: SkillMetadata{
			AllowedTools:  ToolList{"A", "B"},
			License:       "MIT",
			Compatibility: "v1",
		},
	})
	if meta["allowed-tools"] != "A,B" || meta["license"] != "MIT" || meta["compatibility"] != "v1" || meta["source"] != "/tmp/skill" {
		t.Fatalf("unexpected metadata: %v", meta)
	}
}

func TestLoadSkillContentAllowedToolsAndSupportFiles(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "SKILL.md")
	writeSkill(t, skillPath, "demo", "body")
	mustWrite(t, filepath.Join(dir, "scripts", "a.sh"), "echo hi")
	mustWrite(t, filepath.Join(dir, "references", "r.md"), "ref")

	res, err := loadSkillContent(SkillFile{
		Path: skillPath,
		Metadata: SkillMetadata{
			Name:         "demo",
			AllowedTools: ToolList{"ToolA"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok || out["support_files"] == nil {
		t.Fatalf("expected support_files in output, got %v", res.Output)
	}
	if res.Metadata == nil || res.Metadata["support-file-count"] != 2 {
		t.Fatalf("expected support-file-count=2, got %v", res.Metadata)
	}
	if tools, ok := res.Metadata["allowed-tools"].([]string); !ok || len(tools) != 1 || tools[0] != "ToolA" {
		t.Fatalf("expected allowed-tools metadata, got %v", res.Metadata)
	}
}

func TestLoadSupportFilesWithFSWalkError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skill")

	embed := &embedFS{
		stat: func(name string) (fs.FileInfo, error) {
			switch name {
			case "skill/scripts":
				return mockDirInfo{named: "scripts", isDir: true}, nil
			default:
				return nil, fs.ErrNotExist
			}
		},
		readDir: func(name string) ([]fs.DirEntry, error) {
			if name == "skill/scripts" {
				return nil, errors.New("walk-fail")
			}
			return nil, fs.ErrNotExist
		},
	}

	support, errs := loadSupportFilesWithFS(dir, config.NewFS(root, embed))
	if support != nil {
		t.Fatalf("expected nil support, got %v", support)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "skills: walk") {
		t.Fatalf("expected walk error, got %v", errs)
	}
}

func TestLazyHandlerReturnsCachedErrorWithoutReload(t *testing.T) {
	var readCalls int
	now := time.Unix(1, 0)
	embed := &embedFS{
		readFile: func(string) ([]byte, error) {
			readCalls++
			return nil, errors.New("read-denied")
		},
	}

	file := SkillFile{
		Path:     filepath.Join(t.TempDir(), "missing", "SKILL.md"),
		Metadata: SkillMetadata{Name: "x", Description: "d"},
		fs:       config.NewFS("", embed),
	}

	h := buildHandler(file, fileOps{
		readFile: file.fs.ReadFile,
		openFile: file.fs.Open,
		statFile: func(string) (fs.FileInfo, error) { return &mockFileInfo{modTime: now}, nil },
	})

	_, err := h.Execute(context.Background(), ActivationContext{})
	if err == nil {
		t.Fatalf("expected first execute to fail")
	}
	_, err = h.Execute(context.Background(), ActivationContext{})
	if err == nil {
		t.Fatalf("expected second execute to fail")
	}
	if readCalls != 1 {
		t.Fatalf("expected cached error path (1 read), got %d reads", readCalls)
	}
}

func TestReadFrontMatterReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "skill", "SKILL.md")

	embed := &embedFS{
		open: func(name string) (fs.File, error) {
			if name == "skill/SKILL.md" {
				return &errReadFile{err: errors.New("read fail")}, nil
			}
			return nil, fs.ErrNotExist
		},
	}

	if _, err := readFrontMatter(path, config.NewFS(root, embed)); err == nil || !strings.Contains(err.Error(), "read fail") {
		t.Fatalf("expected read fail error, got %v", err)
	}
}

func TestLoadSkillDirSkipsNonDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsRoot, "README.md"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	writeSkill(t, filepath.Join(skillsRoot, "demo", "SKILL.md"), "demo", "body")

	files, errs := loadSkillDir(skillsRoot, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errs: %v", errs)
	}
	if len(files) != 1 || files[0].Metadata.Name != "demo" {
		t.Fatalf("expected 1 demo file, got %v", files)
	}
}
