package core

import (
	"context"
	"strings"
	"testing"
)

func TestMCPPromptCommand_Name(t *testing.T) {
	// The palette entry a user types. Server and prompt are both in the name
	// because two servers may offer a prompt with the same name.
	got := MCPPromptCommand{Server: "linear", Name: "triage"}.Command()
	if got != "/mcp:linear:triage" {
		t.Errorf("Command() = %q", got)
	}
}

func TestMCPPromptCommand_Describe(t *testing.T) {
	c := MCPPromptCommand{
		Server: "linear", Name: "triage", Description: "Triage an issue",
		Arguments: []MCPPromptArgView{{Name: "id", Required: true}, {Name: "note"}},
	}
	got := c.Describe()
	for _, want := range []string{"Triage an issue", "<id>", "[note]"} {
		if !strings.Contains(got, want) {
			t.Errorf("Describe() = %q, missing %q", got, want)
		}
	}
}

func TestMCPPromptCommand_DescribeWithoutArguments(t *testing.T) {
	c := MCPPromptCommand{Server: "s", Name: "n", Description: "just do it"}
	if got := c.Describe(); got != "just do it" {
		t.Errorf("Describe() = %q, want the bare description", got)
	}
}

func TestMCPPromptGet_NoManagerIsAClearError(t *testing.T) {
	// A core started without MCP servers must answer the RPC, not panic.
	c := &Core{}
	_, err := c.MCPPromptGet(context.Background(), MCPPromptGetParams{Server: "x", Name: "y"})
	if err == nil || !strings.Contains(err.Error(), "MCP") {
		t.Fatalf("err = %v, want it to say no MCP servers are running", err)
	}
}

func TestMCPPromptList_NoManagerIsEmptyNotAnError(t *testing.T) {
	c := &Core{}
	res, err := c.MCPPromptList(context.Background(), MCPPromptListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Prompts) != 0 {
		t.Errorf("prompts = %+v, want none", res.Prompts)
	}
}

func TestParsePromptArgs(t *testing.T) {
	spec := []MCPPromptArgView{{Name: "id", Required: true}, {Name: "note"}}

	// The first word fills the first argument; the rest fill the next one,
	// so "/mcp:linear:triage ENG-1 looks flaky" works the way a person types.
	got, err := parsePromptArgs(spec, "ENG-1 looks flaky")
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "ENG-1" || got["note"] != "looks flaky" {
		t.Errorf("args = %v", got)
	}

	// A single-argument prompt takes the whole line, spaces and all.
	got, err = parsePromptArgs([]MCPPromptArgView{{Name: "q", Required: true}}, "what broke in CI today")
	if err != nil {
		t.Fatal(err)
	}
	if got["q"] != "what broke in CI today" {
		t.Errorf("args = %v", got)
	}

	// A missing required argument is named, not silently sent empty.
	if _, err := parsePromptArgs(spec, ""); err == nil || !strings.Contains(err.Error(), "id") {
		t.Errorf("err = %v, want it to name the missing argument", err)
	}

	// A prompt with no arguments ignores whatever was typed after it.
	if got, err := parsePromptArgs(nil, "anything"); err != nil || len(got) != 0 {
		t.Errorf("args = %v err = %v", got, err)
	}
}

func TestMCPPromptList_ShipsTheRenderedCommandAndHint(t *testing.T) {
	// The palette must not re-derive the command string or the argument
	// shape: two implementations of the same formatting drift.
	cmd := MCPPromptCommand{Server: "linear", Name: "triage", Description: "Triage",
		Arguments: []MCPPromptArgView{{Name: "id", Required: true}}}
	cmd.fill()
	if cmd.Slash != "/mcp:linear:triage" {
		t.Errorf("Slash = %q", cmd.Slash)
	}
	if !strings.Contains(cmd.Hint, "<id>") {
		t.Errorf("Hint = %q", cmd.Hint)
	}
}
