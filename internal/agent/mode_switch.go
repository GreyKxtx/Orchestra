package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/orchestra/orchestra/internal/roles"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// Every mode can ask to become another. A mode is the user's promise about
// what a turn may do — Ask only answers, Plan only plans — so the switch is
// theirs to make: the model asks with mode_switch, or calls the tool it needs
// and is refused, and the agent puts the question to the user. On yes the turn
// ends here asking for the new mode (Result.SwitchToMode) and its caller goes
// on in that mode with the same conversation (ContinueInSwitchedMode); on no
// the model is told to stay, and the turn is not asked again.

// offersModeSwitch: a top-level turn with someone to ask. A child's mode is
// its caller's choice, and a custom agent's tool list is its author's.
func (a *Agent) offersModeSwitch() bool {
	return !a.opts.IsChild && a.opts.QuestionAsker != nil && len(a.opts.CustomTools) == 0
}

// runModeSwitch is the mode_switch tool.
func (a *Agent) runModeSwitch(ctx context.Context, c inProcessCall) inProcessOutcome {
	if !a.offersModeSwitch() {
		return inProcessOutcome{err: fmt.Errorf("mode_switch is unavailable here: finish with a final answer, and the user switches modes themselves")}
	}
	var req struct {
		Mode   string `json:"mode"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(c.input, &req); err != nil {
		return inProcessOutcome{err: err}
	}
	target := Mode(strings.ToLower(strings.TrimSpace(req.Mode)))
	if !slices.Contains(tools.ModeSwitchTargets, string(target)) {
		return inProcessOutcome{err: fmt.Errorf("mode_switch: %q is not a mode to switch to; one of %s", req.Mode, strings.Join(tools.ModeSwitchTargets, ", "))}
	}
	if target == a.currentMode() {
		return inProcessOutcome{err: fmt.Errorf("this turn is already in %s mode", target)}
	}
	return a.askModeSwitch(ctx, c, target, strings.TrimSpace(req.Reason))
}

// askModeSwitch puts the switch to the user and answers the call that led to
// it: on yes the turn ends asking for target, on no the model stays.
func (a *Agent) askModeSwitch(ctx context.Context, c inProcessCall, target Mode, reason string) inProcessOutcome {
	from := a.currentMode()
	q := modeSwitchQuestion(from, target, reason, a.userSpeaksRussian)
	answers, err := a.opts.QuestionAsker.Ask(ctx, []tools.QuestionItem{q})
	if err != nil {
		return inProcessOutcome{err: err}
	}
	if len(answers) > 0 && isYesAnswer(answers[0], q.Options[0]) {
		reply := fmt.Sprintf(`{"status":"switched","mode":%q,"message":"The user switched this turn to %s mode. Go on with the task in it; repeat the call %s mode refused if it is still needed."}`, target, target, from)
		return inProcessOutcome{
			reply: reply,
			early: &Result{Steps: c.step, SwitchToMode: target, Todos: a.todos},
		}
	}
	a.modeSwitchDeclined = true
	return inProcessOutcome{reply: fmt.Sprintf(`{"status":"declined","message":"The user declined switching to %s mode. Stay in %s mode: do what it allows, and say in your answer what is left for the user to do."}`, target, from)}
}

// modeSwitchTarget is the mode to offer when a call is refused for this
// mode's sake — a tool the mode does not offer, a write outside what it may
// write — and build would take it. "" when the refusal is not the mode's
// (consent, a runtime-owned path) or there is nobody to ask.
func (a *Agent) modeSwitchTarget(name string, input json.RawMessage) Mode {
	if !a.offersModeSwitch() || a.modeSwitchDeclined {
		return ""
	}
	if spec := a.modeSpec(); spec.Write == roles.WriteAny && a.offersTool(name) {
		return "" // build already: the refusal is about something else
	}
	if !buildOffers(name, a.opts.AllowExec) {
		return ""
	}
	if a.runtimeOwnedCallRefusal(name, input) != nil {
		return "" // build refuses these too
	}
	if a.offeredToolRefusal(name) != nil || a.writeScopeRefusal(name, input) != nil {
		return ModeBuild
	}
	return ""
}

// buildOffers reports whether build mode would offer name.
func buildOffers(name string, allowExec bool) bool {
	if name == "bash" {
		return allowExec
	}
	spec, ok := roles.Lookup(string(ModeBuild))
	return ok && slices.Contains(spec.Tools.Names, name)
}

// switchForRefusedCall asks the user to switch when the call was refused for
// the mode's sake. handled reports that the call was answered here.
func (a *Agent) switchForRefusedCall(ctx context.Context, history *[]llm.Message, tc ToolCall, name, toolCallID string, steps int, reason string) (serialToolOutcome, bool) {
	target := a.modeSwitchTarget(name, tc.Input)
	if target == "" {
		return serialToolOutcome{}, false
	}
	why := fmt.Sprintf("%s: %s", name, reason)
	if a.userSpeaksRussian {
		why = fmt.Sprintf("нужен инструмент %s (%s)", name, reason)
	}
	c := inProcessCall{id: toolCallID, name: name, input: tc.Input, step: steps}
	res := a.askModeSwitch(ctx, c, target, why)
	content := res.reply
	if res.err != nil {
		content = formatToolErrorJSON(name, tc.Input, res.err)
	}
	if a.opts.OnEvent != nil {
		a.opts.OnEvent(AgentEvent{Step: steps, Stream: toolCallCompletedStreamEvent(name, toolCallID, []byte(content), res.err)})
	}
	*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: content})
	if res.early != nil {
		return serialToolOutcome{EarlyResult: res.early}, true
	}
	return serialToolOutcome{}, true
}

func (a *Agent) currentMode() Mode {
	if a.opts.Mode == "" {
		return ModeBuild
	}
	return a.opts.Mode
}

func modeSwitchQuestion(from, to Mode, reason string, russian bool) tools.QuestionItem {
	if russian {
		q := fmt.Sprintf("Агенту нужен режим %s (сейчас %s). Переключиться?", modeTitle(to), modeTitle(from))
		if reason != "" {
			q = fmt.Sprintf("Агенту нужен режим %s (сейчас %s): %s. Переключиться?", modeTitle(to), modeTitle(from), reason)
		}
		return tools.QuestionItem{Question: q, Options: []string{
			"Да, переключить в " + modeTitle(to),
			"Нет, остаться в " + modeTitle(from),
		}}
	}
	q := fmt.Sprintf("The agent needs %s mode (now %s). Switch?", modeTitle(to), modeTitle(from))
	if reason != "" {
		q = fmt.Sprintf("The agent needs %s mode (now %s): %s. Switch?", modeTitle(to), modeTitle(from), reason)
	}
	return tools.QuestionItem{Question: q, Options: []string{
		"Yes, switch to " + modeTitle(to),
		"No, stay in " + modeTitle(from),
	}}
}

func modeTitle(m Mode) string {
	s := string(m)
	if s == "" {
		return "Build"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// isYesAnswer reads the user's answer to a yes/no switch: the yes option, its
// number, or a yes in English or Russian.
func isYesAnswer(answer, yesOption string) bool {
	ans := strings.ToLower(strings.TrimSpace(answer))
	if ans == "" {
		return false
	}
	if ans == strings.ToLower(yesOption) || ans == "1" {
		return true
	}
	for _, yes := range []string{"yes", "y", "да"} {
		if ans == yes || strings.HasPrefix(ans, yes+",") || strings.HasPrefix(ans, yes+" ") {
			return true
		}
	}
	return false
}

// speaksRussian: the user's own words — the query, or the latest user message
// before it — are in Cyrillic. The switch question is put in their language.
func speaksRussian(userQuery string, history []llm.Message) bool {
	if hasCyrillic(userQuery) {
		return true
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleUser {
			return hasCyrillic(history[i].Content)
		}
	}
	return false
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// modeSwitchedQuery is the turn's message for the half that runs in the new
// mode: the conversation above holds the task.
func modeSwitchedQuery(from, to Mode) string {
	if from == ModePlan && to == ModeBuild {
		return planApprovedQuery
	}
	return fmt.Sprintf("The user switched this turn from %s to %s mode. Go on with the task in the conversation above, from where %s mode stopped.", from, to, from)
}

// maxModeSwitches bounds one turn's switches: each needs a yes, but a model
// that keeps asking must not keep the turn alive forever.
const maxModeSwitches = 3

// ContinueInSwitchedMode runs the rest of a turn in the mode the user switched
// it to (Result.SwitchToMode), and again if that half switches in its turn,
// merging every half into one result. onSwitch, when set, hears each switch.
func ContinueInSwitchedMode(
	ctx context.Context,
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	opts Options,
	history []llm.Message,
	res *Result,
	onSwitch ...func(from, to Mode),
) ([]llm.Message, *Result, error) {
	from := opts.Mode
	if from == "" {
		from = ModeBuild
	}
	for n := 0; res != nil && res.SwitchToMode != "" && n < maxModeSwitches; n++ {
		to := res.SwitchToMode
		for _, f := range onSwitch {
			f(from, to)
		}
		next := opts
		next.Mode = to
		next.JustSwitchedFromPlan = from == ModePlan && to == ModeBuild
		// Carry todos written so far; otherwise InitialTodos stays at the
		// pre-turn snapshot and the merge can wipe the checklist.
		if len(res.Todos) > 0 {
			next.InitialTodos = append([]tools.TodoItem(nil), res.Todos...)
		}
		ag, err := New(llmClient, validator, toolRunner, next)
		if err != nil {
			return history, res, err
		}
		outHist, nextRes, err := ag.Run(ctx, history, modeSwitchedQuery(from, to))
		if err != nil {
			return history, res, err
		}
		history = outHist
		pending := Mode("")
		if nextRes != nil {
			pending = nextRes.SwitchToMode
		}
		res = mergeAgentResults(res, nextRes)
		res.SwitchToMode = pending
		from = to
	}
	if res != nil {
		res.SwitchToMode = ""
	}
	return history, res, nil
}
