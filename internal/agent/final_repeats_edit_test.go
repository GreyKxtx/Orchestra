package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/protocol/schema"
)

// A turn has two ways to change a file, and nothing reconciles them.
//
// `edit` changes the file during the turn; `final.patches` are resolved
// against the result at the end of it (run_final.go). A model that edits the
// file and then restates the change in its final answer has its restatement
// resolved against a file that already carries the edit — and the search block
// it wrote is still there, because an insertion after an anchor leaves the
// anchor intact. So the addition lands a second time, the turn reports success,
// and the workspace no longer compiles.
//
// This is not a flaky model. The eval task add_func asks for one Multiply and
// produced two on every run:
//
//	go_build: .\math.go:13:6: Multiply redeclared in this block
//
// The strings below are the bytes the model actually sent, read off a logging
// proxy in front of the model server, not a plausible reconstruction. That
// distinction earned itself: a tidier hand-written version of this same turn
// did NOT duplicate, and would have been recorded as evidence that the turn
// handles restatement correctly. The difference is one trailing newline —
// the final patch's search block ends with the line break after the closing
// brace, the edit's does not — which is exactly the kind of detail a
// reconstruction smooths away.
//
// Neither patch carries a file_hash, so the staleness condition that exists
// for precisely this case never fires.

const mathBefore = "package testpkg\n\n" +
	"// Add adds two integers.\n" +
	"func Add(a, b int) int {\n" +
	"    return a + b\n" +
	"}\n"

// editSearch is the anchor the model chose for its edit: the Add block, with
// no trailing newline.
const editSearch = "// Add adds two integers.\n" +
	"func Add(a, b int) int {\n" +
	"    return a + b\n" +
	"}"

// editReplace is that block with Multiply appended — the change, made once.
const editReplace = editSearch + "\n\n" +
	"// Multiply multiplies two integers.\n" +
	"func Multiply(a, b int) int {\n" +
	"    return a * b\n" +
	"}"

// finalSearch is the same anchor as the model restated it one step later, this
// time including the newline that follows the brace.
const finalSearch = editSearch + "\n"

// mathAfterEdit is what the edit leaves behind — the version the model never
// sees, because the edit tool answers with a hash and not content.
var mathAfterEdit = strings.Replace(mathBefore, editSearch, editReplace, 1)

// runTurn plays a scripted turn against a real tools.Runner and returns the
// file it worked on, as it ended up on disk.
func runTurn(t *testing.T, steps []string) (string, *recordingLLM) {
	t.Helper()
	root := t.TempDir()
	mathPath := filepath.Join(root, "math.go")
	if err := os.WriteFile(mathPath, []byte(mathBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module testpkg\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	client := &recordingLLM{steps: steps}

	ag, err := New(client, v, tr, Options{MaxSteps: 6, Apply: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "Add a Multiply function to math.go that multiplies two integers."); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out, err := os.ReadFile(mathPath)
	if err != nil {
		t.Fatalf("read back math.go: %v", err)
	}

	return string(out), client
}

// readStep and editStep are the two calls every turn below starts with.
const readStep = `{"type":"tool_call","tool":{"name":"read","input":{"path":"math.go"}}}`

var editStep = `{"type":"tool_call","tool":{"name":"edit","input":{"path":"math.go",` +
	`"search":` + jsonString(editSearch) + `,` +
	`"replace":` + jsonString(editReplace) + `}}}`

// runTheAddFuncTurn replays the failing turn verbatim: read, edit, and a final
// that restates the edit. The model answers with a bare patch set rather than
// the {"type":"final"} envelope, which is also how it came off the wire.
func runTheAddFuncTurn(t *testing.T) (string, *recordingLLM) {
	t.Helper()
	got, client := runTurn(t, []string{
		readStep,
		editStep,
		`{"patches":[{"path":"math.go","type":"file.search_replace",` +
			`"search":` + jsonString(finalSearch) + `,` +
			`"replace":` + jsonString(editReplace) + `}]}`,
	})

	// Without this the test passes when the edit never landed: one Multiply
	// from the final patch alone looks exactly like one Multiply from a turn
	// that correctly declined to write it twice.
	if !strings.Contains(got, "func Multiply") {
		t.Fatalf("the scripted edit never landed, so this test proves nothing about "+
			"what the final patch added:\n%s", recordedToolMessages(client))
	}
	return got, client
}

// recordedToolMessages returns every distinct tool message the agent fed back.
func recordedToolMessages(c *recordingLLM) string {
	var found []string
	seen := map[string]bool{}
	for _, req := range c.seen {
		for _, m := range req {
			if m.Role == llm.RoleTool && !seen[m.Content] {
				seen[m.Content] = true
				found = append(found, m.Content)
			}
		}
	}
	return strings.Join(found, "\n")
}

// jsonString quotes a Go string for embedding in the scripted LLM output.
func jsonString(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\t", "\\t")
	return "\"" + r.Replace(s) + "\""
}

// The model asked for Multiply once. Whatever the turn decides to do with the
// restatement — apply it, drop it as already made, or refuse it as stale — the
// file must end up with one Multiply, because that is what was asked for and
// it is the only version of the file that compiles.
func TestFinal_RepeatingTheEditDoesNotApplyItTwice(t *testing.T) {
	got, _ := runTheAddFuncTurn(t)

	if n := strings.Count(got, "func Multiply"); n != 1 {
		t.Errorf("the turn wrote %d copies of Multiply, so the workspace no longer compiles:\n%s", n, got)
	}
	if !strings.Contains(got, "func Add") {
		t.Errorf("the original function was lost:\n%s", got)
	}
}

// Refusing is only half the job. A model that is told "no" and not told what
// to do instead spends the rest of the turn being told "no" — that is how the
// orchestra write guard burned 52 calls before it learned to name the way out.
func TestFinal_TheRestatedPatchIsToldWhyAndWhatToDo(t *testing.T) {
	_, client := runTheAddFuncTurn(t)

	var hint string
	for _, req := range client.seen {
		for _, m := range req {
			if m.Role == llm.RoleUser && strings.Contains(m.Content, "You already changed") {
				hint = m.Content
			}
		}
	}
	if hint == "" {
		t.Fatalf("the restated patch was handled without telling the model anything, "+
			"so the turn passes and the model learns nothing:\n%s", recordedToolMessages(client))
	}
	if !strings.Contains(hint, "math.go") {
		t.Errorf("the refusal must name the file it is about:\n%s", hint)
	}
	if !strings.Contains(hint, `{"patches":[]}`) {
		t.Errorf("the refusal must name the way to finish the turn:\n%s", hint)
	}
	if !strings.Contains(hint, "file_hash") {
		t.Errorf("the refusal must name the way to send a further change:\n%s", hint)
	}
}

// The guard must not cost the model a legitimate second change. A patch that
// carries the hash of the file as it is now was planned against content the
// model has actually seen, and applying it is correct.
func TestFinal_AFurtherChangeCarryingTheCurrentHashStillApplies(t *testing.T) {
	got, _ := runTurn(t, []string{
		readStep,
		editStep,
		`{"patches":[{"path":"math.go","type":"file.search_replace",` +
			`"search":` + jsonString("// Multiply multiplies two integers.") + `,` +
			`"replace":` + jsonString("// Multiply returns a*b.") + `,` +
			`"file_hash":` + jsonString(fsutil.ComputeSHA256([]byte(mathAfterEdit))) + `}]}`,
	})

	if !strings.Contains(got, "// Multiply returns a*b.") {
		t.Errorf("a patch pinned to the current content was not applied, so the guard "+
			"costs the model changes it is entitled to make:\n%s", got)
	}
	if n := strings.Count(got, "func Multiply"); n != 1 {
		t.Errorf("the turn wrote %d copies of Multiply:\n%s", n, got)
	}
}

// The other way to reach a final patch is to have tried a tool and failed —
// edit returns StaleContent, the model falls back to describing the change.
// Nothing landed, so there is nothing to duplicate and the patch must apply.
func TestFinal_APatchAfterAFailedEditStillApplies(t *testing.T) {
	got, _ := runTurn(t, []string{
		readStep,
		`{"type":"tool_call","tool":{"name":"edit","input":{"path":"math.go",` +
			`"search":` + jsonString("func Subtract(a, b int) int {") + `,` +
			`"replace":` + jsonString("func Subtract(a, b int) int { // gone") + `}}}`,
		`{"patches":[{"path":"math.go","type":"file.search_replace",` +
			`"search":` + jsonString(editSearch) + `,` +
			`"replace":` + jsonString(editReplace) + `}]}`,
	})

	if n := strings.Count(got, "func Multiply"); n != 1 {
		t.Errorf("after an edit that changed nothing, the final patch is the only way the "+
			"work gets done, and it produced %d copies:\n%s", n, got)
	}
}
