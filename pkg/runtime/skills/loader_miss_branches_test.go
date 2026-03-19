package skills

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
)

func TestLoadFromFSSortsSkillsByName(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, ".agents", "skills")

	writeSkill(t, filepath.Join(skillsRoot, "b-skill", "SKILL.md"), "b-skill", "body")
	writeSkill(t, filepath.Join(skillsRoot, "a-skill", "SKILL.md"), "a-skill", "body")

	regs, errs := LoadFromFS(LoaderOptions{ProjectRoot: root})
	if len(errs) != 0 {
		t.Fatalf("errs=%v, want none", errs)
	}
	if len(regs) != 2 {
		t.Fatalf("regs len=%d, want 2", len(regs))
	}
	if regs[0].Definition.Name != "a-skill" || regs[1].Definition.Name != "b-skill" {
		t.Fatalf("order=%q,%q, want a-skill,b-skill", regs[0].Definition.Name, regs[1].Definition.Name)
	}
}

func TestLoadSkillDirCollectsParseErrors(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, ".agents", "skills")
	mustWrite(t, filepath.Join(skillsRoot, "bad", "SKILL.md"), "no front matter\n")

	_, errs := loadSkillDir(skillsRoot, config.NewFS("", nil))
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "missing YAML frontmatter") {
		t.Fatalf("errs=%v, want frontmatter error", errs)
	}
}

type readErrorFile struct {
	readCalls int
}

func (f *readErrorFile) Stat() (fs.FileInfo, error) {
	return mockDirInfo{named: "SKILL.md", isDir: false}, nil
}
func (f *readErrorFile) Close() error { return nil }

func (f *readErrorFile) Read(p []byte) (int, error) {
	f.readCalls++
	switch f.readCalls {
	case 1:
		return copy(p, []byte("---\n")), nil
	case 2:
		n := copy(p, []byte("name: demo"))
		return n, errors.New("read boom")
	default:
		return 0, io.EOF
	}
}

type singleFileFS struct {
	path string
	file fs.File
}

func (f *singleFileFS) Open(name string) (fs.File, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.path) {
		return f.file, nil
	}
	return nil, fs.ErrNotExist
}

func (f *singleFileFS) Stat(name string) (fs.FileInfo, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.path) {
		return f.file.Stat()
	}
	return nil, fs.ErrNotExist
}

func TestReadFrontMatterPropagatesReadErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x", "SKILL.md")
	embed := &singleFileFS{
		path: "x/SKILL.md",
		file: &readErrorFile{},
	}
	fsLayer := config.NewFS(root, embed)

	_, err := readFrontMatter(path, fsLayer)
	if err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("err=%v, want read boom", err)
	}
}

func TestLoadSupportFilesWithFSCollectsStatErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permission model differs on windows")
	}

	dir := t.TempDir()
	origMode := os.FileMode(0o700)
	if info, err := os.Stat(dir); err == nil {
		origMode = info.Mode() & 0o777
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatalf("chmod 0: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, origMode); err != nil {
			t.Errorf("chmod cleanup: %v", err)
		}
	})

	_, errs := loadSupportFilesWithFS(dir, config.NewFS("", nil))
	if len(errs) == 0 {
		t.Fatalf("expected stat error")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "skills: stat") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("errs=%v, want stat error", errs)
	}
}

func TestLoadSupportFilesWithFSRelErrorFallsBackToEntryName(t *testing.T) {
	dir := "/does-not-exist-" + t.Name()
	root := filepath.Join(dir, "scripts")

	embedRoot := strings.TrimPrefix(filepath.ToSlash(root), "/")
	embedFile := filepath.ToSlash(filepath.Join(embedRoot, "tool.sh"))
	embed := fstest.MapFS{
		embedRoot: &fstest.MapFile{Mode: fs.ModeDir},
		embedFile: &fstest.MapFile{Data: []byte("echo hi\n")},
	}
	fsLayer := config.NewFS("", embed)

	out, errs := loadSupportFilesWithFS(dir, fsLayer)
	if len(errs) != 0 {
		t.Fatalf("errs=%v, want none", errs)
	}
	files := out["scripts"]
	if len(files) != 1 || files[0] != "tool.sh" {
		t.Fatalf("files=%v, want [tool.sh]", files)
	}
}

type failingReadDirFile struct{}

func (failingReadDirFile) Stat() (fs.FileInfo, error) {
	return mockDirInfo{named: "scripts", isDir: true}, nil
}
func (failingReadDirFile) Read([]byte) (int, error) { return 0, io.EOF }
func (failingReadDirFile) Close() error             { return nil }
func (failingReadDirFile) ReadDir(int) ([]fs.DirEntry, error) {
	return nil, errors.New("readdir boom")
}

type failingWalkFS struct {
	root string
}

func (f failingWalkFS) Open(name string) (fs.File, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.root) {
		return failingReadDirFile{}, nil
	}
	return nil, fs.ErrNotExist
}

func (f failingWalkFS) Stat(name string) (fs.FileInfo, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.root) {
		return mockDirInfo{named: "scripts", isDir: true}, nil
	}
	return nil, fs.ErrNotExist
}

func TestLoadSupportFilesWithFSCollectsWalkErrors(t *testing.T) {
	dir := "/does-not-exist-" + t.Name()
	root := filepath.Join(dir, "scripts")

	embedRoot := strings.TrimPrefix(filepath.ToSlash(root), "/")
	fsLayer := config.NewFS("", failingWalkFS{root: embedRoot})

	_, errs := loadSupportFilesWithFS(dir, fsLayer)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "walk") || !strings.Contains(errs[0].Error(), "readdir boom") {
		t.Fatalf("errs=%v, want walk readdir boom", errs)
	}
}

func TestBuildDefinitionMetadataAddsLicenseAndCompatibilityWhenMapNil(t *testing.T) {
	meta := buildDefinitionMetadata(SkillFile{
		Path: "/tmp/SKILL.md",
		Metadata: SkillMetadata{
			Name:          "demo",
			Description:   "d",
			License:       "MIT",
			Compatibility: "test",
		},
	})
	if meta["license"] != "MIT" || meta["compatibility"] != "test" {
		t.Fatalf("meta=%v, want license+compatibility", meta)
	}
}
