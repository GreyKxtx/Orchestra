package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A command file carries its own instructions; the argument is what the person
// typed after the name, and most commands are run with nothing after the name
// (".claude/commands/list-files.md" and friends). Refusing an empty argument
// made every one of those unusable from every client.
func TestSkillInvoke_EmptyArgumentsAreAllowed(t *testing.T) {
	c := newGraphTestCore(t)
	dir := filepath.Join(c.workspaceRoot, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ndescription: List the files\n---\n\nList every file.\n"
	if err := os.WriteFile(filepath.Join(dir, "list-files.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// An unknown name still fails as not found — the guard being gone must not
	// mean the call stops checking anything.
	_, err := c.SkillInvoke(context.Background(), SkillInvokeParams{Name: "nope", Arguments: ""})
	if err == nil {
		t.Fatal("an unknown command must still be refused")
	}
	if strings.Contains(err.Error(), "arguments are empty") {
		t.Fatalf("an empty argument must not be what fails: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error for an unknown command: %v", err)
	}

	// The command itself gets as far as the model: what fails is the LLM call
	// (this core points at a dead endpoint), not a missing argument.
	if _, err := c.SkillInvoke(context.Background(), SkillInvokeParams{Name: "list-files", Arguments: ""}); err == nil {
		t.Fatal("the dead test endpoint must fail the run")
	} else if strings.Contains(err.Error(), "query is empty") {
		t.Fatalf("a command with no argument must still reach the model: %v", err)
	}

	// A name that is empty is still empty.
	if _, err := c.SkillInvoke(context.Background(), SkillInvokeParams{Name: "  ", Arguments: "x"}); err == nil ||
		!strings.Contains(err.Error(), "name is empty") {
		t.Fatalf("an empty name must still be refused: %v", err)
	}
}
