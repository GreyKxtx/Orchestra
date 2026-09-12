package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

func groundingFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"internal/agent", "internal/core", "docs", "cmd/orchestra", ".orchestra"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	for _, f := range []string{"README.md", "docs/ARCHITECTURE.md", "internal/agent/agent.go"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(f)), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return root
}

// The failure this exists for: asked what the project is, a 9B model answered
// from one file and invented a whole directory tree — "pkg/ — публичные API",
// "pkg/mcp", "pkg/skills" — in a repository that has no pkg/ at all.
func TestUnknownWorkspacePaths_CatchesAnInventedTree(t *testing.T) {
	root := groundingFixture(t)
	answer := `Структура проекта:
- internal/agent — ядро
- internal/core — RPC
- pkg/ — публичные API и утилиты
- pkg/mcp — поддержка MCP
- docs/ARCHITECTURE.md — архитектура`

	got := unknownWorkspacePaths(answer, root)
	if len(got) == 0 {
		t.Fatal("an invented directory must be caught")
	}
	found := map[string]bool{}
	for _, p := range got {
		found[p] = true
	}
	if !found["pkg"] && !found["pkg/mcp"] {
		t.Fatalf("the invented tree must be named: %v", got)
	}
	for _, real := range []string{"internal/agent", "internal/core", "docs/ARCHITECTURE.md"} {
		if found[real] {
			t.Fatalf("%q exists and must not be flagged: %v", real, got)
		}
	}
}

// False positives are worse than the disease: a wrong complaint makes the model
// rewrite a correct answer. Everything here must pass through untouched.
func TestUnknownWorkspacePaths_LeavesInnocentTextAlone(t *testing.T) {
	root := groundingFixture(t)
	cases := map[string]string{
		"a URL":                  "See https://example.com/docs/getting-started for details.",
		"a Go import path":       "It imports github.com/orchestra/orchestra/internal/agent for the loop.",
		"an absolute path":       "Logs go to /var/log/orchestra.log on Linux.",
		"a Windows path":         `The config lives at C:\Users\me\.orchestra.yml`,
		"a bare command":         "Run orchestra apply --apply to write the changes.",
		"a file that exists":     "The entry point is internal/agent/agent.go.",
		"a file under a real dir": "Artifacts land in .orchestra/plan.json after a run.",
		"fenced example code":    "```\nmkdir pkg/newthing\n```",
	}
	for name, answer := range cases {
		if got := unknownWorkspacePaths(answer, root); len(got) != 0 {
			t.Errorf("%s must not be flagged, got %v", name, got)
		}
	}
}

// A path whose parent directory exists is a plausible not-yet-created file, not
// an invented tree — the check only fires when the directory itself is unknown.
func TestUnknownWorkspacePaths_OnlyFlagsAnUnknownDirectory(t *testing.T) {
	root := groundingFixture(t)
	if got := unknownWorkspacePaths("Look at docs/MISSING.md for that.", root); len(got) != 0 {
		t.Fatalf("a missing file in a real directory must not be flagged: %v", got)
	}
	if got := unknownWorkspacePaths("Look at nosuchdir/thing.md for that.", root); len(got) == 0 {
		t.Fatal("a file under an unknown directory must be flagged")
	}
}

func TestGroundingHint_NamesThePathsAndWhatToDo(t *testing.T) {
	hint := groundingHint([]string{"pkg", "pkg/mcp"})
	for _, want := range []string{"pkg", "pkg/mcp", "repo_map"} {
		if !contains(hint, want) {
			t.Fatalf("hint must mention %q: %q", want, hint)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// End to end: the answer that invents a tree goes back once, and the corrected
// one is accepted. Exactly the turn the user hit — "what is this project",
// answered with a pkg/ directory that does not exist.
func TestRun_AnAnswerThatInventsAPathIsSentBackOnce(t *testing.T) {
	root := groundingFixture(t)
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	script := &scriptedLLM{steps: []string{
		"The project is laid out as internal/agent and pkg/mcp for MCP support.",
		"The project is laid out as internal/agent and internal/core.",
	}}
	ag, err := New(script, v, tr, Options{MaxSteps: 6, ModelContextTokens: 16384})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hist, _, runErr := ag.Run(context.Background(), nil, "расскажи о проекте")
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if script.i < 2 {
		t.Fatalf("the invented path must cost one correction, steps used: %d", script.i)
	}
	var corrected bool
	for _, m := range hist {
		if strings.Contains(m.Content, "pkg/mcp") && strings.Contains(m.Content, "do not exist") {
			corrected = true
		}
	}
	if !corrected {
		t.Fatal("the correction must name the invented path back to the model")
	}
}

// And only once: a model that keeps inventing must not be asked forever.
func TestRun_TheGroundingCorrectionHappensOnlyOnce(t *testing.T) {
	root := groundingFixture(t)
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	const invented = "It lives in pkg/mcp and pkg/skills."
	script := &scriptedLLM{steps: []string{invented, invented, invented, invented}}
	ag, err := New(script, v, tr, Options{MaxSteps: 6, ModelContextTokens: 16384})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "расскажи о проекте"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if script.i > 2 {
		t.Fatalf("a stubborn model must be corrected once, not repeatedly: %d steps", script.i)
	}
}
