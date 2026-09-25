package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/agent/history"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/fsutil"
)

const workerLeadResultMaxBytes = 1200
const orchestraWorkerHistoryCompactBytes = 280

func orchestraScratchpadAbs(root string) string {
	return filepath.Join(root, filepath.FromSlash(plan.OrchestraStateRelPath))
}

// readOrchestraScratchpad loads .orchestra/state.md for prompt inject (or "").
func readOrchestraScratchpad(root string) string {
	b, err := os.ReadFile(orchestraScratchpadAbs(root))
	if err != nil {
		return ""
	}
	body := strings.TrimSpace(string(b))
	if body == "" {
		return ""
	}
	const maxScratchpadInject = 2400
	if len(body) > maxScratchpadInject {
		body = body[:maxScratchpadInject] + "\n...(truncated; use read on .orchestra/state.md for full file)"
	}
	return "<orchestra_scratchpad>\n" + body + "\n</orchestra_scratchpad>"
}

// DeptScratchpadDir is where department-instance scratchpads live (spec §5.8):
// .orchestra/state.md belongs to the Orchestrator, .orchestra/depts/{instance}.md
// to Dept Leads (one file per instance, e.g. frontend@web.md).
const DeptScratchpadDir = ".orchestra/depts"

// deptInstanceRe validates a department instance id: `frontend` or `frontend@web`.
var deptInstanceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(@[a-z0-9][a-z0-9_-]*)?$`)

// DeptScratchpadRelPath maps a dept instance id to its scratchpad path, or ""
// when the id is malformed.
func DeptScratchpadRelPath(instance string) string {
	instance = strings.TrimSpace(instance)
	if !deptInstanceRe.MatchString(instance) {
		return ""
	}
	return DeptScratchpadDir + "/" + instance + ".md"
}

// guardStateTransition evaluates the state machine's entry conditions for the
// phase the Lead is about to write (spec §4.2).
//
// Content the runtime cannot parse is not treated as a transition: state.md is
// a markdown document the Lead also uses for goals and epic notes, and a
// missing frontmatter block is a separate problem from an illegal transition.
// Phase enforcement reads the phase, so no frontmatter means no phase change
// to judge.
func (a *Agent) guardStateTransition(prevPhase orchestrastate.Phase, content string) error {
	next, err := orchestrastate.ParseContent(content)
	if err != nil || next == nil {
		return nil
	}
	return orchestrastate.GuardPhaseTransition(
		a.tools.WorkspaceRoot(), a.opts.PhaseEnforcement, prevPhase, next.Phase, next)
}

func (a *Agent) handleUpdateWorkingState(input json.RawMessage) (json.RawMessage, error) {
	if a.opts.Mode != ModeOrchestra {
		return nil, fmt.Errorf("update_working_state is only available in orchestra Lead mode")
	}
	var req struct {
		Content string `json:"content"`
		Dept    string `json:"dept"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("update_working_state: invalid input: %w", err)
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, fmt.Errorf("update_working_state: content is required")
	}
	relPath := plan.OrchestraStateRelPath
	if dept := strings.TrimSpace(req.Dept); dept != "" {
		relPath = DeptScratchpadRelPath(dept)
		if relPath == "" {
			return nil, fmt.Errorf("update_working_state: invalid dept instance %q (expected e.g. frontend or frontend@web)", dept)
		}
	}
	path := filepath.Join(a.tools.WorkspaceRoot(), filepath.FromSlash(relPath))
	// Phase stamp (spec §4.5): remember the pre-write phase so the runtime
	// can refresh phase_since when the orchestrator switches phases.
	var prevPhase orchestrastate.Phase
	frontmatterAdded := false
	var runtimeOwned []string
	if relPath == plan.OrchestraStateRelPath {
		// Under the state file's lock: the runtime-owned fields kept and the
		// transition judged are those of the state this write replaces, not a
		// copy loaded before a worker's doc debt or a barrier's answer landed.
		err := orchestrastate.Rewrite(a.tools.WorkspaceRoot(), func(prev *orchestrastate.State) (string, error) {
			if prev != nil {
				prevPhase = prev.Phase
			}
			// The Lead writes this file as prose — Goal, Done, Next — and the
			// phase guard reads it as a document with a YAML frontmatter. When
			// the body arrives without one, the guard would fail closed on the
			// very next spawn ("missing YAML frontmatter"), and the Lead, which
			// has no way to see the file's shape, spent its steps rewriting
			// state.md by hand (27B, orchestra runs of 2026-09-18, twice). So
			// the runtime keeps the frontmatter it has — the phase the Lead
			// last declared — and puts the new body under it; a fresh session
			// gets one with the phase unset, which the reply says how to fill.
			if _, perr := orchestrastate.ParseContent(content); perr != nil {
				st := orchestrastate.State{}
				if prev != nil {
					st = *prev
				}
				st.Body = content + "\n"
				rendered, rerr := orchestrastate.Render(&st)
				if rerr != nil {
					return "", fmt.Errorf("update_working_state: %w", rerr)
				}
				content = strings.TrimSpace(rendered)
				frontmatterAdded = true
			}
			// Fields the runtime or the user owns come from the file on disk,
			// whatever the new content says; the guard below judges the result.
			if next, perr := orchestrastate.ParseContent(content); perr == nil {
				if changed := orchestrastate.KeepRuntimeOwned(next, prev); len(changed) > 0 {
					rendered, rerr := orchestrastate.Render(next)
					if rerr != nil {
						return "", fmt.Errorf("update_working_state: %w", rerr)
					}
					content = strings.TrimSpace(rendered)
					runtimeOwned = changed
				}
			}
			// Transition gate (spec §4.2). Evaluated before the write, so a
			// refused transition leaves the state file as it was: the Lead
			// cannot declare a phase whose entry condition is unmet and then
			// act on it.
			if err := a.guardStateTransition(prevPhase, content); err != nil {
				return "", err
			}
			return content + "\n", nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := fsutil.AtomicWriteFile(path, []byte(content+"\n"), 0o644); err != nil {
			return nil, err
		}
	}
	respFields := map[string]any{
		"path":    relPath,
		"written": len(content),
		"status":  "ok",
	}
	if len(runtimeOwned) > 0 {
		respFields["runtime_owned"] = "kept as on file: " + strings.Join(runtimeOwned, ", ") +
			" — the runtime and the user set these (waivers other than prd/contract are the user's to grant)"
	}
	if frontmatterAdded {
		phase := string(prevPhase)
		if phase == "" {
			phase = "unset"
		}
		respFields["frontmatter"] = "kept: orchestra.phase=" + phase
		respFields["note"] = "the content had no YAML frontmatter, so the runtime kept the existing one. " +
			"To declare a phase, start the content with:\n---\norchestra:\n  phase: execution\n---\n" +
			"(phases: discovery, documentation, contract, execution, delivery, maintenance; waivers: orchestra.waivers: [prd])"
	}
	// Context budget (spec §6.4): oversized state.md gets its older head
	// archived deterministically; the model keeps only the active tail.
	if relPath == plan.OrchestraStateRelPath {
		if err := orchestrastate.TouchPhaseStamp(a.tools.WorkspaceRoot(), prevPhase); err != nil {
			a.logf("phase stamp update failed: %v", err)
		}
		if arch, err := orchestrastate.ArchiveOverflow(a.tools.WorkspaceRoot(), a.opts.StateMaxBytes); err != nil {
			a.logf("state archive failed: %v", err)
		} else if arch != "" {
			respFields["archived_to"] = arch
			respFields["note"] = "state.md exceeded state_max_bytes; older content moved to " + arch
		}
	}
	resp, _ := json.Marshal(respFields)
	return resp, nil
}

// CompactWorkerResultForLead shrinks worker/verify JSON for Lead history.
//
// What the Lead decides with comes first, and survives the tightest budget
// (280 bytes in older history): the status, why it is blocked, which checks
// failed, where it escalated to. Path, message and the suggestion follow.
// It used to keep only status, path and message, so a Lead reading its own
// history could no longer tell which checks had failed or why a worker was
// blocked (audit §3.4). A result cut to fit says so, and by how much.
func CompactWorkerResultForLead(raw string, maxBytes int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if maxBytes <= 0 {
		maxBytes = workerLeadResultMaxBytes
	}
	if !json.Valid([]byte(raw)) {
		return clipWithMarker(raw, maxBytes)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw
	}
	status, _ := m["status"].(string)
	line := fmt.Sprintf("worker %s", strings.TrimSpace(status))
	if br := workerField(m, "blocked_reason"); br != "" {
		line += " blocked_reason=" + br
	}
	if failed := failedWorkerChecks(m); len(failed) > 0 {
		line += " failed=[" + strings.Join(failed, ", ") + "]"
	}
	if tier := workerField(m, "escalated_to_tier"); tier != "" {
		line += " escalated_to=" + tier
	}
	if path := extractWorkerPath(m); path != "" {
		line += " path=" + path
	}
	if msg := extractWorkerMessage(m); msg != "" {
		line += " — " + msg
	}
	if s := workerField(m, "suggestion_for_lead"); s != "" {
		line += " | next: " + s
	}
	return clipWithMarker(line, maxBytes)
}

// workerField reads a string field of a worker result, at the top level or
// inside worker_result.
func workerField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if wr, ok := m["worker_result"].(map[string]any); ok {
		if v, ok := wr[key].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// failedWorkerChecks names the verification checks that failed.
func failedWorkerChecks(m map[string]any) []string {
	v, ok := m["verification"].(map[string]any)
	if !ok {
		return nil
	}
	checks, _ := v["checks"].([]any)
	var out []string
	for _, c := range checks {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if okv, _ := cm["ok"].(bool); okv {
			continue
		}
		if skip, _ := cm["skip"].(bool); skip {
			continue
		}
		name, _ := cm["name"].(string)
		if p, _ := cm["path"].(string); p != "" {
			name += "(" + p + ")"
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// clipWithMarker cuts s to max bytes on a rune boundary, and when it cuts,
// says how much it dropped: a result that ends mid-word without a mark reads
// as a complete one.
func clipWithMarker(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	marker := fmt.Sprintf(" …[+%d bytes truncated]", len(s)-max)
	keep := max - len(marker)
	if keep < 0 {
		keep = 0
	}
	for keep > 0 && !utf8.RuneStart(s[keep]) {
		keep--
	}
	return s[:keep] + fmt.Sprintf(" …[+%d bytes truncated]", len(s)-keep)
}

func extractWorkerPath(m map[string]any) string {
	if p, ok := m["path"].(string); ok && strings.TrimSpace(p) != "" {
		return strings.TrimSpace(p)
	}
	if wr, ok := m["worker_result"].(map[string]any); ok {
		if p, ok := wr["path"].(string); ok {
			return strings.TrimSpace(p)
		}
	}
	return ""
}

func extractWorkerMessage(m map[string]any) string {
	if msg, ok := m["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	if wr, ok := m["worker_result"].(map[string]any); ok {
		if msg, ok := wr["message"].(string); ok {
			return strings.TrimSpace(msg)
		}
	}
	if v, ok := m["verification"].(map[string]any); ok {
		if passed, ok := v["passed"].(bool); ok {
			if !passed {
				return "verification failed"
			}
			return "verified"
		}
	}
	return ""
}

func appendWorkerSummaryToScratchpad(root, summaryLine string) error {
	summaryLine = strings.TrimSpace(summaryLine)
	if summaryLine == "" {
		return nil
	}
	// Workers finish in parallel, and the runtime writes this file too: the
	// append is a read-modify-write under the state file's lock.
	unlock, err := orchestrastate.Lock(root)
	if err != nil {
		return err
	}
	defer unlock()
	path := orchestraScratchpadAbs(root)
	var content string
	if b, err := os.ReadFile(path); err == nil {
		content = string(b)
	} else if os.IsNotExist(err) {
		// No state.md means the Lead has not opened the phase machine, and
		// the bookkeeping must not open it. This used to create the file from
		// the old Goal/Done/Next template — no YAML frontmatter — and the very
		// next spawn hit GuardSpawn, which fails closed on a state file it
		// cannot parse ("missing YAML frontmatter"). The runtime's own note
		// about worker one locked out worker two. Seen live on the 27B in
		// orchestra_delegates_two_edits, whenever the Lead spawned the two
		// workers one after the other instead of together. The worker's
		// result already reaches the Lead through the tool result; the Done
		// line is a convenience for a scratchpad that exists.
		return nil
	} else {
		return err
	}
	// Done holds what a worker reported done. A failed or blocked one goes
	// under Not done, unticked: recording it as "- [x]" in Done told the
	// Lead's next read of its scratchpad that the work was finished.
	var updated string
	if workerSummarySucceeded(summaryLine) {
		updated = appendScratchpadDoneLine(content, "- [x] "+summaryLine)
	} else {
		updated = appendScratchpadSectionLine(content, "## Not done", "- [ ] "+summaryLine)
	}
	return fsutil.AtomicWriteFile(path, []byte(strings.TrimRight(updated, "\n")+"\n"), 0o644)
}

// workerSummarySucceeded reads the status CompactWorkerResultForLead put at
// the front of summaryLine ("worker <status> …").
func workerSummarySucceeded(summaryLine string) bool {
	fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(summaryLine), "worker "))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "verified_success", "success", "ok", "done":
		return true
	}
	return false
}

// appendScratchpadSectionLine appends line at the end of section (a "## "
// heading), creating the section when missing.
func appendScratchpadSectionLine(content, section, line string) string {
	content = strings.TrimRight(content, "\n")
	idx := strings.Index(content, section)
	if idx < 0 {
		return content + "\n\n" + section + "\n" + line + "\n"
	}
	after := content[idx+len(section):]
	nextRel := strings.Index(after, "\n## ")
	if nextRel < 0 {
		return content + "\n" + line + "\n"
	}
	insertAt := idx + len(section) + nextRel
	return content[:insertAt] + "\n" + line + content[insertAt:]
}

func appendScratchpadDoneLine(content, line string) string {
	return appendScratchpadSectionLine(content, "## Done", line)
}

func looksLikeWorkerResult(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.Contains(raw, "verified_success") || strings.Contains(raw, "verification_failed") {
		return true
	}
	if !json.Valid([]byte(raw)) {
		return false
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return false
	}
	st, _ := m["status"].(string)
	switch strings.ToLower(strings.TrimSpace(st)) {
	// no_changes and needs_review are worker answers like any other: the Lead
	// has to record and compact them, or the one outcome it most needs to act
	// on is the one that never reaches its scratchpad. needs_review predates
	// no_changes and was missing here for the same reason — the list is written
	// out by hand next to a set of wrap* functions that grew past it.
	case "success", "ok", "done", "error",
		"verification_failed", "verified_success", "no_changes", "needs_review",
		"llm_verification_failed":
		_, hasPath := m["path"]
		_, hasWorker := m["worker_result"]
		return hasPath || hasWorker
	case "no_result":
		// no_result carries neither path nor worker_result: there is no result
		// to carry, which is the whole point of it. The status alone identifies
		// it, and it is the outcome the Lead can least afford to miss.
		return true
	}
	return false
}

func (a *Agent) maybeRecordWorkerToScratchpad(subagentType, result string) {
	if a == nil || a.opts.Mode != ModeOrchestra {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(subagentType), "worker") {
		return
	}
	compact := CompactWorkerResultForLead(result, workerLeadResultMaxBytes)
	if compact == "" {
		return
	}
	_ = appendWorkerSummaryToScratchpad(a.tools.WorkspaceRoot(), compact)
}

// orchestraCompactTaskToolOutput shrinks worker result inside task/task_wait JSON.
func orchestraCompactTaskToolOutput(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	if results, ok := m["results"].([]any); ok {
		return compactWaitManyOutput(m, results)
	}
	res, _ := m["result"].(string)
	if res == "" || !looksLikeWorkerResult(res) {
		return ""
	}
	m["result"] = CompactWorkerResultForLead(res, orchestraWorkerHistoryCompactBytes)
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// compactWaitManyOutput shrinks task_wait{task_ids}' answer — {results[],
// integration} — result by result. Nothing here knew that shape: the whole
// answer, every worker's status and the integration verdict included, used
// to collapse into the literal "worker " (audit §3.4).
func compactWaitManyOutput(m map[string]any, results []any) string {
	for _, r := range results {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if res, _ := rm["result"].(string); res != "" && looksLikeWorkerResult(res) {
			rm["result"] = CompactWorkerResultForLead(res, orchestraWorkerHistoryCompactBytes)
		}
	}
	if integ, ok := m["integration"].(map[string]any); ok {
		kept := map[string]any{}
		for _, k := range []string{"status", "workers", "files"} {
			if v, ok := integ[k]; ok {
				kept[k] = v
			}
		}
		if sum, _ := integ["summary"].(string); sum != "" {
			kept["summary"] = clipWithMarker(sum, orchestraWorkerHistoryCompactBytes)
		}
		m["integration"] = kept
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

func orchestraTaskToolCompactor(_ string, content string) (string, bool) {
	if compact := orchestraCompactTaskToolOutput(content); compact != "" {
		return compact, true
	}
	if looksLikeWorkerResult(content) {
		return CompactWorkerResultForLead(content, orchestraWorkerHistoryCompactBytes), true
	}
	return "", false
}

func collapseOrchestraWorkerTaskHistory(messages []llm.Message, keepRecent int) []llm.Message {
	return history.CollapseOrchestraWorkerTaskOutputs(messages, keepRecent, orchestraTaskToolCompactor)
}
