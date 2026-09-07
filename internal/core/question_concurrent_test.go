package core

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/tools"
)

// Until MCP elicitation existed, question/ask had exactly one source: the
// agent's own question tool, inside a turn, serialized by runMu. An MCP
// server's elicitation now issues it too, from a server goroutine — so two
// can now overlap, and the clients cannot survive that: the TUI keeps ONE
// questionModal and ONE questionReqID (ui/tui/app.go), so the second request
// overwrites the first and the first's caller waits for an answer nobody can
// give it any more. rpcPermissionRequester already serializes for exactly
// this reason; the question path was left without it.
func TestRPCQuestionAsker_SerializesConcurrentAsks(t *testing.T) {
	var inFlight, maxInFlight int32
	release := make(chan struct{})
	var releaseOnce sync.Once

	asker := &rpcQuestionAsker{requestFn: func(_ context.Context, _ string, _ any, result any) error {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if n <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, n) {
				break
			}
		}
		// The first caller in holds the "modal" open until the second has had
		// every chance to barge in.
		releaseOnce.Do(func() {
			go func() {
				time.Sleep(150 * time.Millisecond)
				close(release)
			}()
		})
		<-release
		atomic.AddInt32(&inFlight, -1)

		if r, ok := result.(*struct {
			Answers []string `json:"answers"`
		}); ok {
			r.Answers = []string{"ok"}
		}
		return nil
	}}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = asker.Ask(context.Background(), []tools.QuestionItem{{Question: "?"}})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got > 1 {
		t.Fatalf("%d question/ask requests were in flight at once; the TUI has one modal slot, "+
			"so the earlier one is silently dropped and its caller hangs", got)
	}
}

// A per-instance lock only serializes callers that share the instance, and
// the handler used to build a fresh asker for every agent.run / session.message
// and another for the MCP host. The collision that actually matters is between
// two different sources: the agent's question tool mid-turn and a server's
// elicitation. They must serialize against each other, so every source has to
// come from the same asker.
func TestRPCHandler_AllQuestionSourcesShareOneAsker(t *testing.T) {
	h := NewRPCHandler(&Core{mcpHost: newMCPHost(nil)})

	var inFlight, maxInFlight int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	h.SetRequester(func(_ context.Context, _ string, _ any, result any) error {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if n <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, n) {
				break
			}
		}
		releaseOnce.Do(func() {
			go func() {
				time.Sleep(150 * time.Millisecond)
				close(release)
			}()
		})
		<-release
		atomic.AddInt32(&inFlight, -1)
		if r, ok := result.(*struct {
			Answers []string `json:"answers"`
		}); ok {
			r.Answers = []string{"ok"}
		}
		return nil
	})

	// One asker as an agent turn gets it, one as the MCP host got it.
	agentAsker := h.questionAskerForRun()
	mcpAsker := h.core.mcpHost.asker
	if mcpAsker == nil {
		t.Fatal("SetRequester did not bind an asker to the MCP host")
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = agentAsker.Ask(context.Background(), []tools.QuestionItem{{Question: "agent"}})
	}()
	go func() {
		defer wg.Done()
		_, _ = mcpAsker.Ask(context.Background(), []tools.QuestionItem{{Question: "server"}})
	}()
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got > 1 {
		t.Fatalf("an agent question and an MCP elicitation were in flight at once (%d); "+
			"they came from different askers, so neither lock saw the other", got)
	}
}

// Sharing one asker is a property of the wiring, not of any single call, and
// the test above can only reach the wiring through questionAskerForRun. A
// dispatch arm that goes back to building its own asker would slip past it and
// resurrect the collision, so the single construction site is asserted
// directly: exactly one, inside SetRequester.
func TestRPCQuestionAsker_HasExactlyOneConstructionSite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var sites []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "&rpcQuestionAsker{") {
				sites = append(sites, fmt.Sprintf("%s:%d", name, i+1))
			}
		}
	}
	if len(sites) != 1 {
		t.Fatalf("rpcQuestionAsker is built at %d places (%v); every source of question/ask must "+
			"share the one instance, or its lock cannot see the other sources", len(sites), sites)
	}
	if !strings.HasPrefix(sites[0], "rpc_handler.go:") {
		t.Errorf("the one asker is built at %s; it belongs in RPCHandler.SetRequester", sites[0])
	}
}
