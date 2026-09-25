package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/internal/roles"
)

// modeSpec returns the registry entry of the agent's mode. A mode outside the
// registry — "" or a custom agent's name — runs with build's tools, and so
// with build's write policy.
func (a *Agent) modeSpec() roles.Spec {
	if spec, ok := roles.Lookup(string(a.opts.Mode)); ok {
		return spec
	}
	spec, _ := roles.Lookup(string(ModeBuild))
	return spec
}

// fileChangingTools change files, or run a skill that may. A read-only mode
// refuses all of them.
var fileChangingTools = map[string]bool{
	"write": true, "edit": true, "fs.delete": true, "fs.rename": true,
	"ast_rename": true, "lsp.rename": true, "skill_invoke": true,
}

// writeScopeRefusal applies the mode's write policy (roles.Spec.Write) to a
// call: a read-only mode refuses whatever changes files, and a scoped mode
// refuses write and edit outside its scope. The final answer's patches meet
// the same scopes through finalPatchRefusal.
func (a *Agent) writeScopeRefusal(name string, input json.RawMessage) error {
	spec := a.modeSpec()
	if spec.NoExec && name == "bash" {
		return fmt.Errorf("%s mode is read-only", a.modeLabel())
	}
	if spec.ReadOnly() {
		if fileChangingTools[name] {
			return readOnlyRefusal(a.modeLabel(), spec)
		}
		return nil
	}
	if err := a.runtimeOwnedCallRefusal(name, input); err != nil {
		return err
	}
	if name != "write" && name != "edit" {
		return nil
	}
	switch spec.Write {
	case roles.WritePlan, roles.WriteOrchestraLead, roles.WriteDeptLead:
		if !a.leadWritablePath(extractWriteOrEditPath(input)) {
			return a.leadScopeRefusal(spec)
		}
		if err := a.checkDeptPlaybookNarrowing(input); err != nil {
			return err
		}
		return a.checkLocalPlaybookOverlayGate(name, input)
	case roles.WriteTargets:
		return a.checkWorkerEditScope(name, input)
	case roles.WriteProduct:
		return a.checkProductEditScope(name, input)
	case roles.WriteDocs:
		return a.checkDocsEditScope(name, input)
	}
	return nil
}

func readOnlyRefusal(label string, spec roles.Spec) error {
	if spec.Tools.Caps {
		return fmt.Errorf("%s mode is read-only (bash allowed for verification commands)", label)
	}
	return fmt.Errorf("%s mode is read-only", label)
}

// leadScopeRefusal refuses a write outside a planning mode's documents.
//
// A refusal that names only where writes ARE allowed answers a question the
// model did not ask. It wanted this file changed, and being told it may write
// to .orchestra/plans/*.md instead leaves it with nowhere to go — so it calls
// write again, and again, until the denied-repeat breaker ends the turn with
// nothing done. That is measured, not supposed: an orchestra run against a
// local model spent every one of its steps this way.
//
// So each of these says what to do with the file as well as what not to do.
// The route is in the system prompt already; the point is that it is here too,
// at the moment of the refusal.
func (a *Agent) leadScopeRefusal(spec roles.Spec) error {
	label := "plan mode"
	scope := fmt.Sprintf("%s or .orchestra/plans/*.md", a.effectivePlanPath())
	nextStep := ""
	switch spec.Write {
	case roles.WriteOrchestraLead:
		label = "orchestra lead"
		scope = fmt.Sprintf("%s, .orchestra/plans/*.md, .orchestra/playbooks/local/{dept}.md%s (state.md and .orchestra/depts/*.md through update_working_state)",
			a.effectivePlanPath(), ownedArtifactsScope(contract.OwnerOrchestrator))
		nextStep = ". To change this file, delegate it: task(subagent_type=\"worker\", prompt=\"…\") — the worker edits, you do not"
	case roles.WritePlan:
		nextStep = ". Plan mode does not edit; describe the change in the plan, and it is applied after you plan_exit"
	case roles.WriteDeptLead:
		label = "architecture mode"
		scope = fmt.Sprintf("%s, .orchestra/plans/*.md, .orchestra/playbooks/{dept}.md, .orchestra/playbooks/local/{dept}.md (decision_ref required), .orchestra/specs/** (not conventions.md)%s",
			a.effectivePlanPath(), ownedArtifactsScope(deptType(a.opts.Dept)))
	}
	return fmt.Errorf("%s: writes are allowed only to %s%s", label, scope, nextStep)
}

// runtimeOwnedCallRefusal refuses write, edit, delete and rename of the
// runtime's own records (plan.IsRuntimeOwnedPath), in every mode.
func (a *Agent) runtimeOwnedCallRefusal(name string, input json.RawMessage) error {
	var req struct {
		Path    string `json:"path"`
		NewPath string `json:"new_path"`
	}
	switch name {
	case "write", "edit":
		return a.runtimeOwnedRefusal(extractWriteOrEditPath(input), plan.IsRuntimeOwnedPath)
	case "fs.delete", "fs.rename":
		_ = json.Unmarshal(input, &req)
		if err := a.runtimeOwnedRefusal(req.Path, plan.HoldsRuntimeOwnedPath); err != nil {
			return err
		}
		return a.runtimeOwnedRefusal(req.NewPath, plan.IsRuntimeOwnedPath)
	}
	return nil
}

// runtimeOwnedRefusal refuses a change to one of the runtime's records. They
// change only through the tools that keep their rules. state.md written with
// write skipped every one of them: the phase-transition guard, and the
// waivers and runtime fields kept from the file, so a Lead could grant itself
// the user's waivers and walk the delivery gate. A decision log the model
// writes is a user approval it gave itself (dept playbooks check it), and an
// EPOCH.yaml it writes is a contract nobody froze.
func (a *Agent) runtimeOwnedRefusal(path string, owned func(string) bool) error {
	rel := strings.TrimSpace(path)
	if rel == "" {
		return nil
	}
	if filepath.IsAbs(rel) {
		r, err := filepath.Rel(a.tools.WorkspaceRoot(), rel)
		if err != nil {
			return nil // outside the project: the runner refuses it
		}
		rel = r
	}
	if !owned(rel) {
		return nil
	}
	return fmt.Errorf("%s is the runtime's record, not a file to write: %s", plan.NormalizeRelPath(rel), runtimeOwnedRoute(rel))
}

// runtimeOwnedRoute says how a runtime record does change.
func runtimeOwnedRoute(rel string) string {
	p := strings.ToLower(plan.NormalizeRelPath(rel))
	switch {
	case p == plan.OrchestraStateRelPath:
		return "the orchestra Lead changes it with update_working_state{content}, which checks the phase transition; waivers other than prd/contract are the user's to grant"
	case strings.HasPrefix(p, plan.OrchestraDeptsRelDir):
		return "the orchestra Lead changes a department scratchpad with update_working_state{content, dept}; the runtime appends worker results"
	case strings.HasSuffix(p, "decisions.md"):
		return "it records the user's answers, appended by the runtime; to ask the user, return open_questions in task_result or use the question tool"
	case strings.HasSuffix(p, "epoch.yaml"):
		return "the runtime maintains it: contract_freeze freezes the artifacts, and an owner's write to an artifact moves the epoch"
	case strings.HasPrefix(p, ".orchestra/agency"):
		return "agents talk through send_message and agent_post"
	}
	return "the runtime maintains it"
}

// ownedArtifactsScope lists, for a refusal, the contract artifacts owner may
// write.
func ownedArtifactsScope(owner string) string {
	var names []string
	for _, name := range contract.RequiredArtifacts {
		if contract.OwnedBy(contract.DirRel+"/"+name, owner) {
			names = append(names, contract.DirRel+"/"+name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return ", " + strings.Join(names, ", ") + " (the contract your role owns)"
}

// deptType strips a department instance suffix: backend@api → backend, the
// owner name in contract.DefaultOwners.
func deptType(dept string) string {
	dept = strings.TrimSpace(dept)
	if i := strings.Index(dept, "@"); i > 0 {
		return dept[:i]
	}
	return dept
}
