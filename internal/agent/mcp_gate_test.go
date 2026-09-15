package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// fakeMCP records which MCP tools actually ran.
type fakeMCP struct{ ran []string }

func (f *fakeMCP) Call(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
	f.ran = append(f.ran, name)
	return json.RawMessage(`{"result":"done"}`), nil
}

// scriptedRequester answers every permission request the same way.
type scriptedRequester struct {
	approve, always bool
	asked           []PermissionRequest
}

func (r *scriptedRequester) RequestPermission(_ context.Context, req PermissionRequest) (PermissionResponse, error) {
	r.asked = append(r.asked, req)
	return PermissionResponse{Approved: r.approve, Always: r.always}, nil
}

const (
	mcpWrite = "mcp:fs:write_file"
	mcpRead  = "mcp:fs:read_file"
)

func mcpDefs() []llm.ToolDef {
	params := json.RawMessage(`{"type":"object","properties":{}}`)
	return []llm.ToolDef{
		{Type: "function", Function: llm.ToolFunctionDef{Name: mcpWrite, Parameters: params}, Mutating: true},
		{Type: "function", Function: llm.ToolFunctionDef{Name: mcpRead, Parameters: params}},
	}
}

// runMCPCalls runs one step of calls, each in its own step so they go through
// the serial gates one by one, and returns the tool results by id and what ran.
func runMCPCalls(t *testing.T, opts Options, calls ...llm.ToolCall) (map[string]string, *fakeMCP) {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	mcp := &fakeMCP{}
	tr.SetMCPCaller(mcp)
	opts.ExtraTools = mcpDefs()
	if opts.MaxSteps == 0 {
		opts.MaxSteps = 6
	}
	client := &callsLLM{calls: calls, results: map[string]string{}}
	ag, err := New(client, v, tr, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "save the notes"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return client.results, mcp
}

func offered(defs []llm.ToolDef, name string) bool {
	for _, d := range defs {
		if d.Function.Name == name {
			return true
		}
	}
	return false
}

// The live finding: a preview turn (apply off) called server-filesystem's
// write_file and the file appeared on disk. MCP tools run outside the staging
// overlay, and nothing asked.
func TestAgent_AnMCPToolThatCanWriteAsksBeforeEachCall(t *testing.T) {
	req := &scriptedRequester{approve: true}
	got, mcp := runMCPCalls(t, Options{Mode: ModeBuild, PermissionRequester: req},
		toolCall("w1", mcpWrite, `{"path":"notes.txt"}`),
		toolCall("w2", mcpWrite, `{"path":"more.txt"}`),
	)
	if len(req.asked) != 2 {
		t.Fatalf("the user was asked %d times for two calls, want 2", len(req.asked))
	}
	if req.asked[0].Tool != mcpWrite || req.asked[0].Kind != "mcp.tool" || !strings.Contains(req.asked[0].Description, "notes.txt") {
		t.Errorf("the request does not say what is asked for: %+v", req.asked[0])
	}
	if len(mcp.ran) != 2 {
		t.Errorf("approved calls ran %d times, want 2 (%v)", len(mcp.ran), got)
	}
}

func TestAgent_AnMCPToolTheUserRefusesDoesNotRun(t *testing.T) {
	got, mcp := runMCPCalls(t, Options{Mode: ModeBuild, PermissionRequester: &scriptedRequester{approve: false}},
		toolCall("w1", mcpWrite, `{}`))
	if len(mcp.ran) != 0 {
		t.Fatalf("a refused MCP call ran: %v", mcp.ran)
	}
	if !strings.Contains(got["w1"], "denied") {
		t.Errorf("the model was not told the call was refused:\n%s", got["w1"])
	}
}

// With no one to ask (orchestra apply, a bare agent.run) the call is refused,
// and the refusal says how to allow the tool.
func TestAgent_AnMCPToolThatCanWriteIsRefusedWithNoOneToAsk(t *testing.T) {
	got, mcp := runMCPCalls(t, Options{Mode: ModeBuild}, toolCall("w1", mcpWrite, `{}`))
	if len(mcp.ran) != 0 {
		t.Fatalf("an MCP call ran with no consent: %v", mcp.ran)
	}
	if !strings.Contains(got["w1"], "permissions rule") {
		t.Errorf("the refusal does not say how to allow the tool:\n%s", got["w1"])
	}
}

func TestAgent_APermissionRuleAllowsAnMCPToolWithoutAsking(t *testing.T) {
	_, mcp := runMCPCalls(t, Options{
		Mode:            ModeBuild,
		PermissionRules: []config.PermissionRule{{Tool: "mcp:fs:*", Action: "allow"}},
	}, toolCall("w1", mcpWrite, `{}`))
	if len(mcp.ran) != 1 {
		t.Fatalf("an allow rule did not let the MCP call run: %v", mcp.ran)
	}
}

func TestAgent_AlwaysAllowingAnMCPToolStopsTheQuestionsForIt(t *testing.T) {
	req := &scriptedRequester{approve: true, always: true}
	_, mcp := runMCPCalls(t, Options{Mode: ModeBuild, PermissionRequester: req},
		toolCall("w1", mcpWrite, `{"path":"a"}`),
		toolCall("w2", mcpWrite, `{"path":"b"}`),
	)
	if len(req.asked) != 1 || len(mcp.ran) != 2 {
		t.Errorf("asked %d times, ran %d; want asked once, ran twice", len(req.asked), len(mcp.ran))
	}
}

// The server's readOnlyHint is the word that lets a call through unasked.
func TestAgent_AReadOnlyMCPToolRunsWithoutAsking(t *testing.T) {
	req := &scriptedRequester{approve: false}
	_, mcp := runMCPCalls(t, Options{Mode: ModeAsk, PermissionRequester: req}, toolCall("r1", mcpRead, `{}`))
	if len(req.asked) != 0 || len(mcp.ran) != 1 {
		t.Errorf("a read-only MCP tool: asked %d times, ran %d; want 0 and 1", len(req.asked), len(mcp.ran))
	}
}

// A mode that only reads is not offered the MCP tools that write, and a call
// the model makes anyway is refused without putting the question to the user.
func TestAgent_AReadOnlyModeNeverRunsAnMCPToolThatCanWrite(t *testing.T) {
	for _, mode := range []Mode{ModeAsk, ModeExplore, ModePlan, ModeArchitecture, ModeVerifier} {
		ag := &Agent{opts: Options{Mode: mode, ExtraTools: mcpDefs()}}
		defs := ag.computeToolDefs()
		if offered(defs, mcpWrite) {
			t.Errorf("%s mode offers %s", mode, mcpWrite)
		}
		if !offered(defs, mcpRead) {
			t.Errorf("%s mode lost the read-only %s", mode, mcpRead)
		}

		req := &scriptedRequester{approve: true}
		got, mcp := runMCPCalls(t, Options{Mode: mode, PermissionRequester: req}, toolCall("w1", mcpWrite, `{}`))
		if len(mcp.ran) != 0 || len(req.asked) != 0 {
			t.Errorf("%s mode: ran %v, asked %d times; want neither", mode, mcp.ran, len(req.asked))
		}
		if !strings.Contains(got["w1"], "not available in "+string(mode)+" mode") {
			t.Errorf("%s mode: the refusal does not name the mode:\n%s", mode, got["w1"])
		}
	}
}

// A tool the run never offered has no readOnlyHint to trust.
func TestAgent_AnMCPToolTheRunDidNotOfferNeedsConsent(t *testing.T) {
	_, mcp := runMCPCalls(t, Options{Mode: ModeBuild}, toolCall("x1", "mcp:fs:delete_everything", `{}`))
	if len(mcp.ran) != 0 {
		t.Fatalf("an MCP tool nobody offered ran: %v", mcp.ran)
	}
}

func TestAgent_ABatchWithAnMCPToolThatCanWriteRunsThroughTheGates(t *testing.T) {
	ag := &Agent{opts: Options{Mode: ModeBuild, ExtraTools: mcpDefs()}}
	if !ag.batchNeedsSerialGates([]ToolCall{{Name: "read"}, {Name: mcpWrite}}) {
		t.Error("a batch holding an MCP tool that can write skipped the serial gates")
	}
	if ag.batchNeedsSerialGates([]ToolCall{{Name: "read"}, {Name: mcpRead}}) {
		t.Error("a read-only MCP tool alone should not force the batch serial")
	}
}
