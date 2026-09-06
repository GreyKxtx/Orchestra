package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent/working"
	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/tools"
)

func newLessonAgent(t *testing.T, mode Mode) (*Agent, string) {
	t.Helper()
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return &Agent{
		tools:   tr,
		opts:    Options{Mode: mode, SessionID: "s1"},
		working: working.New("wire the weather panel into the sidebar"),
	}, root
}

func readEngineeringLessons(t *testing.T, root string) string {
	t.Helper()
	return readLessonsFile(t, root, "engineering")
}

func readLessonsFile(t *testing.T, root, dept string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "lessons", dept+".md"))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestRecordTurnLesson_WritesAntiPatternInBuildMode(t *testing.T) {
	a, root := newLessonAgent(t, ModeBuild)
	a.working.ObserveTool("edit", json.RawMessage(`{"path":"src/App.jsx"}`), nil,
		errors.New("StaleContent: search block not found"))

	a.recordTurnLesson(nil)

	// Episodic learning only ever fired for worker children, so the single
	// agent mode almost everyone actually uses learned nothing from its own
	// mistakes — across a whole field run the lessons directory never existed.
	// .jsx infers the javascript_engineering dept (see dept-inference test below).
	got := readLessonsFile(t, root, "javascript_engineering")
	if got == "" {
		t.Fatal("a turn that ended in errors must leave a lesson")
	}
	if !strings.Contains(got, "StaleContent") {
		t.Errorf("lesson must carry the error, got: %s", got)
	}
	if !strings.Contains(got, "src/App.jsx") {
		t.Errorf("lesson must carry the file it happened in, got: %s", got)
	}
}

// §1.11 #3: single-agent mode used to hardcode dept="engineering" for every
// lesson regardless of what the turn actually touched, so a repo working in
// several languages piled every anti-pattern into one undifferentiated file.
func TestRecordTurnLesson_InfersDeptFromTouchedFiles(t *testing.T) {
	a, root := newLessonAgent(t, ModeBuild)
	a.working.ObserveTool("edit", json.RawMessage(`{"path":"internal/agent/agent.go"}`), nil,
		errors.New("StaleContent: search block not found"))

	a.recordTurnLesson(nil)

	got := readLessonsFile(t, root, "go_engineering")
	if got == "" {
		t.Fatal("a .go-file turn must land its lesson under go_engineering, not engineering")
	}
	if strings.Contains(readEngineeringLessons(t, root), "StaleContent") {
		t.Fatal("the lesson must not also land in the generic engineering dept")
	}
}

func TestRecordTurnLesson_QuietWhenNothingWentWrong(t *testing.T) {
	a, root := newLessonAgent(t, ModeBuild)
	a.working.ObserveTool("edit", json.RawMessage(`{"path":"src/App.jsx"}`), []byte(`{}`), nil)

	a.recordTurnLesson(nil)

	// Lessons are injected into later prompts, so a clean turn writing one
	// would cost tokens forever to say nothing happened.
	if got := readEngineeringLessons(t, root); got != "" {
		t.Fatalf("a clean turn must not write a lesson, got: %s", got)
	}
}

func TestRecordTurnLesson_LeavesWorkerChildrenToTheirOwnRecorder(t *testing.T) {
	a, root := newLessonAgent(t, ModeWorker)
	a.working.ObserveTool("edit", json.RawMessage(`{"path":"src/App.jsx"}`), nil,
		errors.New("StaleContent: search block not found"))

	a.recordTurnLesson(nil)

	// tasks.recordWorkerLesson already records worker outcomes under the
	// WorkOrder's own dept, with verify state this layer cannot see.
	if got := readEngineeringLessons(t, root); got != "" {
		t.Fatalf("worker children are recorded by the task runner, got: %s", got)
	}
}

// Discarding BumpAntiPatternSignal's return meant a single agent hitting the
// same error 3x in a row across turns never told anyone — the plan's own
// example ("3× StaleContent on src/App.jsx — add to ORCHESTRA.md?") is
// exactly this case.
func TestRecordTurnLesson_SuggestsARuleAfterThreeRepeatsOnTheSameFile(t *testing.T) {
	a, _ := newLessonAgent(t, ModeBuild)
	observeSameFailure := func() {
		a.working = working.New("wire the weather panel into the sidebar")
		a.working.ObserveTool("edit", json.RawMessage(`{"path":"src/App.jsx"}`), nil,
			errors.New("StaleContent: search block not found"))
	}

	observeSameFailure()
	res := &Result{}
	a.recordTurnLesson(res)
	if res.RuleSuggestion != nil {
		t.Fatalf("must not suggest before the threshold, got %+v", res.RuleSuggestion)
	}

	observeSameFailure()
	res = &Result{}
	a.recordTurnLesson(res)
	if res.RuleSuggestion != nil {
		t.Fatalf("must not suggest on the second repeat, got %+v", res.RuleSuggestion)
	}

	observeSameFailure()
	res = &Result{}
	a.recordTurnLesson(res)
	if res.RuleSuggestion == nil {
		t.Fatal("third repeat on the same file must suggest a rule")
	}
	if res.RuleSuggestion.File != "src/App.jsx" {
		t.Errorf("File = %q, want src/App.jsx", res.RuleSuggestion.File)
	}
	if res.RuleSuggestion.Count != lessons.RuleSuggestThreshold {
		t.Errorf("Count = %d, want %d", res.RuleSuggestion.Count, lessons.RuleSuggestThreshold)
	}
	if !strings.Contains(res.RuleSuggestion.RuleLine, "src/App.jsx") {
		t.Errorf("RuleLine must name the file: %q", res.RuleSuggestion.RuleLine)
	}
	if !strings.Contains(res.RuleSuggestion.Text, "src/App.jsx") {
		t.Errorf("Text must name the file: %q", res.RuleSuggestion.Text)
	}
}

// A different file hitting the same error must not combine into one count —
// each file earns the suggestion on its own repeat history.
func TestRecordTurnLesson_DifferentFilesDoNotShareTheCount(t *testing.T) {
	a, _ := newLessonAgent(t, ModeBuild)
	observe := func(path string) {
		a.working = working.New("wire the weather panel into the sidebar")
		a.working.ObserveTool("edit", json.RawMessage(`{"path":"`+path+`"}`), nil,
			errors.New("StaleContent: search block not found"))
	}

	for i := 0; i < lessons.RuleSuggestThreshold-1; i++ {
		observe("src/App.jsx")
		a.recordTurnLesson(&Result{})
	}
	observe("src/Other.jsx")
	res := &Result{}
	a.recordTurnLesson(res)
	if res.RuleSuggestion != nil {
		t.Fatalf("a different file must not inherit src/App.jsx's count, got %+v", res.RuleSuggestion)
	}
}

func seedLesson(t *testing.T, root string) {
	t.Helper()
	if err := lessons.Append(root, lessons.Entry{
		Dept:   "engineering",
		Kind:   lessons.KindAntiPattern,
		Task:   "edit the search panel",
		Files:  []string{"src/components/CitySearch.jsx"},
		Verify: "StaleContent: search block not found",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildSystemPrompt_ReplaysLessonsInBuildMode(t *testing.T) {
	a, root := newLessonAgent(t, ModeBuild)
	seedLesson(t, root)

	got := a.buildSystemPrompt()

	// Writing lessons nobody reads back is only half a loop: the point is that
	// the next session starts already knowing what went wrong in the last one.
	if !strings.Contains(got, "StaleContent") {
		t.Fatalf("build mode must see its own past mistakes, prompt: %s", got)
	}
}

// §1.11 #3: the per-step <dept_lessons> injection used to always read the
// generic "engineering" dept too, so once a turn had touched, say, a .go
// file, its own earlier-recorded go_engineering lessons were invisible to it.
func TestBuildSystemPrompt_ReplaysLessonsForTheInferredDept(t *testing.T) {
	a, root := newLessonAgent(t, ModeBuild)
	if err := lessons.Append(root, lessons.Entry{
		Dept:   "go_engineering",
		Kind:   lessons.KindAntiPattern,
		Task:   "wire the weather panel into the sidebar",
		Files:  []string{"internal/agent/agent.go"},
		Verify: "StaleContent: search block not found",
	}); err != nil {
		t.Fatal(err)
	}
	// A prior tool call in this same turn already touched a .go file, so the
	// dept lessons this step should replay are go_engineering's, not engineering's.
	a.working.ObserveTool("read", json.RawMessage(`{"path":"internal/agent/agent.go"}`), []byte(`{}`), nil)

	got := a.buildSystemPrompt()

	if !strings.Contains(got, "StaleContent") {
		t.Fatalf("build mode must see its own go_engineering lessons once it has touched a .go file: %s", got)
	}
}

func TestBuildSystemPrompt_LeavesWorkerLessonsToTheSpawner(t *testing.T) {
	a, root := newLessonAgent(t, ModeWorker)
	seedLesson(t, root)

	got := a.buildSystemPrompt()

	// tasks.loadDeptLessons already hands a worker its dept lessons with the
	// WorkOrder; injecting here too would send the same block twice.
	if strings.Contains(got, "<dept_lessons") {
		t.Fatalf("worker prompt must not carry a second lessons block: %s", got)
	}
}

func TestRecordTurnLesson_NeedsASession(t *testing.T) {
	a, root := newLessonAgent(t, ModeBuild)
	a.opts.SessionID = ""
	a.working.ObserveTool("edit", json.RawMessage(`{"path":"a.go"}`), nil, errors.New("boom"))

	a.recordTurnLesson(nil)

	// One-shot `apply` runs have no session and no continuity to learn into.
	if got := readEngineeringLessons(t, root); got != "" {
		t.Fatalf("sessionless run must not write lessons, got: %s", got)
	}
}
