package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// finalRecorderLLM answers every step with a final and keeps what each request
// carried.
type finalRecorderLLM struct {
	mu   sync.Mutex
	reqs [][]llm.Message
}

func (r *finalRecorderLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (r *finalRecorderLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, append([]llm.Message(nil), req.Messages...))
	r.mu.Unlock()
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
}

func carries(msgs []llm.Message, text string) bool {
	for _, m := range msgs {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "<user_message>") && strings.Contains(m.Content, text) {
			return true
		}
	}
	return false
}

// A message the user sent while the turn ran reaches the model at the next
// step, as the user's own words.
func TestInterjection_ReachesTheNextStep(t *testing.T) {
	rec := &finalRecorderLLM{}
	var calls int
	ag, _ := newTestAgent(t, rec, Options{Mode: ModeAsk, Interjections: func(final bool) []string {
		calls++
		if !final && calls == 1 {
			return []string{"use tabs, not spaces"}
		}
		return nil
	}})
	if _, _, err := ag.Run(context.Background(), nil, "tidy a.txt"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.reqs) == 0 || !carries(rec.reqs[0], "use tabs, not spaces") {
		t.Fatalf("the model never saw the message; first request = %+v", rec.reqs)
	}
}

// A final that arrives with a message pending does not end the turn: the
// model is given the message and answers again.
func TestInterjection_AtTheFinalKeepsTheTurnGoing(t *testing.T) {
	rec := &finalRecorderLLM{}
	var finals int
	ag, _ := newTestAgent(t, rec, Options{Mode: ModeAsk, Interjections: func(final bool) []string {
		if final {
			finals++
			if finals == 1 {
				return []string{"also add a README"}
			}
		}
		return nil
	}})
	if _, _, err := ag.Run(context.Background(), nil, "tidy a.txt"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.reqs) < 2 {
		t.Fatalf("the turn ended at the first final; requests = %d", len(rec.reqs))
	}
	if !carries(rec.reqs[1], "also add a README") {
		t.Fatalf("the second step did not carry the message: %+v", rec.reqs[1])
	}
}

// Compaction keeps what the user wrote word for word; a message sent during a
// turn is as much the user's as the turn's query.
func TestUsersWords_KeepsInterjections(t *testing.T) {
	hist := []llm.Message{
		{Role: llm.RoleUser, Content: "<user_query>\nbuild the parser\n</user_query>"},
		{Role: llm.RoleAssistant, Content: "working"},
		{Role: llm.RoleUser, Content: interjectionMessage("and keep it streaming").Content},
	}
	words := usersWords(hist)
	if len(words) != 2 || words[1] != "and keep it streaming" {
		t.Fatalf("usersWords = %q", words)
	}
}
