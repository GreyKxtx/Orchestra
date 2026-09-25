package agent

import (
	"encoding/json"
	"fmt"

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
	scope := fmt.Sprintf("%s, .orchestra/plans/*.md, .orchestra/state.md, or .orchestra/depts/*.md", a.effectivePlanPath())
	nextStep := ""
	switch spec.Write {
	case roles.WriteOrchestraLead:
		label = "orchestra lead"
		nextStep = ". To change this file, delegate it: task(subagent_type=\"worker\", prompt=\"…\") — the worker edits, you do not"
	case roles.WritePlan:
		nextStep = ". Plan mode does not edit; describe the change in the plan, and it is applied after you plan_exit"
	case roles.WriteDeptLead:
		label = "architecture mode"
		scope = fmt.Sprintf("%s, .orchestra/plans/*.md, .orchestra/playbooks/{dept}.md, .orchestra/playbooks/local/{dept}.md (decision_ref required), .orchestra/specs/** (not conventions.md)", a.effectivePlanPath())
	}
	return fmt.Errorf("%s: writes are allowed only to %s%s", label, scope, nextStep)
}
