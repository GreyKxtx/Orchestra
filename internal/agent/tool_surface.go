package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/internal/roles"
	"github.com/orchestra/orchestra/internal/tools"
)

// The tool list a mode offers is its boundary. It used to be advice only:
// the dispatcher ran any registered tool by name, so an explore child could
// call write and a plan-mode turn could stage production edits the moment the
// model named a tool it had not been given.

// offersTool reports whether name is in this agent's tool list for the step.
func (a *Agent) offersTool(name string) bool {
	for _, d := range a.buildToolDefs() {
		if d.Function.Name == name {
			return true
		}
	}
	return false
}

// offeredToolRefusal refuses a call to a known tool this agent was not given.
// A name that is no tool at all is left to tools.Call, whose "unknown tool"
// answer lists what exists.
func (a *Agent) offeredToolRefusal(name string) error {
	switch name {
	case "task_result", "plan_enter":
		// Their handlers answer the not-offered case with a direction of
		// their own (finish with a final answer; stay in this mode).
		return nil
	}
	// Without consent the consent gates answer, and say how to grant it (bash
	// may still ask the user); the tool list is checked once consent exists.
	if isWebTool(name) && !a.opts.AllowWeb {
		return nil
	}
	if (name == "bash" || strings.HasPrefix(name, "bash.")) && !a.opts.AllowExec {
		return nil
	}
	known := tools.IsRegisteredTool(name) || IsAgencyTool(name) || isAgentInProcessTool(name) ||
		strings.HasPrefix(name, "mcp:") || strings.HasPrefix(name, "browser.")
	if !known || a.offersTool(name) {
		return nil
	}
	return fmt.Errorf("%s is not available in %s mode — it is not in your tool list; use one of the tools you were given", name, a.modeLabel())
}

func (a *Agent) modeLabel() string {
	if a.opts.Mode == "" {
		return string(ModeBuild)
	}
	return string(a.opts.Mode)
}

// leadWritablePath is the write scope of the planning modes (plan,
// orchestra, architecture), shared by write/edit and final.patches.
func (a *Agent) leadWritablePath(path string) bool {
	switch a.modeSpec().Write {
	case roles.WriteOrchestraLead:
		return plan.IsOrchestraLeadWritablePath(path, a.effectivePlanPath())
	case roles.WriteDeptLead:
		// Dept Lead surface (spec §6.1): plans + L2 playbook + specs.
		return plan.IsDeptLeadWritablePath(path, a.effectivePlanPath())
	default:
		return plan.IsWritablePath(path, a.effectivePlanPath())
	}
}

// finalPatchRefusal applies to a final.patches entry the rules a write to the
// same path would meet. The final answer used to be a way around all of them:
// a worker scoped to a.go returned a patch for another file, an explore or
// scout child with no write tool at all returned one, and both were staged
// and flushed with the turn.
func (a *Agent) finalPatchRefusal(path string) error {
	if !a.offersTool("write") && !a.offersTool("edit") {
		return fmt.Errorf("%s mode does not change files; report what should change in your answer instead of returning patches", a.modeLabel())
	}
	spec := a.modeSpec()
	switch spec.Write {
	case roles.WritePlan, roles.WriteOrchestraLead, roles.WriteDeptLead:
		if !a.leadWritablePath(path) {
			return fmt.Errorf("%s mode: %s is outside the files this mode may write", a.modeLabel(), path)
		}
	case roles.WriteNone:
		return fmt.Errorf("%s mode is read-only", a.modeLabel())
	}
	input, _ := json.Marshal(map[string]string{"path": path})
	for _, check := range []func(string, json.RawMessage) error{
		a.checkWorkerEditScope, a.checkProductEditScope, a.checkDocsEditScope,
	} {
		if err := check("write", input); err != nil {
			return err
		}
	}
	return nil
}

// readOnlyChildren reports whether this agent promised the user not to change
// files — plan, architecture and ask at the top level — so what it delegates
// must not change them either. A Dept Lead (architecture as a child) hands out
// WorkOrders by design; its reach is governed by agency.flows instead.
func (a *Agent) readOnlyChildren() bool {
	return !a.opts.IsChild && a.modeSpec().ReadOnlyChildren
}

// ReadOnlyRole reports whether a child of this role leaves files alone.
func ReadOnlyRole(role string) bool {
	spec, ok := roles.Lookup(role)
	return ok && spec.ReadOnly()
}
