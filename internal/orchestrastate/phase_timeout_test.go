package orchestrastate

import (
	"strings"
	"testing"
	"time"
)

func TestPhaseTimeoutWarning(t *testing.T) {
	timeouts := PhaseTimeouts{DiscoveryS: 900, ContractS: 900}
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

	// Fresh phase — no warning.
	st := &State{Phase: PhaseDiscovery, PhaseSince: now.Add(-5 * time.Minute).Format(time.RFC3339)}
	if w := st.PhaseTimeoutWarning(timeouts, now); w != "" {
		t.Fatalf("fresh discovery must not warn: %q", w)
	}

	// Overdue discovery — warns with an unblock path.
	st.PhaseSince = now.Add(-20 * time.Minute).Format(time.RFC3339)
	w := st.PhaseTimeoutWarning(timeouts, now)
	if w == "" || !strings.Contains(w, "phase_timeout") || !strings.Contains(w, "discovery") {
		t.Fatalf("overdue discovery must warn, got %q", w)
	}

	// Execution is open-ended.
	exec := &State{Phase: PhaseExecution, PhaseSince: now.Add(-24 * time.Hour).Format(time.RFC3339)}
	if w := exec.PhaseTimeoutWarning(timeouts, now); w != "" {
		t.Fatalf("execution must never warn: %q", w)
	}

	// Missing stamp / disabled budget / nil state — silent.
	if w := (&State{Phase: PhaseContract}).PhaseTimeoutWarning(timeouts, now); w != "" {
		t.Fatalf("no stamp → no warning: %q", w)
	}
	off := PhaseTimeouts{}
	stale := &State{Phase: PhaseContract, PhaseSince: now.Add(-time.Hour).Format(time.RFC3339)}
	if w := stale.PhaseTimeoutWarning(off, now); w != "" {
		t.Fatalf("zero budget disables: %q", w)
	}
	var nilState *State
	if w := nilState.PhaseTimeoutWarning(timeouts, now); w != "" {
		t.Fatalf("nil state silent: %q", w)
	}
}

func TestSaveMaintainsPhaseSince(t *testing.T) {
	root := t.TempDir()

	// First save stamps the phase.
	if err := Save(root, &State{Phase: PhaseDiscovery, PRDStatus: "draft"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	st, _, err := Load(root)
	if err != nil || st.PhaseSince == "" {
		t.Fatalf("first save must stamp phase_since: %v %+v", err, st)
	}
	first := st.PhaseSince

	// Same-phase save keeps the stamp.
	st.PRDStatus = "approved"
	st.PhaseSince = "" // caller does not manage the stamp
	if err := Save(root, st); err != nil {
		t.Fatalf("save: %v", err)
	}
	st2, _, _ := Load(root)
	if st2.PhaseSince != first {
		t.Fatalf("same phase must keep the stamp: %q → %q", first, st2.PhaseSince)
	}

	// Phase change refreshes the stamp (values may coincide within the
	// same second — assert the stamp exists and parses, phase switched).
	st2.Phase = PhaseContract
	if err := Save(root, st2); err != nil {
		t.Fatalf("save: %v", err)
	}
	st3, _, _ := Load(root)
	if st3.Phase != PhaseContract || st3.PhaseSince == "" {
		t.Fatalf("phase change must keep a stamp: %+v", st3)
	}
	if _, err := time.Parse(time.RFC3339, st3.PhaseSince); err != nil {
		t.Fatalf("stamp must be RFC3339: %v", err)
	}
}

func TestTouchPhaseStamp(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, "---\norchestra:\n  phase: discovery\n  prd_status: draft\n---\n")

	// External write without a stamp → touch sets it.
	if err := TouchPhaseStamp(root, ""); err != nil {
		t.Fatalf("touch: %v", err)
	}
	st, _, _ := Load(root)
	if st.PhaseSince == "" {
		t.Fatal("touch must stamp a phase change")
	}
	stamp := st.PhaseSince

	// Same phase → stamp preserved.
	if err := TouchPhaseStamp(root, PhaseDiscovery); err != nil {
		t.Fatalf("touch: %v", err)
	}
	st2, _, _ := Load(root)
	if st2.PhaseSince != stamp {
		t.Fatalf("same phase must keep the stamp: %q → %q", stamp, st2.PhaseSince)
	}

	// Missing state file → no-op, no error.
	if err := TouchPhaseStamp(t.TempDir(), PhaseDiscovery); err != nil {
		t.Fatalf("missing state must no-op: %v", err)
	}
}
