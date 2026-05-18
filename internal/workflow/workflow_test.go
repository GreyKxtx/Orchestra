package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func writeWF(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAndTopoSort(t *testing.T) {
	dir := t.TempDir()
	p := writeWF(t, dir, "w.yaml", `name: w
stages:
  - id: c
    skill: x
    depends_on: [a, b]
  - id: a
    skill: x
  - id: b
    skill: x
    depends_on: [a]
`)
	w, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	order, err := TopoSort(w)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("topo order wrong: %v", order)
	}
}

func TestValidate_CycleRejected(t *testing.T) {
	dir := t.TempDir()
	p := writeWF(t, dir, "w.yaml", `name: w
stages:
  - id: a
    skill: x
    depends_on: [b]
  - id: b
    skill: x
    depends_on: [a]
`)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestValidate_UnknownDependency(t *testing.T) {
	dir := t.TempDir()
	p := writeWF(t, dir, "w.yaml", `name: w
stages:
  - id: a
    skill: x
    depends_on: [missing]
`)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "unknown stage") {
		t.Fatalf("expected unknown stage err, got %v", err)
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	dir := t.TempDir()
	p := writeWF(t, dir, "w.yaml", `name: w
stages:
  - id: a
    skill: x
  - id: a
    skill: y
`)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate err, got %v", err)
	}
}

func TestValidate_BadOnMarkerRedo(t *testing.T) {
	dir := t.TempDir()
	p := writeWF(t, dir, "w.yaml", `name: w
stages:
  - id: a
    skill: x
    on_marker:
      "## X": "redo:nowhere"
`)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "unknown stage") {
		t.Fatalf("expected unknown redo target err, got %v", err)
	}
}

// fakeInvoker scripts per-skill outputs and counts invocations.
// Safe under concurrent access from the Parallel runner.
type fakeInvoker struct {
	mu      sync.Mutex
	outputs map[string][]string
	calls   map[string]int
}

func (f *fakeInvoker) Invoke(_ context.Context, skill, query string) (string, error) {
	f.mu.Lock()
	f.calls[skill]++
	seq := f.outputs[skill]
	idx := f.calls[skill] - 1
	f.mu.Unlock()
	if idx >= len(seq) {
		return seq[len(seq)-1], nil
	}
	return strings.ReplaceAll(seq[idx], "{Q}", query), nil
}

func TestRun_LinearChain_ForwardsOutputs(t *testing.T) {
	w := &Workflow{Name: "w", Stages: []Stage{
		{ID: "a", Skill: "sa", Inputs: []string{"$ARGUMENTS"}},
		{ID: "b", Skill: "sb", DependsOn: []string{"a"}, Inputs: []string{"task=$ARGUMENTS prev={a.output}"}},
	}}
	if err := Validate(w); err != nil {
		t.Fatal(err)
	}

	inv := &fakeInvoker{
		outputs: map[string][]string{
			"sa": {"<findings>{Q}</findings>"},
			"sb": {"saw: {Q}"},
		},
		calls: map[string]int{},
	}
	res, err := Run(context.Background(), w, inv, func(string) []string { return nil }, RunOptions{Arguments: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outputs["a"] != "<findings>hello</findings>" {
		t.Fatalf("a output wrong: %q", res.Outputs["a"])
	}
	bWant := "saw: task=hello prev=<findings>hello</findings>"
	if res.Outputs["b"] != bWant {
		t.Fatalf("b interpolation wrong:\n got: %q\nwant: %q", res.Outputs["b"], bWant)
	}
}

func TestRun_LoopUntilMarker_AdvancesOnSuccess(t *testing.T) {
	w := &Workflow{Name: "w", Stages: []Stage{
		{ID: "v", Skill: "sv", LoopUntilMarker: "## OK", MaxAttempts: 3},
	}}
	inv := &fakeInvoker{
		outputs: map[string][]string{"sv": {"## OK"}},
		calls:   map[string]int{},
	}
	res, err := Run(context.Background(), w, inv, func(string) []string { return []string{"## OK"} }, RunOptions{Arguments: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.calls["sv"] != 1 {
		t.Fatalf("expected 1 invocation, got %d", inv.calls["sv"])
	}
	if got := res.StagesExecuted[0].Action; got != "advance" {
		t.Fatalf("action = %q want advance", got)
	}
}

func TestRun_LoopUntilMarker_RedoRoutesBack(t *testing.T) {
	w := &Workflow{Name: "w", Stages: []Stage{
		{ID: "plan", Skill: "sp", Inputs: []string{"$ARGUMENTS"}},
		{ID: "check", Skill: "sc",
			DependsOn: []string{"plan"},
			Inputs:    []string{"plan: {plan.output}"},
			LoopUntilMarker: "## VERIFICATION PASSED",
			OnMarker:        map[string]string{"## ISSUES FOUND": "redo:plan"},
			MaxAttempts:     3,
		},
	}}
	if err := Validate(w); err != nil {
		t.Fatal(err)
	}
	inv := &fakeInvoker{
		outputs: map[string][]string{
			"sp": {"plan v1", "plan v2"},
			"sc": {"## ISSUES FOUND", "## VERIFICATION PASSED"},
		},
		calls: map[string]int{},
	}
	res, err := Run(context.Background(), w, inv, func(s string) []string {
		switch s {
		case "sc":
			return []string{"## VERIFICATION PASSED", "## ISSUES FOUND"}
		}
		return nil
	}, RunOptions{Arguments: "task"})
	if err != nil {
		t.Fatalf("run err: %v", err)
	}
	if inv.calls["sp"] != 2 {
		t.Fatalf("plan should have been re-run once (total 2), got %d", inv.calls["sp"])
	}
	if inv.calls["sc"] != 2 {
		t.Fatalf("checker should have run twice, got %d", inv.calls["sc"])
	}
	if res.FinalStage != "check" || res.Outputs["check"] != "## VERIFICATION PASSED" {
		t.Fatalf("unexpected final state: %+v", res)
	}
}

func TestRun_LoopExhausted_Fails(t *testing.T) {
	w := &Workflow{Name: "w", Stages: []Stage{
		{ID: "v", Skill: "sv", LoopUntilMarker: "## OK", MaxAttempts: 2},
	}}
	inv := &fakeInvoker{
		outputs: map[string][]string{"sv": {"## NOT YET", "## STILL NO"}},
		calls:   map[string]int{},
	}
	_, err := Run(context.Background(), w, inv, func(string) []string { return []string{"## OK"} }, RunOptions{Arguments: "x"})
	if err == nil {
		t.Fatal("expected failure when max_attempts exhausted")
	}
}

func TestRun_Parallel_JoinsOutputs(t *testing.T) {
	w := &Workflow{Name: "w", Stages: []Stage{
		{ID: "r", Skill: "sr", Parallel: 3, Inputs: []string{"$ARGUMENTS"}},
	}}
	inv := &fakeInvoker{
		outputs: map[string][]string{"sr": {"out-{Q}"}},
		calls:   map[string]int{},
	}
	res, err := Run(context.Background(), w, inv, func(string) []string { return nil }, RunOptions{Arguments: "X"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.calls["sr"] != 3 {
		t.Fatalf("expected 3 parallel calls, got %d", inv.calls["sr"])
	}
	if !strings.Contains(res.Outputs["r"], "---") {
		t.Fatalf("parallel output should have separator: %q", res.Outputs["r"])
	}
}

func TestDiscover_ProjectAndUser(t *testing.T) {
	root := t.TempDir()
	writeWF(t, filepath.Join(root, ".orchestra", "workflows"), "p.yaml",
		"name: p\nstages:\n  - id: a\n    skill: x\n")
	ws, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].Name != "p" {
		t.Fatalf("got: %v", ws)
	}
}
