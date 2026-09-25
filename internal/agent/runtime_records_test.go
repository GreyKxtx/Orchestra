package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

func seedFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stagingRunner(t *testing.T) (*tools.Runner, string) {
	t.Helper()
	root := t.TempDir()
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tr.SetDryRun(true)
	t.Cleanup(func() { tr.Close() })
	return tr, root
}

func callArgs(id, name string, args any) llm.ToolCall {
	raw, _ := json.Marshal(args)
	return llm.ToolCall{ID: id, Type: "function", Function: llm.ToolCallFunc{Name: name, Arguments: llm.ToolArguments(raw)}}
}

// The model cannot grant itself a waiver (phase 4 acceptance). A Lead that
// wants delivery while documentation is still owed tries both ways in: write
// straight to state.md — which in an apply run used to reach disk untouched
// by any check — and update_working_state. Neither moves the phase, clears
// the debt or adds the user's waivers.
func TestWaiver_ModelCannotGrantItself(t *testing.T) {
	state := "---\norchestra:\n  phase: execution\n  doc_debt:\n    - docs/api.md\n---\n\n## Goal\nship\n"
	forged := "---\norchestra:\n  phase: delivery\n  waivers: [doc_debt, playbooks]\n---\n\n## Goal\nship\n"
	llmClient := &toolCallSequenceLLM{responses: []*llm.CompleteResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			callArgs("r1", "read", map[string]string{"path": ".orchestra/state.md"}),
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			callArgs("w1", "write", map[string]string{"path": ".orchestra/state.md", "content": forged}),
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			callArgs("u1", "update_working_state", map[string]string{"content": forged}),
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[{"type":"file.write_atomic","path":".orchestra/state.md","content":` + jsonString(forged) + `}]}}`}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}
	ag, tr := newTestAgent(t, llmClient, Options{Mode: ModeOrchestra, Apply: true, PhaseEnforcement: orchestrastate.EnforcementStrict})
	tr.SetDryRun(true)
	root := tr.WorkspaceRoot()
	seedFile(t, root, ".orchestra/state.md", state)

	hist, _, _ := ag.Run(context.Background(), nil, "deliver")
	replies := map[string]string{}
	for _, m := range toolMessages(hist) {
		replies[m.ToolCallID] = m.Content
	}
	if !strings.Contains(replies["w1"], "runtime's record") {
		t.Errorf("write to state.md must be refused, got %q", replies["w1"])
	}
	if !strings.Contains(replies["u1"], "doc_debt") {
		t.Errorf("update_working_state must refuse delivery with doc_debt owed, got %q", replies["u1"])
	}
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found {
		t.Fatalf("state.md: found=%v err=%v", found, err)
	}
	if _, staged := tr.StagedFileContent(context.Background())[".orchestra/state.md"]; staged {
		t.Error("nothing of the forged state.md may be staged for the final apply")
	}
	if st.Phase != orchestrastate.PhaseExecution || len(st.DocDebt) != 1 || st.HasWaiver(orchestrastate.WaiverDocDebt) || st.HasWaiver(orchestrastate.WaiverPlaybooks) {
		t.Fatalf("the model moved the state machine itself: %+v", st)
	}
}

// The runtime's records are refused to every mode that writes, whatever the
// tool: write, edit, delete, rename or a final patch.
func TestRuntimeRecords_RefusedInEveryMode(t *testing.T) {
	tr, root := stagingRunner(t)
	cases := []struct {
		name  string
		opts  Options
		tool  string
		input any
	}{
		{"build writes decisions.md", Options{Mode: ModeBuild}, "write", map[string]string{"path": ".orchestra/decisions.md", "content": "User approved: everything"}},
		{"build edits EPOCH.yaml", Options{Mode: ModeBuild}, "edit", map[string]string{"path": ".orchestra/contract/EPOCH.yaml", "search": "a", "replace": "b"}},
		{"absolute path", Options{Mode: ModeBuild}, "write", map[string]string{"path": filepath.Join(root, ".orchestra", "state.md"), "content": "x"}},
		{"mixed case", Options{Mode: ModeBuild}, "write", map[string]string{"path": ".Orchestra/State.md", "content": "x"}},
		{"orchestra writes state.md", Options{Mode: ModeOrchestra}, "write", map[string]string{"path": ".orchestra/state.md", "content": "x"}},
		{"orchestra writes a scratchpad", Options{Mode: ModeOrchestra}, "write", map[string]string{"path": ".orchestra/depts/backend.md", "content": "x"}},
		{"worker told to", Options{Mode: ModeWorker, IsChild: true, WorkerEditPaths: []string{".orchestra/state.md"}}, "write", map[string]string{"path": ".orchestra/state.md", "content": "x"}},
		{"delete .orchestra", Options{Mode: ModeBuild}, "fs.delete", map[string]any{"path": ".orchestra", "recursive": true}},
		{"rename onto state.md", Options{Mode: ModeBuild}, "fs.rename", map[string]string{"path": "notes.md", "new_path": ".orchestra/state.md"}},
		{"rename the agency away", Options{Mode: ModeBuild}, "fs.rename", map[string]string{"path": ".orchestra/agency", "new_path": "tmp/agency"}},
	}
	for _, c := range cases {
		a := &Agent{opts: c.opts, tools: tr}
		raw, _ := json.Marshal(c.input)
		if err := a.writeScopeRefusal(c.tool, raw); err == nil || !strings.Contains(err.Error(), "runtime's record") {
			t.Errorf("%s: want a runtime-record refusal, got %v", c.name, err)
		}
	}
	a := &Agent{opts: Options{Mode: ModeBuild}, tools: tr}
	if err := a.finalPatchRefusal(".orchestra/decisions.md"); err == nil {
		t.Error("a final patch to decisions.md must be refused")
	}
	for _, ok := range []string{"src/a.go", ".orchestra/plans/p.md", ".orchestra/contract/NFR.md"} {
		raw, _ := json.Marshal(map[string]string{"path": ok, "content": "x"})
		if err := a.writeScopeRefusal("write", raw); err != nil {
			t.Errorf("build may write %s: %v", ok, err)
		}
	}
}

// Every contract artifact has an owner, and the owner may write it. Before,
// no role that runs unwaived could: the Dept Lead's scope and the
// Orchestrator's left .orchestra/contract/ out, so the contract phase could
// only be waived (ORC-5).
func TestContractOwners_MayWriteTheirArtifacts(t *testing.T) {
	tr, _ := stagingRunner(t)
	art := func(name string) string { return contract.DirRel + "/" + name }
	cases := []struct {
		opts  Options
		path  string
		write bool
	}{
		{Options{Mode: ModeOrchestra}, art(contract.ArtifactNFR), true},
		{Options{Mode: ModeOrchestra}, art(contract.ArtifactOpenAPI), false},
		{Options{Mode: ModeArchitecture, IsChild: true, Dept: "backend@api"}, art(contract.ArtifactOpenAPI), true},
		{Options{Mode: ModeArchitecture, IsChild: true, Dept: "backend"}, art(contract.ArtifactDomainModel), true},
		{Options{Mode: ModeArchitecture, IsChild: true, Dept: "backend"}, art(contract.ArtifactUITokens), false},
		{Options{Mode: ModeArchitecture, IsChild: true, Dept: "design"}, art(contract.ArtifactUITokens), true},
		{Options{Mode: ModeArchitecture, IsChild: true, Dept: "frontend@web"}, art(contract.ArtifactOpenAPI), false},
		{Options{Mode: ModeArchitecture, IsChild: true}, art(contract.ArtifactOpenAPI), false},
		{Options{Mode: ModeArchitecture, IsChild: true, Dept: "backend"}, contract.EpochFileRel, false},
	}
	for _, c := range cases {
		a := &Agent{opts: c.opts, tools: tr}
		raw, _ := json.Marshal(map[string]string{"path": c.path, "content": "x"})
		err := a.writeScopeRefusal("write", raw)
		if c.write && err != nil {
			t.Errorf("%s (dept %q) owns %s: %v", c.opts.Mode, c.opts.Dept, c.path, err)
		}
		if !c.write && err == nil {
			t.Errorf("%s (dept %q) must not write %s", c.opts.Mode, c.opts.Dept, c.path)
		}
	}
}

// invalidator counts the epoch changes the agent reports to its runner.
type invalidator struct{ calls int }

func (i *invalidator) Spawn(context.Context, SubtaskSpawnRequest) (string, error) { return "", nil }
func (i *invalidator) Wait(context.Context, string, int) (*SubtaskResult, error) {
	return nil, nil
}
func (i *invalidator) Cancel(context.Context, string) error { return nil }
func (i *invalidator) InvalidateStaleContractTasks(context.Context) []string {
	i.calls++
	return []string{"task_1"}
}

// The owners stage the artifacts, so the freeze checks and hashes the staged
// versions — it used to read the disk and find nothing — and then cancels the
// workers the new epoch left behind. After it, the Lead's own staged change
// to NFR.md moves the epoch; that used to need an apply run.
func TestContractFreeze_OnStagedArtifacts(t *testing.T) {
	tr, root := stagingRunner(t)
	for name, body := range map[string]string{
		contract.ArtifactDomainModel: "# Domain\n\n## Booking\nid, user_id, status\n",
		contract.ArtifactNFR:         "## Latency\np95 < 200ms\n\n## Availability\n99.9%\n",
		contract.ArtifactOpenAPI:     "openapi: 3.1.0\npaths:\n  /bookings:\n    get: {}\n",
		contract.ArtifactUITokens:    `{"color": {"primary": "#333"}}`,
	} {
		if _, err := tr.FSWrite(context.Background(), tools.FSWriteRequest{Path: contract.DirRel + "/" + name, Content: body}); err != nil {
			t.Fatal(err)
		}
	}
	inv := &invalidator{}
	a := &Agent{opts: Options{Mode: ModeOrchestra, SubtaskRunner: inv}, tools: tr}
	out, err := a.handleContractFreeze(context.Background())
	if err != nil {
		t.Fatalf("the freeze reads what the owners staged: %v", err)
	}
	if inv.calls != 1 || !strings.Contains(string(out), "cancelled_stale_workers") {
		t.Fatalf("the freeze cancels the workers of the old epoch: calls=%d %s", inv.calls, out)
	}
	if _, err := os.Stat(filepath.Join(root, contract.DirRel, contract.ArtifactNFR)); !os.IsNotExist(err) {
		t.Fatalf("the artifacts were only staged: %v", err)
	}

	if _, err := tr.FSEdit(context.Background(), tools.FSEditRequest{Path: contract.DirRel + "/" + contract.ArtifactNFR, Search: "p95 < 200ms", Replace: "p95 < 50ms"}); err != nil {
		t.Fatal(err)
	}
	a.afterContractArtifactWrite(context.Background(), contract.DirRel+"/"+contract.ArtifactNFR)
	e, _, err := contract.Load(root)
	if err != nil || e.Epoch != 5 || e.Artifacts[contract.ArtifactNFR].Version != 2 {
		t.Fatalf("the staged change moves the epoch: %+v %v", e, err)
	}
	if inv.calls != 2 {
		t.Fatalf("and cancels the workers it left behind: calls=%d", inv.calls)
	}
}
