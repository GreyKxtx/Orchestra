package view

import (
	"strings"
	"testing"
)

func TestStripFinalEnvelope_balancedPatches(t *testing.T) {
	in := `done {"type":"final","final":{"patches":[{"x":1}]}} ok`
	got := stripFinalEnvelope(in)
	if strings.Contains(got, "patches") || !strings.Contains(got, "done") || !strings.Contains(got, "ok") {
		t.Fatalf("got %q", got)
	}
}

func TestStripFinalEnvelope_unbalancedPatchesTail(t *testing.T) {
	in := "answer\n{\"type\":\"final\",\"final\":{\"patches\":["
	got := stripFinalEnvelope(in)
	if got != "answer" {
		t.Fatalf("got %q want answer", got)
	}
}
