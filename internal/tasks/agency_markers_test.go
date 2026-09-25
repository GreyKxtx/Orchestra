package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// A failed WorkOrder is recorded unticked under Not done, not as done.
func TestDeptScratchpad_FailedWorkerIsNotDone(t *testing.T) {
	r, root := newAgencyRunner(t, &scriptLLM{}, ChildAgentConfig{})
	wo := &WorkOrder{TaskID: "wo-7", Context: map[string]any{"scratchpad": ".orchestra/depts/backend.md"}}
	r.recordWorkerToDeptScratchpad(wo, `{"status":"verification_failed","verification":{"passed":false,"checks":[{"name":"go_test","ok":false}]}}`, "done", "")
	r.recordWorkerToDeptScratchpad(&WorkOrder{TaskID: "wo-8", Context: wo.Context}, `{"status":"verified_success","path":"b.go"}`, "done", "")
	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "depts", "backend.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	done, notDone, ok := strings.Cut(body, "## Not done")
	if !ok || !strings.Contains(notDone, "- [ ] wo-7: worker verification_failed failed=[go_test]") {
		t.Fatalf("the failure goes unticked under Not done:\n%s", body)
	}
	if strings.Contains(done, "wo-7") || !strings.Contains(done, "- [x] wo-8: worker verified_success") {
		t.Fatalf("Done holds the success only:\n%s", body)
	}
}

// A full inbox tells its reader how many notes it let go of.
func TestInbox_OverflowIsReported(t *testing.T) {
	r, _ := newAgencyRunner(t, &scriptLLM{}, ChildAgentConfig{Agency: agencyOn()})
	for i := 0; i < inboxMaxMessages+3; i++ {
		if err := r.appendInbox("backend", agent.InboxMessage{From: "frontend", Kind: "note", Message: "n"}); err != nil {
			t.Fatal(err)
		}
	}
	notes := r.takeInbox("backend")
	if len(notes) != inboxMaxMessages+1 {
		t.Fatalf("want the kept notes plus one runtime note, got %d", len(notes))
	}
	if notes[0].From != "runtime" || !strings.Contains(notes[0].Message, "3 older notes") {
		t.Fatalf("the first note must say how many were dropped: %+v", notes[0])
	}
}

// trimThread counts what it drops; the count persists with the thread.
func TestThread_TrimmedExchangesAreCounted(t *testing.T) {
	var msgs []llmMessage
	for i := 0; i < threadMaxMessages/2+3; i++ {
		msgs = append(msgs, llmMessage{Role: "user", Content: "q"}, llmMessage{Role: "assistant", Content: "a"})
	}
	kept, dropped := trimThread(msgs)
	if dropped != 3 || len(kept) != threadMaxMessages {
		t.Fatalf("dropped=%d kept=%d", dropped, len(kept))
	}
	r, _ := newAgencyRunner(t, &scriptLLM{}, ChildAgentConfig{Agency: agencyOn()})
	if err := r.saveThread("frontend", "backend", kept, dropped); err != nil {
		t.Fatal(err)
	}
	if got, n := r.loadThread("frontend", "backend"); len(got) != len(kept) || n != 3 {
		t.Fatalf("loaded %d messages, dropped=%d", len(got), n)
	}
}

// A conversation whose start was trimmed says so to the recipient.
func TestSendMessage_TellsTheRecipientTheThreadWasTrimmed(t *testing.T) {
	m := &scriptLLM{reply: func(llm.CompleteRequest) llm.Message { return finish("ack") }}
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	if err := r.saveThread("orchestrator", "backend", []llmMessage{{Role: "user", Content: "old q"}, {Role: "assistant", Content: "old a"}}, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SendMessage(context.Background(), agent.AgentMessageRequest{To: "backend", Message: "new question"}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.seen) == 0 {
		t.Fatal("the recipient never ran")
	}
	if seen := conversation(m.seen[0]); !strings.Contains(seen, "its 4 earliest exchanges were trimmed") {
		t.Fatalf("the recipient must be told the thread was trimmed:\n%s", seen)
	}
	// The marker is for the reader, not part of the conversation on disk.
	if got, _ := r.loadThread("orchestrator", "backend"); strings.Contains(got[len(got)-2].Content, "trimmed") {
		t.Fatalf("the stored message must be the message itself: %q", got[len(got)-2].Content)
	}
}
