package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestDeptScratchpadRelPath_Agent(t *testing.T) {
	cases := map[string]string{
		"frontend":       ".orchestra/depts/frontend.md",
		"frontend@web":   ".orchestra/depts/frontend@web.md",
		"backend@api-v2": ".orchestra/depts/backend@api-v2.md",
		"":               "",
		"Frontend":       "", // uppercase
		"../evil":        "",
		"a/b":            "",
		"@web":           "",
	}
	for in, want := range cases {
		if got := DeptScratchpadRelPath(in); got != want {
			t.Errorf("DeptScratchpadRelPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpdateWorkingState_DeptWritesInstanceFile(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	a := &Agent{opts: Options{Mode: ModeOrchestra}, tools: tr}

	out, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"## Goal\nFE web work","dept":"frontend@web"}`))
	if err != nil {
		t.Fatalf("handleUpdateWorkingState: %v", err)
	}
	var resp struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Path != ".orchestra/depts/frontend@web.md" {
		t.Fatalf("path = %q", resp.Path)
	}
	b, err := os.ReadFile(filepath.Join(root, ".orchestra", "depts", "frontend@web.md"))
	if err != nil {
		t.Fatalf("dept file not written: %v", err)
	}
	if !strings.Contains(string(b), "FE web work") {
		t.Fatalf("unexpected content: %q", string(b))
	}
	// state.md must be untouched.
	if _, err := os.Stat(filepath.Join(root, ".orchestra", "state.md")); !os.IsNotExist(err) {
		t.Fatalf("state.md must not be created on dept write: %v", err)
	}

	// Invalid instance id is rejected.
	if _, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"x","dept":"../evil"}`)); err == nil {
		t.Fatal("invalid dept must be rejected")
	}

	// Without dept the write still goes to state.md.
	if _, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"## Goal\nglobal"}`)); err != nil {
		t.Fatalf("state.md write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".orchestra", "state.md")); err != nil {
		t.Fatalf("state.md missing after default write: %v", err)
	}
}

// The Lead writes state.md as prose and the phase guard reads it as a
// document with a YAML frontmatter. Twice in the 27B runs of 2026-09-18 the
// Lead's first update_working_state carried no frontmatter, the next spawn was
// refused with "missing YAML frontmatter", and the Lead burned steps rewriting
// the file by hand. The runtime has to keep the frontmatter it has and put the
// new body under it — and say so, with the shape of a phase declaration.
func TestUpdateWorkingState_BodyWithoutFrontmatterKeepsThePhase(t *testing.T) {
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	a := &Agent{opts: Options{Mode: ModeOrchestra}, tools: tr}

	// A fresh session: the body gets a frontmatter with the phase unset, the
	// file parses, and an explore child can still be spawned.
	out, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"## Goal\nbuild the board\n\n## Next\n- spawn workers"}`))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found {
		t.Fatalf("state.md must parse after a frontmatter-less write: found=%v err=%v", found, err)
	}
	if !strings.Contains(st.Body, "build the board") {
		t.Fatalf("the body was lost:\n%s", st.Body)
	}
	if !strings.Contains(string(out), "phase: execution") || !strings.Contains(string(out), "frontmatter") {
		t.Fatalf("the reply must show the Lead how to declare a phase, got: %s", out)
	}
	if err := orchestrastate.GuardSpawn(root, orchestrastate.EnforcementStrict, "explore"); err != nil {
		t.Fatalf("a read-only child must still spawn: %v", err)
	}

	// A declared phase survives a later body-only write.
	if _, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"---\norchestra:\n  phase: maintenance\n---\n## Goal\nhotfix"}`)); err != nil {
		t.Fatalf("phase write: %v", err)
	}
	if _, err := a.handleUpdateWorkingState(json.RawMessage(`{"content":"## Goal\nhotfix\n\n## Done\n- [x] step one"}`)); err != nil {
		t.Fatalf("body-only write: %v", err)
	}
	st, _, err = orchestrastate.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != orchestrastate.PhaseMaintenance {
		t.Fatalf("a body-only write dropped the phase: %q", st.Phase)
	}
	if !strings.Contains(st.Body, "step one") {
		t.Fatalf("the new body was not written:\n%s", st.Body)
	}
	if err := orchestrastate.GuardSpawn(root, orchestrastate.EnforcementStrict, "worker"); err != nil {
		t.Fatalf("maintenance must still admit a worker after the body-only write: %v", err)
	}
}
