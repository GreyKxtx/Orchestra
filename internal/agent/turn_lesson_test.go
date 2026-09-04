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
	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "lessons", "engineering.md"))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestRecordTurnLesson_WritesAntiPatternInBuildMode(t *testing.T) {
	a, root := newLessonAgent(t, ModeBuild)
	a.working.ObserveTool("edit", json.RawMessage(`{"path":"src/App.jsx"}`), nil,
		errors.New("StaleContent: search block not found"))

	a.recordTurnLesson()

	// Episodic learning only ever fired for worker children, so the single
	// agent mode almost everyone actually uses learned nothing from its own
	// mistakes — across a whole field run the lessons directory never existed.
	got := readEngineeringLessons(t, root)
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

func TestRecordTurnLesson_QuietWhenNothingWentWrong(t *testing.T) {
	a, root := newLessonAgent(t, ModeBuild)
	a.working.ObserveTool("edit", json.RawMessage(`{"path":"src/App.jsx"}`), []byte(`{}`), nil)

	a.recordTurnLesson()

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

	a.recordTurnLesson()

	// tasks.recordWorkerLesson already records worker outcomes under the
	// WorkOrder's own dept, with verify state this layer cannot see.
	if got := readEngineeringLessons(t, root); got != "" {
		t.Fatalf("worker children are recorded by the task runner, got: %s", got)
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

	a.recordTurnLesson()

	// One-shot `apply` runs have no session and no continuity to learn into.
	if got := readEngineeringLessons(t, root); got != "" {
		t.Fatalf("sessionless run must not write lessons, got: %s", got)
	}
}
