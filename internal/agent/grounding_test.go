package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
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

	got := unknownWorkspacePaths(answer, root, "")
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
		"a URL":                   "See https://example.com/docs/getting-started for details.",
		"a Go import path":        "It imports github.com/orchestra/orchestra/internal/agent for the loop.",
		"an absolute path":        "Logs go to /var/log/orchestra.log on Linux.",
		"a Windows path":          `The config lives at C:\Users\me\.orchestra.yml`,
		"a bare command":          "Run orchestra apply --apply to write the changes.",
		"a file that exists":      "The entry point is internal/agent/agent.go.",
		"a file under a real dir": "Artifacts land in .orchestra/plan.json after a run.",
		"fenced example code":     "```\nmkdir pkg/newthing\n```",
		// Seen live: a correct description of a web page was sent back once
		// for naming "combobox/dropdown" as a path.
		"two words joined by a slash": "3. **Plan** - a combobox/dropdown with three options: Free, Pro, Team",
		"and/or":                      "Fill the name and/or the email.",
		// Seen live on the 27B: an answer that named a real file beside it got
		// "sample/eval" sent back as an invented path.
		"word/word beside a real path": "internal/agent/agent.go runs the loop; the sample/eval split is 80/20.",
	}
	for name, answer := range cases {
		if got := unknownWorkspacePaths(answer, root, ""); len(got) != 0 {
			t.Errorf("%s must not be flagged, got %v", name, got)
		}
	}
}

// Two words joined by a slash read like "combobox/dropdown" in prose, so a short
// path is taken as a claim only when something else marks it as one: code
// formatting, a file extension, a third segment.
func TestUnknownWorkspacePaths_CatchesAShortPathMarkedAsAPath(t *testing.T) {
	root := groundingFixture(t)
	for name, answer := range map[string]string{
		"in backticks":      "MCP support lives in `pkg/mcp`.",
		"with an extension": "See nosuchdir/thing.md.",
		"three segments":    "MCP support lives in pkg/mcp/client.",
	} {
		if got := unknownWorkspacePaths(answer, root, ""); len(got) == 0 {
			t.Errorf("%s: an invented path must be caught", name)
		}
	}
}

// A path whose parent directory exists is a plausible not-yet-created file, not
// an invented tree — the check only fires when the directory itself is unknown.
func TestUnknownWorkspacePaths_OnlyFlagsAnUnknownDirectory(t *testing.T) {
	root := groundingFixture(t)
	if got := unknownWorkspacePaths("Look at docs/MISSING.md for that.", root, ""); len(got) != 0 {
		t.Fatalf("a missing file in a real directory must not be flagged: %v", got)
	}
	if got := unknownWorkspacePaths("Look at nosuchdir/thing.md for that.", root, ""); len(got) == 0 {
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

// Seen live on a plan: "ставим emsdk (~1 ГБ + VS Build Tools/MinGW)" in an
// answer that also named a real path. "Tools/MinGW" is a choice between two
// products, not a directory; the correction sent the model back to ask the
// user its question again.
func TestUnknownWorkspacePaths_CapitalisedAlternativesAreNotPaths(t *testing.T) {
	root := groundingFixture(t)
	answer := "See docs/ARCHITECTURE.md. Install emsdk (~1 GB + VS Build Tools/MinGW), render with Canvas2D/WebGL."
	if got := unknownWorkspacePaths(answer, root, ""); len(got) != 0 {
		t.Fatalf("product alternatives must not be flagged, got %v", got)
	}
	if got := unknownWorkspacePaths("See docs/ARCHITECTURE.md and `Assets/Scripts`.", root, ""); len(got) == 0 {
		t.Fatal("a capitalised path in code formatting is still a claim")
	}
}

// The same answer named phys/blackhole.cpp — a file the plan written earlier in
// the conversation proposes. A path the conversation already holds was not
// invented by this answer.
func TestUnknownWorkspacePaths_APathTheConversationNamedIsNotInvented(t *testing.T) {
	root := groundingFixture(t)
	answer := "The plan puts the physics in phys/blackhole.cpp."
	if got := unknownWorkspacePaths(answer, root, ""); len(got) == 0 {
		t.Fatal("precondition: with no conversation the path is unknown")
	}
	known := "## Steps\n4. phys/blackhole.cpp — RK2 integrator"
	if got := unknownWorkspacePaths(answer, root, known); len(got) != 0 {
		t.Fatalf("a path from the conversation must not be flagged, got %v", got)
	}
}

func TestConversationMentions_ReadsMessagesAndToolArguments(t *testing.T) {
	got := conversationMentions([]llm.Message{
		{Role: llm.RoleUser, Content: "build it"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunc{
			Name: "write", Arguments: llm.ToolArguments(`{"path":".orchestra/plans/p.md","content":"phys/blackhole.cpp"}`),
		}}}},
	})
	if !strings.Contains(got, "build it") || !strings.Contains(got, "phys/blackhole.cpp") {
		t.Fatalf("conversationMentions = %q", got)
	}
}

// A list whose items start with real paths is a list of paths: an invented
// one among them is caught even with nothing else marking it.
func TestUnknownWorkspacePaths_AnInventedItemInAListOfPaths(t *testing.T) {
	root := groundingFixture(t)
	answer := "Структура:\n- internal/core — RPC\n- docs/ARCHITECTURE.md — архитектура\n- web/static — фронтенд"
	got := unknownWorkspacePaths(answer, root, "")
	if len(got) != 1 || got[0] != "web/static" {
		t.Fatalf("got %v, want [web/static]", got)
	}
	// Two words inside a list item's prose are still two words.
	prose := "- internal/core — RPC, the sample/eval split lives here"
	if got := unknownWorkspacePaths(prose, root, ""); len(got) != 0 {
		t.Fatalf("got %v for prose inside a list item", got)
	}
}

// Seen live: an answer about a page said "центр `cx/cy`" — two variables the
// model had just read — and was sent back as naming an invented path. A pair
// whose halves are both names the conversation already holds is a pair of
// names.
func TestUnknownWorkspacePaths_APairOfNamesFromTheCodeIsNotAPath(t *testing.T) {
	root := groundingFixture(t)
	known := `{"path":"index.html","content":"let cx = W / 2, cy = H / 2;\nfunction moveBH(e) { cx = e.x; cy = e.y; }"}`
	answer := "Состояние: размеры `W/H`, центр `cx/cy`, см. internal/core."
	if got := unknownWorkspacePaths(answer, root, known); len(got) != 0 {
		t.Fatalf("got %v, want nothing: cx and cy are variables the model read", got)
	}
	// An invented directory whose halves the conversation never named is
	// still caught.
	if got := unknownWorkspacePaths("MCP support lives in `pkg/mcp`.", root, known); len(got) == 0 {
		t.Fatal("an invented path must still be caught")
	}
}
