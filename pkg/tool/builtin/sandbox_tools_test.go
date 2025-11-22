package toolbuiltin

import (
	"testing"

	"github.com/cexll/agentsdk-go/pkg/security"
)

func TestCustomSandboxConstructors(t *testing.T) {
	root := cleanTempDir(t)
	sb := security.NewDisabledSandbox()

	bash := NewBashToolWithSandbox(root, sb)
	bash.AllowShellMetachars(true)
	if bash.sandbox != sb || bash.root != resolveRoot(root) {
		t.Fatalf("bash tool did not apply custom sandbox/root: %+v", bash)
	}

	read := NewReadToolWithSandbox(root, sb)
	if read.base == nil || read.base.sandbox != sb {
		t.Fatalf("read tool sandbox mismatch: %#v", read.base)
	}

	write := NewWriteToolWithSandbox(root, sb)
	if write.base == nil || write.base.sandbox != sb {
		t.Fatalf("write tool sandbox mismatch: %#v", write.base)
	}

	edit := NewEditToolWithSandbox(root, sb)
	if edit.base == nil || edit.base.sandbox != sb {
		t.Fatalf("edit tool sandbox mismatch: %#v", edit.base)
	}

	glob := NewGlobToolWithSandbox(root, sb)
	if glob.sandbox != sb || glob.root != resolveRoot(root) {
		t.Fatalf("glob tool did not apply custom sandbox/root: %+v", glob)
	}
}
