package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const failedWorkerJSON = `{"status":"verification_failed",
 "worker_result":{"status":"success","path":"billing/invoice.go","summary":"added rounding"},
 "verification":{"passed":false,"checks":[
   {"name":"go_build","path":"billing","ok":true},
   {"name":"go_test","path":"billing","ok":false,"detail":"TestRound: got 1.004"},
   {"name":"tsc","skip":true}]},
 "escalated_to_tier":"complex",
 "suggestion_for_lead":"Replan, spawn debug child, or issue a narrower WorkOrder with fix instructions."}`

// A Lead reading its own history must still see what failed and why.
func TestCompactWorkerResult_KeepsWhatTheLeadDecidesWith(t *testing.T) {
	got := CompactWorkerResultForLead(failedWorkerJSON, workerLeadResultMaxBytes)
	for _, want := range []string{"worker verification_failed", "failed=[go_test(billing)]", "escalated_to=complex", "path=billing/invoice.go", "next: Replan"} {
		if !strings.Contains(got, want) {
			t.Errorf("compact result lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "go_build") || strings.Contains(got, "tsc") {
		t.Errorf("only failed checks are named: %s", got)
	}

	blocked := CompactWorkerResultForLead(`{"status":"blocked","blocked_reason":"missing_answer","path":"a.go"}`, orchestraWorkerHistoryCompactBytes)
	if !strings.Contains(blocked, "blocked_reason=missing_answer") {
		t.Errorf("blocked_reason must survive: %s", blocked)
	}

	// The tightest budget keeps the verdict first and says it cut.
	tight := CompactWorkerResultForLead(failedWorkerJSON, 100)
	if !strings.HasPrefix(tight, "worker verification_failed failed=[go_test") || !strings.Contains(tight, "bytes truncated]") {
		t.Errorf("a cut result must lead with the verdict and say it was cut: %s", tight)
	}
}

// task_wait{task_ids}' answer used to compact to the literal "worker ".
func TestOrchestraCompactor_UnderstandsWaitMany(t *testing.T) {
	results := map[string]any{
		"results": []map[string]any{
			{"task_id": "task_1", "status": "done", "result": failedWorkerJSON},
			{"task_id": "task_2", "status": "still_running", "error": "still running after 5s"},
		},
		"integration": map[string]any{"status": "failed", "workers": 2, "files": []string{"a.go"}, "summary": strings.Repeat("go vet: ", 200), "log": strings.Repeat("x", 4000)},
	}
	raw, _ := json.Marshal(results)
	got, ok := orchestraTaskToolCompactor("task_wait", string(raw))
	if !ok {
		t.Fatal("the compactor must take a WaitManyResult")
	}
	if strings.TrimSpace(got) == "worker" {
		t.Fatal("regressed to the literal \"worker \"")
	}
	var back struct {
		Results []struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
			Result string `json:"result"`
		} `json:"results"`
		Integration map[string]any `json:"integration"`
	}
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("compacted answer must stay JSON: %v\n%s", err, got)
	}
	if len(back.Results) != 2 || back.Results[0].TaskID != "task_1" || back.Results[1].Status != "still_running" {
		t.Fatalf("every task keeps its id and status: %+v", back.Results)
	}
	if !strings.Contains(back.Results[0].Result, "failed=[go_test(billing)]") {
		t.Errorf("a worker's result is compacted, not dropped: %q", back.Results[0].Result)
	}
	if back.Integration["status"] != "failed" || back.Integration["log"] != nil {
		t.Errorf("the integration verdict stays, its bulk goes: %v", back.Integration)
	}
	if sum, _ := back.Integration["summary"].(string); !strings.Contains(sum, "bytes truncated]") {
		t.Errorf("a cut summary says so: %q", sum)
	}
	if len(got) > 2000 {
		t.Errorf("compacted answer is still %d bytes", len(got))
	}
}

// A failed worker is not recorded as done.
func TestScratchpad_FailuresAreNotDone(t *testing.T) {
	root := t.TempDir()
	path := orchestraScratchpadAbs(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\norchestra:\n  phase: execution\n---\n## Goal\nship it\n\n## Done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendWorkerSummaryToScratchpad(root, CompactWorkerResultForLead(failedWorkerJSON, workerLeadResultMaxBytes)); err != nil {
		t.Fatal(err)
	}
	if err := appendWorkerSummaryToScratchpad(root, CompactWorkerResultForLead(`{"status":"verified_success","path":"b.go"}`, workerLeadResultMaxBytes)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	body := string(data)
	done, notDone, ok := strings.Cut(body, "## Not done")
	if !ok {
		t.Fatalf("a failure goes under Not done:\n%s", body)
	}
	if !strings.Contains(notDone, "- [ ] worker verification_failed") {
		t.Errorf("the failure is unticked under Not done:\n%s", body)
	}
	if strings.Contains(done, "verification_failed") {
		t.Errorf("the failure must not be in Done:\n%s", body)
	}
	if !strings.Contains(done, "- [x] worker verified_success path=b.go") {
		t.Errorf("a success is ticked under Done:\n%s", body)
	}
}

// Notes that do not fit are handed back, and the block says how many.
func TestFitAgentMessages_ReturnsWhatDidNotFit(t *testing.T) {
	var msgs []InboxMessage
	for i := 0; i < 5; i++ {
		msgs = append(msgs, InboxMessage{From: "backend", Kind: "note", Message: strings.Repeat("n", 100)})
	}
	text, rest := FitAgentMessages(msgs, 400)
	if len(rest) == 0 || len(rest) == len(msgs) {
		t.Fatalf("some notes fit and some do not, got rest=%d", len(rest))
	}
	if !strings.Contains(text, "more notes did not fit here") {
		t.Errorf("the block must say notes are still to come:\n%s", text)
	}
	if all, rest := FitAgentMessages(msgs, 0); len(rest) != 0 || strings.Count(all, "[note from backend]") != 5 {
		t.Errorf("no limit renders everything")
	}
}
