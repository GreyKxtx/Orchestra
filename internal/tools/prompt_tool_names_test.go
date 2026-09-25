package tools

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/roles"
	"github.com/orchestra/orchestra/internal/toolspec"
)

// promptModes are the agent modes a turn can actually run in — every mode in
// the registry but the runtime's internal ones. Each has a system prompt and a
// tool list, and the two have to agree.
func promptModes() []string {
	var out []string
	for _, spec := range roles.All() {
		if spec.Kind != roles.Internal {
			out = append(out, spec.Name)
		}
	}
	return out
}

// checkableToolNames are every built-in tool name, the agency tools and
// skill_invoke included, less those that are also ordinary English words.
func checkableToolNames() []string {
	var out []string
	for _, spec := range toolspec.All() {
		if !ambiguousToolNames[spec.Name] {
			out = append(out, spec.Name)
		}
	}
	sort.Strings(out)
	return out
}

// offeredInMode is everything a turn in mode can be given: its list with every
// capability, plus what the runtime adds — the agency tools whenever the
// agency is on (any mode can have flows), and skill_invoke in a mode that may
// change files.
func offeredInMode(mode string, caps Capabilities) map[string]bool {
	offered := map[string]bool{}
	for _, d := range ListToolsForMode(mode, caps, true, true) {
		offered[d.Function.Name] = true
	}
	for _, name := range []string{"send_message", "agent_post", "task_board"} {
		offered[name] = true
	}
	if spec, _ := roles.Lookup(mode); !spec.ReadOnly() && !spec.Tools.Lead {
		offered["skill_invoke"] = true
	}
	return offered
}

var promptFamilies = []string{"", "local", "gpt", "anthropic", "gemini", "kimi"}

// ambiguousToolNames are tool names that are also ordinary English words. A
// prompt saying "read the file" is not advertising the read tool, so mentions
// of these cannot be checked mechanically.
var ambiguousToolNames = map[string]bool{
	"read": true, "write": true, "edit": true, "grep": true, "ls": true,
	"glob": true, "explore": true, "symbols": true, "question": true,
	"bash": true, "task": true,

	// task_result is never in a mode list because it only makes sense to a
	// child: tasks.childToolsForSubagent appends it to whatever the mode
	// returns. The subagent prompts that name it all say "when invoked as a
	// child", which is exactly right.
	"task_result": true,
}

// A tool name mentioned in a prompt but absent from that mode's schema is an
// invitation to hallucinate: the model is told the tool exists, calls it, and
// gets "unknown tool" — which on a small model costs the whole turn. This
// caught build-local.txt advertising semantic_search, which is only ever
// offered when an embedding model is configured.
func TestPromptsOnlyNameToolsTheModeActuallyOffers(t *testing.T) {
	checkable := checkableToolNames()

	// Every capability on, and both optional surfaces present: the check is
	// about names the mode can never produce, not about gated ones.
	caps := Capabilities{Exec: true, Web: true, Browser: true}

	for _, mode := range promptModes() {
		offered := offeredInMode(mode, caps)
		for _, family := range promptFamilies {
			text := prompt.BuildSystemPromptForMode(mode, family)
			for _, name := range checkable {
				if offered[name] || !mentionsTool(text, name) {
					continue
				}
				t.Errorf("prompt for mode %q family %q names tool %q, which %q never offers",
					mode, orDefault(family), name, mode)
			}
		}
	}
}

// Tool descriptions are part of the same prompt and have the same problem:
// one tool telling the model to reach for another that is not in the schema.
func TestToolDescriptionsOnlyNameToolsTheModeAlsoOffers(t *testing.T) {
	checkable := checkableToolNames()

	caps := Capabilities{Exec: true, Web: true, Browser: true}
	for _, mode := range promptModes() {
		defs := ListToolsForMode(mode, caps, true, true)
		defs = append(defs, ToolSendMessage(), ToolAgentPost(), ToolTaskBoard())
		offered := offeredInMode(mode, caps)
		for _, d := range defs {
			for _, name := range checkable {
				if offered[name] || !mentionsTool(d.Function.Description, name) {
					continue
				}
				t.Errorf("in mode %q the description of %q points at %q, which %q never offers",
					mode, d.Function.Name, name, mode)
			}
		}
	}
}

var toolMentionCache = map[string]*regexp.Regexp{}

// mentionsTool reports whether text names the tool as a word rather than as
// part of a longer identifier.
func mentionsTool(text, name string) bool {
	re, ok := toolMentionCache[name]
	if !ok {
		re = regexp.MustCompile(`(^|[^A-Za-z0-9_.])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9_.])`)
		toolMentionCache[name] = re
	}
	return re.MatchString(text)
}

func orDefault(family string) string {
	if strings.TrimSpace(family) == "" {
		return "default"
	}
	return family
}
