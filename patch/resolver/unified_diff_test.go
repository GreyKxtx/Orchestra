package resolver

import (
	"strings"
	"testing"
)

// TestApplyUnifiedDiff_PreservesCRLF is the H10 regression: a CRLF-encoded
// file must keep CRLF line endings after a successful diff apply.
// Previously every apply silently rewrote the file as LF, churning every
// subsequent commit on Windows / Git-with-autocrlf workflows.
func TestApplyUnifiedDiff_PreservesCRLF(t *testing.T) {
	src := "line1\r\nline2\r\nline3\r\n"
	diff := "@@ -2,1 +2,1 @@\n-line2\n+changed\n"

	out, err := applyUnifiedDiff(src, diff)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(out, "\r\n") {
		t.Errorf("output lost CRLF: %q", out)
	}
	if strings.Contains(out, "line2\r\n") {
		t.Errorf("removed line still present: %q", out)
	}
	if !strings.Contains(out, "changed\r\n") {
		t.Errorf("new line not in output: %q", out)
	}
}

// TestApplyUnifiedDiff_RejectsHunkCountMismatch is the H10 regression:
// a header that declares "-3 +1" but contains 5 actual `-` lines must
// be rejected. Previously this silently corrupted the file.
func TestApplyUnifiedDiff_RejectsHunkCountMismatch(t *testing.T) {
	src := "a\nb\nc\nd\ne\nf\n"
	// Header says only 2 old lines but body has 5 `-` lines.
	diff := "@@ -1,2 +1,1 @@\n-a\n-b\n-c\n-d\n-e\n+X\n"

	_, err := applyUnifiedDiff(src, diff)
	if err == nil {
		t.Fatal("expected hunk-count-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "declares") {
		t.Errorf("error should mention mismatch, got %q", err.Error())
	}
}

// TestApplyUnifiedDiff_NoSilentLFConversion: when the original is LF
// the output stays LF (no \r introduced).
func TestApplyUnifiedDiff_NoSilentLFConversion(t *testing.T) {
	src := "a\nb\nc\n"
	diff := "@@ -2,1 +2,1 @@\n-b\n+B\n"

	out, err := applyUnifiedDiff(src, diff)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("CRLF appeared in LF-original output: %q", out)
	}
}
