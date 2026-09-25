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
	"github.com/orchestra/orchestra/internal/roles"
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

// ParseContent reads a state document that has not been written yet. The phase
// guard needs the incoming phase before the file is replaced, so a refused
// transition leaves the old state in place.
func ParseContent(content string) (*State, error) {
	return parse(content)
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
	rendered, err := Render(st)
	if err != nil {
		return err
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(StateFileRel))
	return fsutil.AtomicWriteFile(path, []byte(rendered), 0o644)
}

// Render is the state file's text: the YAML frontmatter block and the body.
// Exported so update_working_state can put a frontmatter on a body the Lead
// wrote without one, instead of writing a file the guard cannot read.
func Render(st *State) (string, error) {
	if st == nil {
		return "", fmt.Errorf("nil state")
	}
	fm, err := yaml.Marshal(stateFrontmatter{Orchestra: *st})
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
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
	return b.String(), nil
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

// isExecuting reports whether the subagent can mutate production files and is
// therefore phase-gated. The child types that may spawn in any phase are the
// spawnable roles whose writes stay out of production code
// (!roles.Spec.WritesCode): read-only against the workspace, or confined to
// the artifacts a phase exists to produce —
//
//	explore, ask, verifier,
//	scout                    — no write/edit tool at all
//	architecture             — writes confined to plans, the L2 playbook and specs
//	product                  — writes confined to .orchestra/product/ (and the PRD
//	                           gate's own unblock path is "spawn product")
//	documentation            — writes confined to conventions.md, .orchestra/docs/, docs/
//
// Anything else mutates production files and is phase-gated.
//
// It is an allowlist rather than a "gate the writers" denylist because the
// denylist form was open at both ends. It named only "worker", so general and
// debug children — both carrying write, edit and (general) delete/rename —
// walked through the gate untouched; and an unrecognised subagent_type falls
// through tools.ListToolsForMode's default arm to the full build surface, so a
// typo or a custom agent name bypassed the phase machine entirely. Invariant
// #7 is fail-closed spawn: an unknown child type is gated, not waved through.
//
// An empty type defaults to explore (see tasks.childToolsForSubagent), which
// is read-only.
func isExecuting(subagentType string) bool {
	t := strings.ToLower(strings.TrimSpace(subagentType))
	if t == "" {
		return false
	}
	spec, ok := roles.Lookup(t)
	return !ok || !spec.Spawnable || spec.WritesCode()
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
			"unblock: spawn product | phase=maintenance | waiver 'prd' in %s", phaseLabel(st.Phase), StateFileRel)
	}
	if st.Phase == PhaseContract && !st.HasWaiver(WaiverContract) {
		return fmt.Errorf("runtime_guard: contract not frozen; "+
			"unblock: complete Domain_Model+NFR+OpenAPI v0 + contract_freeze | waiver 'contract' in %s", StateFileRel)
	}
	if st.Phase == PhaseContract && st.HasWaiver(WaiverContract) {
		return nil
	}
	if st.Phase != PhaseExecution {
		return fmt.Errorf("runtime_guard: %s writes production files, allowed only in execution|maintenance (phase=%s); "+
			"unblock: transition per state machine | phase=maintenance | delegate the read-only part to explore|verifier",
			subagentLabel(subagentType), phaseLabel(st.Phase))
	}
	return nil
}

// ConventionsFileRel is the L1 playbook the Docs Lead writes at stage 1. The
// contract phase opens once it exists (spec §4.2 transition table). It lives
// here rather than in tasks because the phase guard needs it and tasks already
// depends on this package.
const ConventionsFileRel = ".orchestra/playbooks/conventions.md"

// GuardPhaseTransition enforces the session state machine's transition table
// (spec §4.2). The Lead drives phases by writing state.md through
// update_working_state, and that write used to be unchecked: any phase could
// be set from any other, so "execution opens once the contract is frozen" and
// "delivery waits for doc_debt to clear" were diagram, not runtime. A machine
// whose transitions are advisory is a machine the model can skip.
//
// Only conditions the runtime can observe on disk are enforced. "Epics closed"
// and "verify green" have no ledger to read yet, so the delivery gate checks
// doc_debt alone; the remaining conditions stay the Lead's judgment, which the
// prompt states and this guard does not pretend to check.
//
// Every refusal names an unblock path (spec §5.2). Inactive under prompt_only,
// and on the first write of a state file (no prior phase) so bootstrapping a
// session is never blocked.
func GuardPhaseTransition(projectRoot, enforcement string, from, to Phase, next *State) error {
	if strings.EqualFold(strings.TrimSpace(enforcement), EnforcementPromptOnly) {
		return nil
	}
	if from == "" || to == "" || from == to {
		return nil
	}
	switch to {
	// maintenance is itself an unblock path, and a scope change legitimately
	// reopens discovery from anywhere.
	case PhaseMaintenance, PhaseDiscovery:
		return nil

	case PhaseDocumentation:
		if prdApproved(projectRoot, next) || next.HasWaiver(WaiverPRD) {
			return nil
		}
		return fmt.Errorf("runtime_guard: documentation needs an approved PRD (prd_status=%s); "+
			"unblock: spawn product | phase=maintenance | waiver 'prd' in %s",
			prdStatusLabel(next), StateFileRel)

	case PhaseContract:
		if fileExists(projectRoot, ConventionsFileRel) || next.HasWaiver(WaiverPlaybooks) {
			return nil
		}
		return fmt.Errorf("runtime_guard: contract needs L1 conventions (%s missing); "+
			"unblock: spawn documentation | user waiver 'playbooks' in %s (L0 defaults)",
			ConventionsFileRel, StateFileRel)

	case PhaseExecution:
		if next.HasWaiver(WaiverContract) {
			return nil
		}
		_, found, err := contract.Load(projectRoot)
		if err != nil {
			return fmt.Errorf("runtime_guard: %w; unblock: fix or delete the contract epoch file", err)
		}
		if found {
			return nil
		}
		return fmt.Errorf("runtime_guard: execution needs a frozen contract (no contract epoch recorded); "+
			"unblock: complete Domain_Model+NFR+OpenAPI v0 + contract_freeze | waiver 'contract' in %s",
			StateFileRel)

	case PhaseDelivery:
		if len(next.DocDebt) == 0 || next.HasWaiver(WaiverDocDebt) {
			return nil
		}
		return fmt.Errorf("runtime_guard: delivery needs doc_debt empty (%d file(s) still owed: %s); "+
			"unblock: spawn documentation to close them | user waiver 'doc_debt' in %s",
			len(next.DocDebt), strings.Join(next.DocDebt, ", "), StateFileRel)
	}
	return nil
}

func fileExists(projectRoot, rel string) bool {
	st, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(rel)))
	return err == nil && !st.IsDir()
}

func prdStatusLabel(st *State) string {
	if st == nil || strings.TrimSpace(st.PRDStatus) == "" {
		return "unset"
	}
	return strings.TrimSpace(st.PRDStatus)
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
			"unblock: Lead regenerates the WorkOrder with contract_refs from EPOCH.yaml | phase=maintenance | waiver 'contract'")
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

// subagentLabel names the blocked child in a guard message. An unrecognised
// type is gated on purpose (see isExecuting), and the message says
// so — otherwise a typo reads as an unexplained refusal.
func subagentLabel(subagentType string) string {
	t := strings.ToLower(strings.TrimSpace(subagentType))
	if t == "" {
		return "child"
	}
	if spec, ok := roles.Lookup(t); ok && spec.Spawnable {
		return t
	}
	return fmt.Sprintf("%q (unknown child type, gated as a writer)", t)
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

// modelGrantableWaivers are the waivers the Orchestrator may declare itself,
// for a job it sized as small (orchestra.txt). The rest — playbooks, doc_debt,
// brief_completeness — are the user's to grant.
var modelGrantableWaivers = map[string]bool{WaiverPRD: true, WaiverContract: true}

// KeepRuntimeOwned makes next — a state.md the model wrote — keep what only
// the runtime or the user may set, taken from prev (the file on disk, nil for
// none): prd_status, contract_epoch, clarification_rounds, doc_debt,
// phase_since, blocked_since, state_bytes, and the waivers outside the
// model-grantable set. The model may still add prd/contract waivers and drop
// any waiver. It returns the fields the content tried to change.
//
// The transition guard reads next. It used to take all of it from the model:
// a Lead could write waivers: [doc_debt] or an empty doc_debt and walk the
// delivery gate, or erase clarification_rounds and blocked_since.
func KeepRuntimeOwned(next, prev *State) []string {
	if next == nil {
		return nil
	}
	var p State
	if prev != nil {
		p = *prev
	}
	var changed []string
	if next.PRDStatus != p.PRDStatus {
		changed = append(changed, "prd_status")
		next.PRDStatus = p.PRDStatus
	}
	if next.ContractEpoch != p.ContractEpoch {
		changed = append(changed, "contract_epoch")
		next.ContractEpoch = p.ContractEpoch
	}
	if next.ClarificationRounds != p.ClarificationRounds {
		changed = append(changed, "clarification_rounds")
		next.ClarificationRounds = p.ClarificationRounds
	}
	if strings.Join(next.DocDebt, "\x00") != strings.Join(p.DocDebt, "\x00") {
		changed = append(changed, "doc_debt")
		next.DocDebt = append([]string(nil), p.DocDebt...)
	}
	if next.PhaseSince != p.PhaseSince {
		next.PhaseSince = p.PhaseSince // maintained by the phase stamp, silently
	}
	if next.BlockedSince != p.BlockedSince {
		changed = append(changed, "blocked_since")
		next.BlockedSince = p.BlockedSince
	}
	next.StateBytes = p.StateBytes

	var waivers []string
	seen := map[string]bool{}
	add := func(w string) {
		k := strings.ToLower(strings.TrimSpace(w))
		if k != "" && !seen[k] {
			seen[k] = true
			waivers = append(waivers, k)
		}
	}
	userGranted := map[string]bool{}
	for _, w := range p.Waivers {
		k := strings.ToLower(strings.TrimSpace(w))
		if !modelGrantableWaivers[k] {
			userGranted[k] = true
			add(k)
		}
	}
	for _, w := range next.Waivers {
		k := strings.ToLower(strings.TrimSpace(w))
		switch {
		case modelGrantableWaivers[k]:
			add(k)
		case !userGranted[k]:
			changed = append(changed, "waivers:"+k)
		}
	}
	next.Waivers = waivers
	return changed
}
