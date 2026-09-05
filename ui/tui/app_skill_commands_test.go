package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

func TestParseSkillSlashCommand(t *testing.T) {
	names := []string{"refactor-go", "review"}
	cases := []struct {
		in         string
		name, args string
		ok         bool
	}{
		{"/refactor-go clean up foo.go", "refactor-go", "clean up foo.go", true},
		{"  /refactor-go  clean up  ", "refactor-go", "clean up", true},
		{"/review", "", "", false},               // skill.invoke requires arguments
		{"/unknown-skill do it", "", "", false},   // not a loaded skill
		{"/help do it", "", "", false},            // "help" is a built-in, never a skill route
		{"just a message", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		name, args, ok := parseSkillSlashCommand(tc.in, names)
		if ok != tc.ok || name != tc.name || args != tc.args {
			t.Errorf("parseSkillSlashCommand(%q) = %q/%q/%v, want %q/%q/%v",
				tc.in, name, args, ok, tc.name, tc.args, tc.ok)
		}
	}
}

func TestIsBuiltinSlashCmd(t *testing.T) {
	if !isBuiltinSlashCmd("help") {
		t.Error(`"help" must be recognised as a built-in`)
	}
	if !isBuiltinSlashCmd("skill") {
		t.Error(`"skill" must be recognised as a built-in`)
	}
	if isBuiltinSlashCmd("refactor-go") {
		t.Error(`"refactor-go" must not be a built-in`)
	}
}

// A loaded skill becomes its own slash command: typing "/name args" and
// pressing Enter must invoke skill.invoke directly, the same way
// "/skill name args" already does, without the user needing the "/skill "
// prefix.
func TestSkillSlashCommand_TypedWithArgsRunsSkillInvoke(t *testing.T) {
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1"
	a.skillNames = []string{"refactor-go"}
	f.skillInvokeResult = &rpcclient.SkillInvokeResult{Skill: "refactor-go", Output: "done", Steps: 1}

	a.input.SetValue("/refactor-go clean up foo.go")
	_, cmd, handled := a.handleEnter()
	if !handled {
		t.Fatal("Enter on a skill command must be handled")
	}
	if cmd == nil {
		t.Fatal("expected a command to run skill.invoke")
	}
	execCmdTree(cmd)

	if f.skillInvokeName != "refactor-go" {
		t.Errorf("skill invoked = %q, want refactor-go", f.skillInvokeName)
	}
	if f.skillInvokeArgs != "clean up foo.go" {
		t.Errorf("args = %q", f.skillInvokeArgs)
	}
}

// A name that is not a loaded skill falls through to an ordinary chat
// message — unrecognised text must reach the model, not silently vanish.
func TestSkillSlashCommand_UnknownNameFallsThroughToChat(t *testing.T) {
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1"
	a.skillNames = []string{"refactor-go"}

	a.input.SetValue("/not-a-skill hello there")
	_, cmd, handled := a.handleEnter()
	if !handled {
		t.Fatal("Enter must always be handled")
	}
	execCmdTree(cmd)

	if f.skillInvokeName != "" {
		t.Errorf("skill.invoke must not run for an unknown name, got %q", f.skillInvokeName)
	}
	msgs := f.recordedSessionMessages()
	if len(msgs) != 1 || msgs[0].Query != "/not-a-skill hello there" {
		t.Errorf("expected the text to be sent as a chat message, got %+v", msgs)
	}
}

// handleSkillsLoadedMsg is what turns a skill.list result into both the
// name list parseSkillSlashCommand matches against and the palette rows a
// user sees while typing "/".
func TestHandleSkillsLoadedMsg_PopulatesNamesAndPaletteExcludingBuiltins(t *testing.T) {
	a := testChromeApp(t)
	a.handleSkillsLoadedMsg(skillsLoadedMsg{skills: []rpcclient.SkillSummary{
		{Name: "refactor-go", Description: "Refactor Go code"},
		{Name: "skill", Description: "collides with the built-in /skill"},
	}})

	if len(a.skillNames) != 1 || a.skillNames[0] != "refactor-go" {
		t.Fatalf("skillNames = %v, want just [refactor-go]", a.skillNames)
	}
	a.slashPalette.Filter("refactor")
	if len(a.slashPalette.Items) != 1 || a.slashPalette.Items[0].Cmd != "/refactor-go" {
		t.Fatalf("palette items = %+v", a.slashPalette.Items)
	}
}
