package toolbuiltin

import (
	"strings"
	"testing"

	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
)

func TestBuildSkillDescriptionEscapesAndDefaults(t *testing.T) {
	empty := buildSkillDescription(nil)
	if !strings.Contains(empty, "</available_skills>") {
		t.Fatalf("expected closing tag for empty skills, got %q", empty)
	}

	defs := []skills.Definition{
		{Name: "", Description: "", Metadata: map[string]string{}},
		{Name: "xml&skill", Description: "use <xml>", Metadata: map[string]string{"location": "path/to.xml"}},
	}
	desc := buildSkillDescription(defs)
	if !strings.Contains(desc, "unknown") {
		t.Fatalf("missing default name fallback: %q", desc)
	}
	if !strings.Contains(desc, "&lt;xml&gt;") || !strings.Contains(desc, "&amp;") {
		t.Fatalf("expected XML escaping, got %q", desc)
	}
	if !strings.Contains(desc, "path/to.xml") {
		t.Fatalf("expected location metadata to surface, got %q", desc)
	}
}
