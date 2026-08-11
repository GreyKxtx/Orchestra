package agent

import (
	"strings"
	"testing"
)

func TestCompactWorkerResultForLead(t *testing.T) {
	raw := `{"status":"verified_success","worker_result":{"status":"success","path":"internal/api/handler.go"},"verification":{"passed":true}}`
	got := CompactWorkerResultForLead(raw, 500)
	if got == "" || len(got) > 500 {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "handler.go") {
		t.Fatalf("expected compact summary: %q", got)
	}
}

func TestAppendScratchpadDoneLine(t *testing.T) {
	in := "## Goal\nfix auth\n\n## Done\n\n## Next\n"
	got := appendScratchpadDoneLine(in, "- [x] worker done")
	if !strings.Contains(got, "- [x] worker done") {
		t.Fatalf("got %q", got)
	}
}

func TestLooksLikeWorkerResult(t *testing.T) {
	if !looksLikeWorkerResult(`{"status":"success","path":"a.go"}`) {
		t.Fatal("expected worker shape")
	}
	if looksLikeWorkerResult(`{"status":"done","result":"explore findings"}`) {
		t.Fatal("explore result should not match")
	}
}
