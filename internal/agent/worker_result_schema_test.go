package agent

import (
	"strings"
	"testing"
)

func TestCheckWorkerResultSchema(t *testing.T) {
	strict := &Agent{opts: Options{Mode: ModeWorker, WorkerStrictResult: true}}

	// Valid shapes pass.
	for _, c := range []string{
		`{"status":"success","path":"a.go","summary":"done"}`,
		`{"status":"error","summary":"build failed"}`,
		`{"status":"blocked","blocked_reason":"missing_answer"}`,
		`{"status":"DONE"}`,
	} {
		if err := strict.checkWorkerResultSchema(c); err != nil {
			t.Fatalf("%s must pass: %v", c, err)
		}
	}

	// Invalid shapes are rejected with the template hint.
	for _, c := range []string{
		"",
		"I edited the file and everything works now.",
		`["status","success"]`,
		`{"summary":"no status"}`,
		`{"status":"partial"}`,
		`{"status":"blocked"}`, // blocked without blocked_reason
	} {
		err := strict.checkWorkerResultSchema(c)
		if err == nil {
			t.Fatalf("%q must be rejected", c)
		}
		if !strings.Contains(err.Error(), `"status"`) {
			t.Fatalf("error must carry the JSON template, got: %v", err)
		}
	}

	// Non-strict workers and other modes are exempt.
	loose := &Agent{opts: Options{Mode: ModeWorker}}
	if err := loose.checkWorkerResultSchema("free text"); err != nil {
		t.Fatalf("non-WorkOrder worker exempt: %v", err)
	}
	lead := &Agent{opts: Options{Mode: ModeOrchestra, WorkerStrictResult: true}}
	if err := lead.checkWorkerResultSchema("free text"); err != nil {
		t.Fatalf("non-worker mode exempt: %v", err)
	}
}
