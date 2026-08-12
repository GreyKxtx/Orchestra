// Package orchestrastate owns the Orchestra session state file
// (.orchestra/state.md) and the fail-closed phase guard evaluated before
// every subagent spawn. See docs/architecture/orchestra-routing.md §4.2, §5.1.
package orchestrastate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// StateFileRel is the state file path relative to the project root.
const StateFileRel = ".orchestra/state.md"

// PRDFileRel is the Product Lead output checked by the PRD gate.
const PRDFileRel = ".orchestra/product/PRD.md"

// Phase is the orchestra session phase (state machine in spec §4.2).
type Phase string

const (
	PhaseDiscovery     Phase = "discovery"
	PhaseDocumentation Phase = "documentation"
	PhaseContract      Phase = "contract"
	PhaseExecution     Phase = "execution"
	PhaseDelivery      Phase = "delivery"
	PhaseMaintenance   Phase = "maintenance"
)

// ValidPhase reports whether p is a known phase.
func ValidPhase(p Phase) bool {
	switch p {
	case PhaseDiscovery, PhaseDocumentation, PhaseContract, PhaseExecution, PhaseDelivery, PhaseMaintenance:
		return true
	}
	return false
}

// State is the frontmatter of .orchestra/state.md plus the markdown body.
type State struct {
	Phase               Phase  `yaml:"phase"`
	PRDStatus           string `yaml:"prd_status,omitempty"`           // draft | approved
	ContractEpoch       int    `yaml:"contract_epoch,omitempty"`       // increments on contract artifact change
	ClarificationRounds int    `yaml:"clarification_rounds,omitempty"` // reset on phase change
	StateBytes          int    `yaml:"state_bytes,omitempty"`

	// DocDebt lists docs files (MANIFEST paths) invalidated by code changes
	// during the epic (spec §2.3.2). Accumulated during 2–5, handed to the
	// Docs Lead at 6b as one batch; unresolved debt blocks the release (6c).
	DocDebt []string `yaml:"doc_debt,omitempty"`

	// Waivers are user-granted gate bypasses (spec §4.2 unblock paths):
	// "prd", "contract", "playbooks", "doc_debt". A waiver is an explicit
	// user decision recorded in the state file (and mirrored in
	// decisions.md); guards honor only the waivable set — human gates
	// G2–G4 are never waiver-driven.
	Waivers []string `yaml:"waivers,omitempty"`

	// PhaseSince is the RFC3339 timestamp of the last phase change,
	// maintained by Save (spec §4.5 phase timeouts). Runtime-owned.
	PhaseSince string `yaml:"phase_since,omitempty"`
	// BlockedSince marks the first blocked task_result of the current
	// blockage window; cleared by any non-blocked child result. When a new
	// blocked result arrives after blocked_escalate_s, the runtime forces
	// a question to the User (spec §4.5). Runtime-owned.
	BlockedSince string `yaml:"blocked_since,omitempty"`

	// Body is the markdown content after the frontmatter (## Goal, epics, …).
	Body string `yaml:"-"`
}

type stateFrontmatter struct {
	Orchestra State `yaml:"orchestra"`
}

// Waivable gate names (spec §4.2 unblock column, §4.6 waivable list).
const (
	WaiverPRD               = "prd"
	WaiverContract          = "contract"
	WaiverPlaybooks         = "playbooks"
	WaiverDocDebt           = "doc_debt"
	WaiverBriefCompleteness = "brief_completeness"
)

// HasWaiver reports whether the user granted the named waiver. Unknown names
// never match — the waivable set is closed.
func (s *State) HasWaiver(name string) bool {
	if s == nil {
		return false
	}
	switch name {
	case WaiverPRD, WaiverContract, WaiverPlaybooks, WaiverDocDebt, WaiverBriefCompleteness:
	default:
		return false
	}
	for _, w := range s.Waivers {
		if strings.EqualFold(strings.TrimSpace(w), name) {
			return true
		}
	}
	return false
}

// Load reads the state file. Missing file is not an error: it returns
// (nil, false, nil) so callers can treat the phase machine as disabled
// (backward compatibility with pre-vNext projects).
func Load(projectRoot string) (*State, bool, error) {
	path := filepath.Join(projectRoot, filepath.FromSlash(StateFileRel))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", StateFileRel, err)
	}
	st, err := parse(string(data))
	if err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", StateFileRel, err)
	}
	return st, true, nil
}

func parse(content string) (*State, error) {
	front, body, ok := splitFrontmatter(content)
	if !ok {
		return nil, fmt.Errorf("missing YAML frontmatter (--- … ---)")
	}
	var fm stateFrontmatter
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	st := fm.Orchestra
	if st.Phase != "" && !ValidPhase(st.Phase) {
		return nil, fmt.Errorf("unknown phase %q", st.Phase)
	}
	st.Body = body
	return &st, nil
}

// splitFrontmatter extracts YAML between the leading "---" fences.
func splitFrontmatter(content string) (front, body string, ok bool) {
	s := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return "", "", false
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", false
	}
	front = rest[:end]
	body = strings.TrimPrefix(rest[end+len("\n---"):], "\n")
	return front, body, true
}

// Save writes the state file atomically (temp → fsync → rename).
// It also maintains phase_since (spec §4.5): when the phase differs from the
// on-disk state (or the stamp is missing), the timestamp is refreshed.
func Save(projectRoot string, st *State) error {
	if st == nil {
		return fmt.Errorf("nil state")
	}
	if st.Phase != "" && !ValidPhase(st.Phase) {
		return fmt.Errorf("unknown phase %q", st.Phase)
	}
	if prev, found, err := Load(projectRoot); err == nil {
		switch {
		case !found || prev.Phase != st.Phase:
			st.PhaseSince = time.Now().UTC().Format(time.RFC3339)
		case st.PhaseSince == "":
			st.PhaseSince = prev.PhaseSince
		}
	}
	fm, err := yaml.Marshal(stateFrontmatter{Orchestra: *st})
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n")
	if st.Body != "" {
		b.WriteString(st.Body)
		if !strings.HasSuffix(st.Body, "\n") {
			b.WriteString("\n")
		}
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(StateFileRel))
	return fsutil.AtomicWriteFile(path, []byte(b.String()), 0o644)
}

// TouchPhaseStamp refreshes phase_since after an external write to state.md
// (the orchestrator edits the file via update_working_state, bypassing Save).
// prevPhase is the phase before the write ("" when the file did not exist).
func TouchPhaseStamp(projectRoot string, prevPhase Phase) error {
	st, found, err := Load(projectRoot)
	if err != nil || !found {
		return err
	}
	if st.PhaseSince == "" || st.Phase != prevPhase {
		st.PhaseSince = time.Now().UTC().Format(time.RFC3339)
		return Save(projectRoot, st)
	}
	return nil
}

// PhaseTimeouts carries the resolved orchestra.phase_timeouts values in
// seconds (spec §4.5). Zero disables the corresponding timeout.
type PhaseTimeouts struct {
	DiscoveryS       int
	ContractS        int
	LeadBriefS       int
	BlockedEscalateS int
}

// PhaseTimeoutWarning returns a non-empty advisory when the current phase has
// outlived its budget. Only discovery and contract are budgeted — execution
// and maintenance are open-ended by design. The warning is advice for the
// orchestrator (escalate to the user / switch phase), never a hard block.
func (s *State) PhaseTimeoutWarning(t PhaseTimeouts, now time.Time) string {
	if s == nil || s.PhaseSince == "" {
		return ""
	}
	since, err := time.Parse(time.RFC3339, s.PhaseSince)
	if err != nil {
		return ""
	}
	var budget int
	switch s.Phase {
	case PhaseDiscovery:
		budget = t.DiscoveryS
	case PhaseContract:
		budget = t.ContractS
	default:
		return ""
	}
	if budget <= 0 {
		return ""
	}
	elapsed := int(now.Sub(since).Seconds())
	if elapsed <= budget {
		return ""
	}
	return fmt.Sprintf("phase_timeout: phase %q running for %ds (budget %ds, phase_since %s); "+
		"escalate to the user: finish the phase, switch to maintenance, or record a waiver",
		s.Phase, elapsed, budget, s.PhaseSince)
}

// EnforcementStrict / EnforcementPromptOnly are orchestra.phase_enforcement values.
const (
	EnforcementStrict     = "strict"
	EnforcementPromptOnly = "prompt_only"
)

// isExecuting reports whether the subagent mutates the workspace and is
// therefore phase-gated. explore/ask/debug/architecture/verifier are
// read-only against production files and stay unrestricted.
func isExecuting(subagentType string) bool {
	return strings.EqualFold(strings.TrimSpace(subagentType), "worker")
}

// GuardSpawn is the fail-closed phase gate evaluated before spawning a child.
// Every returned error contains an explicit unblock path (spec §5.2 — a guard
// without an unblock path is a defect).
//
// The guard is inactive when phase_enforcement is prompt_only or when the
// state file does not exist (pre-vNext projects and plain agent mode).
func GuardSpawn(projectRoot, enforcement, subagentType string) error {
	if strings.EqualFold(strings.TrimSpace(enforcement), EnforcementPromptOnly) {
		return nil
	}
	st, found, err := Load(projectRoot)
	if err != nil {
		// Fail closed: a corrupted state file must not silently disable gating.
		return fmt.Errorf("runtime_guard: %w; unblock: fix or delete %s", err, StateFileRel)
	}
	if !found {
		return nil
	}
	// maintenance legally bypasses the PRD and contract gates
	// (contract_refs checks arrive with the contract layer, PR8).
	if st.Phase == PhaseMaintenance {
		return nil
	}
	if !isExecuting(subagentType) {
		return nil
	}
	if (st.Phase == PhaseDiscovery || !prdApproved(projectRoot, st)) && !st.HasWaiver(WaiverPRD) {
		return fmt.Errorf("runtime_guard: PRD status != approved (phase=%s); "+
			"unblock: spawn product | phase=maintenance | user waiver 'prd' in %s", phaseLabel(st.Phase), StateFileRel)
	}
	if st.Phase == PhaseContract && !st.HasWaiver(WaiverContract) {
		return fmt.Errorf("runtime_guard: contract not frozen; "+
			"unblock: complete Domain_Model+NFR+OpenAPI v0 + contract_freeze | user waiver 'contract' in %s", StateFileRel)
	}
	if st.Phase == PhaseContract && st.HasWaiver(WaiverContract) {
		return nil
	}
	if st.Phase != PhaseExecution {
		return fmt.Errorf("runtime_guard: workers allowed only in execution|maintenance (phase=%s); "+
			"unblock: transition per state machine | phase=maintenance", phaseLabel(st.Phase))
	}
	return nil
}

// GuardWorkOrderContract is the Contract Epoch gate (spec §5.3) evaluated for
// worker WorkOrders at spawn and re-evaluated on success. Inactive when
// enforcement is prompt_only, the state file is absent, the phase is
// maintenance, or the contract layer is not adopted (no EPOCH.yaml and no refs).
func GuardWorkOrderContract(projectRoot, enforcement string, refs []contract.Ref) error {
	if strings.EqualFold(strings.TrimSpace(enforcement), EnforcementPromptOnly) {
		return nil
	}
	st, found, err := Load(projectRoot)
	if err != nil {
		return fmt.Errorf("runtime_guard: %w; unblock: fix or delete %s", err, StateFileRel)
	}
	if !found || st.Phase == PhaseMaintenance {
		return nil
	}
	_, epochFound, err := contract.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("runtime_guard: %w; unblock: fix or delete %s", err, contract.EpochFileRel)
	}
	if !epochFound {
		if len(refs) == 0 {
			return nil // contract layer not adopted
		}
		return fmt.Errorf("runtime_guard: WorkOrder carries contract_refs but %s does not exist; "+
			"unblock: freeze the contract (stage 2.5) or drop the refs", contract.EpochFileRel)
	}
	if st.Phase == PhaseExecution && len(refs) == 0 && !st.HasWaiver(WaiverContract) {
		return fmt.Errorf("runtime_guard: WorkOrder without contract_refs is invalid in execution once the contract is frozen; " +
			"unblock: Lead regenerates the WorkOrder with contract_refs from EPOCH.yaml | phase=maintenance | user waiver 'contract'")
	}
	if err := contract.VerifyRefs(projectRoot, refs); err != nil {
		return fmt.Errorf("runtime_guard: %w", err)
	}
	return nil
}

// ArchiveDirRel is where trimmed state history goes (spec §6.4: старые эпики
// архивируются, state.md держит только активный контекст).
const ArchiveDirRel = ".orchestra/archive"

// DefaultStateMaxBytes is the state.md size budget before archiving.
const DefaultStateMaxBytes = 16 * 1024

// ArchiveOverflow trims .orchestra/state.md when it exceeds maxBytes: the
// older head of the body moves to .orchestra/archive/state-<n>.md, the
// frontmatter and the recent tail stay. Cut prefers a "## " section boundary
// so archived epics stay whole. Returns the archive path when trimming
// happened.
func ArchiveOverflow(projectRoot string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultStateMaxBytes
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(StateFileRel))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if len(data) <= maxBytes {
		return "", nil
	}
	st, err := parse(string(data))
	if err != nil {
		// Corrupt state fails closed in the guards; archiving must not
		// destroy evidence.
		return "", nil
	}
	body := st.Body
	keep := maxBytes / 2
	if len(body) <= keep {
		return "", nil // frontmatter dominates; nothing sensible to trim
	}
	cut := len(body) - keep
	if idx := strings.Index(body[cut:], "\n## "); idx >= 0 && cut+idx+1 < len(body) {
		cut += idx + 1
	}
	head, tail := body[:cut], body[cut:]

	dir := filepath.Join(projectRoot, filepath.FromSlash(ArchiveDirRel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	archName := fmt.Sprintf("state-%s.md", time.Now().UTC().Format("20060102-150405"))
	archPath := filepath.Join(dir, archName)
	for i := 2; ; i++ {
		if _, err := os.Stat(archPath); os.IsNotExist(err) {
			break
		}
		archPath = filepath.Join(dir, fmt.Sprintf("state-%s-%d.md", time.Now().UTC().Format("20060102-150405"), i))
	}
	header := fmt.Sprintf("# Archived from %s at %s\n\n", StateFileRel, time.Now().UTC().Format("2006-01-02 15:04"))
	if err := fsutil.AtomicWriteFile(archPath, []byte(header+head), 0o644); err != nil {
		return "", err
	}

	rel := ArchiveDirRel + "/" + filepath.Base(archPath)
	st.Body = "> Older content archived to " + rel + "\n\n" + strings.TrimLeft(tail, "\n")
	st.StateBytes = len(st.Body)
	if err := Save(projectRoot, st); err != nil {
		return "", err
	}
	return rel, nil
}

// AddDocDebt appends a docs path to the state's doc_debt list (idempotent),
// creating nothing when the state file does not exist.
func AddDocDebt(projectRoot, docPath string) error {
	docPath = strings.TrimSpace(docPath)
	if docPath == "" {
		return nil
	}
	st, found, err := Load(projectRoot)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for _, p := range st.DocDebt {
		if p == docPath {
			return nil
		}
	}
	st.DocDebt = append(st.DocDebt, docPath)
	return Save(projectRoot, st)
}

func phaseLabel(p Phase) string {
	if p == "" {
		return "unset"
	}
	return string(p)
}

// prdApproved checks state frontmatter first, then the PRD.md frontmatter
// (status: approved) as fallback.
func prdApproved(projectRoot string, st *State) bool {
	if st != nil && strings.EqualFold(strings.TrimSpace(st.PRDStatus), "approved") {
		return true
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(PRDFileRel)))
	if err != nil {
		return false
	}
	front, _, ok := splitFrontmatter(string(data))
	if !ok {
		return false
	}
	var fm struct {
		Status string `yaml:"status"`
	}
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fm.Status), "approved")
}
