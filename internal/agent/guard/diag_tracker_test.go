package guard

import (
	"encoding/json"
	"testing"
)

func TestDiagTracker_StreaksOnIdenticalFingerprint(t *testing.T) {
	tk := NewDiagTracker()
	if n := tk.Observe("foo.go", "abc"); n != 1 {
		t.Fatalf("first observe: streak=%d, want 1", n)
	}
	if n := tk.Observe("foo.go", "abc"); n != 2 {
		t.Fatalf("second identical observe: streak=%d, want 2", n)
	}
	if n := tk.Observe("foo.go", "abc"); n != 3 {
		t.Fatalf("third identical observe: streak=%d, want 3", n)
	}
}

func TestDiagTracker_ResetOnDifferentFingerprint(t *testing.T) {
	tk := NewDiagTracker()
	tk.Observe("foo.go", "abc")
	tk.Observe("foo.go", "abc")
	if n := tk.Observe("foo.go", "xyz"); n != 1 {
		t.Errorf("changed fingerprint: streak=%d, want 1 (reset)", n)
	}
}

func TestDiagTracker_EmptyFingerprintClearsState(t *testing.T) {
	tk := NewDiagTracker()
	tk.Observe("foo.go", "abc")
	tk.Observe("foo.go", "abc")
	if n := tk.Observe("foo.go", ""); n != 0 {
		t.Errorf("clean state: streak=%d, want 0", n)
	}
	// And the next non-empty observe should restart from 1, not continue
	// the previous streak.
	if n := tk.Observe("foo.go", "abc"); n != 1 {
		t.Errorf("after clear: streak=%d, want 1", n)
	}
}

func TestDiagTracker_PerPathIsolation(t *testing.T) {
	tk := NewDiagTracker()
	tk.Observe("foo.go", "abc")
	tk.Observe("foo.go", "abc")
	if n := tk.Observe("bar.go", "abc"); n != 1 {
		t.Errorf("different path with same fingerprint: streak=%d, want 1", n)
	}
}

func TestDiagTracker_NilSafe(t *testing.T) {
	var tk *DiagTracker
	if n := tk.Observe("foo.go", "abc"); n != 0 {
		t.Errorf("nil tracker: streak=%d, want 0", n)
	}
}

func TestFingerprintLSPErrors_StableForSameDiagnostics(t *testing.T) {
	a := json.RawMessage(`{"diagnostics":[
		{"severity":"error","message":"undefined: foo","start_line":12,"start_col":4},
		{"severity":"error","message":"missing return","start_line":33,"start_col":0}
	]}`)
	// Same diagnostics in reverse order — fingerprint should be identical
	// because we sort lexically before hashing.
	b := json.RawMessage(`{"diagnostics":[
		{"severity":"error","message":"missing return","start_line":33,"start_col":0},
		{"severity":"error","message":"undefined: foo","start_line":12,"start_col":4}
	]}`)
	fa := FingerprintLSPErrors(a)
	fb := FingerprintLSPErrors(b)
	if fa == "" || fa != fb {
		t.Errorf("expected identical fingerprints, got fa=%q fb=%q", fa, fb)
	}
}

func TestFingerprintLSPErrors_EmptyOnNoErrors(t *testing.T) {
	a := json.RawMessage(`{"diagnostics":[{"severity":"warning","message":"unused"}]}`)
	if fp := FingerprintLSPErrors(a); fp != "" {
		t.Errorf("warnings only should produce empty fingerprint, got %q", fp)
	}
	if fp := FingerprintLSPErrors(json.RawMessage(`{}`)); fp != "" {
		t.Errorf("empty diagnostics should produce empty fingerprint, got %q", fp)
	}
}

func TestFingerprintLSPErrors_DifferForDifferentErrors(t *testing.T) {
	a := json.RawMessage(`{"diagnostics":[{"severity":"error","message":"undefined: foo","start_line":12}]}`)
	b := json.RawMessage(`{"diagnostics":[{"severity":"error","message":"undefined: bar","start_line":12}]}`)
	if FingerprintLSPErrors(a) == FingerprintLSPErrors(b) {
		t.Error("different messages should produce different fingerprints")
	}
}
