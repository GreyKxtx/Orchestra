package examples

import (
	"strings"
	"testing"
)

// The Docs Lead's stage-1 prompt narrows conventions.md from these L0
// defaults; a missing or empty entry here means a whole language's worth of
// engineering conventions silently vanishes from that step.
func TestL0Playbooks_IncludesEngineeringDefaults(t *testing.T) {
	got := L0Playbooks()
	for _, name := range []string{"go_engineering.md", "typescript_engineering.md", "python_engineering.md"} {
		content, ok := got[name]
		if !ok {
			t.Errorf("L0Playbooks() missing %q", name)
			continue
		}
		if strings.TrimSpace(content) == "" {
			t.Errorf("%s embedded empty", name)
		}
	}
}

// Every shipped L0 file must round-trip with real content — a build that
// embeds an empty template is worse than one that fails to compile, since
// nothing signals the loss downstream.
func TestL0Playbooks_NoFileIsEmpty(t *testing.T) {
	got := L0Playbooks()
	if len(got) == 0 {
		t.Fatal("no L0 playbooks embedded")
	}
	for name, content := range got {
		if strings.TrimSpace(content) == "" {
			t.Errorf("%s embedded empty", name)
		}
	}
}
