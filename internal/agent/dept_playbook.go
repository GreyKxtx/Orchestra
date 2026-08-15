package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/internal/playbooks"
)

// L2 playbook narrowing floor (spec §6.1, checklist 14b): a Dept Lead may only
// narrow the project conventions. Weakening them requires an accepted_risk
// with explicit User approval. Semantic narrowing cannot be checked
// deterministically; the fail-closed floor is: every accepted_risks entry in
// the playbook frontmatter must already be approved in decisions.md (the
// runtime writes approvals there via the Question Barrier / waiver flow).
func (a *Agent) checkDeptPlaybookNarrowing(input json.RawMessage) error {
	if a == nil || a.opts.Mode != ModeArchitecture {
		return nil
	}
	var req struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil
	}
	p := plan.NormalizeRelPath(req.Path)
	if _, ok := playbooks.ParseLocalOverlayPath(p); ok {
		return nil
	}
	if !strings.HasPrefix(p, plan.OrchestraPlaybooksRelDir) {
		return nil
	}
	body := req.Content
	if body == "" {
		body = req.NewString // edit: check at least the inserted text
	}
	risks := parseAcceptedRisks(body)
	if len(risks) == 0 {
		return nil
	}
	log := readDecisionLogRaw(a.tools.WorkspaceRoot())
	var unapproved []string
	for _, r := range risks {
		if !strings.Contains(log, r) {
			unapproved = append(unapproved, r)
		}
	}
	if len(unapproved) == 0 {
		return nil
	}
	return fmt.Errorf(
		"L2 playbook may only narrow L1 conventions: accepted_risks %q lack User approval in %s; "+
			"unblock: return the risk via task_result (open_questions/accepted_risk) so the orchestrator asks the user — the approval lands in decisions.md, then retry the write",
		unapproved, decisions.FileRel,
	)
}

// parseAcceptedRisks extracts non-empty accepted_risks entries from the
// playbook's YAML frontmatter (or a bare YAML document without fences).
func parseAcceptedRisks(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	yamlPart := body
	if strings.HasPrefix(body, "---\n") {
		rest := body[len("---\n"):]
		if end := strings.Index(rest, "\n---"); end >= 0 {
			yamlPart = rest[:end]
		}
	}
	var fm struct {
		AcceptedRisks []string `yaml:"accepted_risks"`
	}
	if err := yaml.Unmarshal([]byte(yamlPart), &fm); err != nil {
		return nil
	}
	out := fm.AcceptedRisks[:0]
	for _, r := range fm.AcceptedRisks {
		if strings.TrimSpace(r) != "" {
			out = append(out, strings.TrimSpace(r))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// checkLocalPlaybookOverlayGate requires decision_ref frontmatter approved in
// decisions.md before a Dept/Orchestra Lead may create or replace a local L3
// overlay (.orchestra/playbooks/local/{dept}.md). Edits to an already-approved
// file are allowed without repeating the ref in the patch.
func (a *Agent) checkLocalPlaybookOverlayGate(name string, input json.RawMessage) error {
	if a == nil || (a.opts.Mode != ModeArchitecture && a.opts.Mode != ModeOrchestra) {
		return nil
	}
	if name != "write" && name != "edit" {
		return nil
	}
	var req struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil
	}
	if _, ok := playbooks.ParseLocalOverlayPath(req.Path); !ok {
		return nil
	}
	log := readDecisionLogRaw(a.tools.WorkspaceRoot())
	if name == "write" {
		if playbooks.LocalOverlayApproved(req.Content, log) {
			return nil
		}
		return localOverlayGateError(req.Path)
	}
	existing := readProjectFile(a.tools.WorkspaceRoot(), req.Path)
	if playbooks.LocalOverlayApproved(existing, log) {
		return nil
	}
	if playbooks.LocalOverlayApproved(req.NewString, log) {
		return nil
	}
	return localOverlayGateError(req.Path)
}

func localOverlayGateError(path string) error {
	return fmt.Errorf(
		"local playbook overlay %q requires YAML frontmatter decision_ref approved in %s; "+
			"unblock: ask the User via open_questions — the runtime records the answer in decisions.md and auto-seals decision_ref; "+
			"or write with frontmatter like ---\\ndecision_ref: \"<exact approval text>\"\\n---",
		plan.NormalizeRelPath(path), decisions.FileRel,
	)
}

func readProjectFile(root, relPath string) string {
	p := plan.NormalizeRelPath(relPath)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
	if err != nil {
		return ""
	}
	return string(data)
}

func readDecisionLogRaw(root string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(decisions.FileRel)))
	if err != nil {
		return ""
	}
	return string(data)
}
