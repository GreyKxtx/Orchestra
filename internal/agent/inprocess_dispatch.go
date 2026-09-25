package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

// inProcessCall is a gated call to a tool the agent answers itself.
type inProcessCall struct {
	id    string
	name  string
	input json.RawMessage
	step  int
}

// inProcessOutcome is what an in-process tool returns.
type inProcessOutcome struct {
	out []byte
	// err is a tool error: the model is told, and the breaker counts it.
	err error
	// reply, when set, is the tool message verbatim in place of out or err.
	reply string
	// early ends the turn here: a child's task_result, an approved plan_exit.
	early *Result
}

func outcome(out []byte, err error) inProcessOutcome { return inProcessOutcome{out: out, err: err} }

// inProcessTool is one entry of the table the dispatcher consults before
// tools.Runner. needs, when set, is what the tool requires of this run; a call
// without it goes on to the Runner, which does not know the tool.
type inProcessTool struct {
	needs func(a *Agent) bool
	run   func(a *Agent, ctx context.Context, c inProcessCall) inProcessOutcome
}

func hasSubtasks(a *Agent) bool { return a.opts.SubtaskRunner != nil }

func runTaskTool(a *Agent, ctx context.Context, c inProcessCall) inProcessOutcome {
	return outcome(a.handleTaskTool(ctx, c.name, c.id, c.input))
}

func runTodoTool(a *Agent, _ context.Context, c inProcessCall) inProcessOutcome {
	out, err := a.handleTodoTool(c.name, c.input)
	if err == nil && c.name == "todowrite" && a.opts.OnEvent != nil {
		payload, _ := json.Marshal(a.todos)
		a.opts.OnEvent(AgentEvent{Step: c.step, Stream: llm.StreamEvent{
			Kind:    llm.StreamEventTodosUpdated,
			Content: string(payload),
		}})
	}
	return outcome(out, err)
}

// inProcessTools are the tools the agent answers itself: session state,
// delegation, skills, questions and plan mode (toolspec.Spec.InProcess). They
// were a chain of `if name == …` blocks in the serial dispatcher, each with its
// own copy of the logging, history and breaker bookkeeping.
var inProcessTools = map[string]inProcessTool{
	"task_result": {run: (*Agent).runTaskResult},
	"skill_invoke": {
		needs: func(a *Agent) bool { return a.opts.SkillRunner != nil },
		run: func(a *Agent, ctx context.Context, c inProcessCall) inProcessOutcome {
			return outcome(a.handleSkillInvoke(ctx, c.input))
		},
	},
	"task":         {needs: hasSubtasks, run: runTaskTool},
	"task_spawn":   {needs: hasSubtasks, run: runTaskTool},
	"task_wait":    {needs: hasSubtasks, run: runTaskTool},
	"task_cancel":  {needs: hasSubtasks, run: runTaskTool},
	"send_message": {needs: hasSubtasks, run: runTaskTool},
	"agent_post":   {needs: hasSubtasks, run: runTaskTool},
	"task_board":   {needs: hasSubtasks, run: runTaskTool},
	"todowrite":    {run: runTodoTool},
	"todoread":     {run: runTodoTool},
	"contract_freeze": {run: func(a *Agent, ctx context.Context, _ inProcessCall) inProcessOutcome {
		return outcome(a.handleContractFreeze(ctx))
	}},
	"lesson_promote": {run: func(a *Agent, ctx context.Context, c inProcessCall) inProcessOutcome {
		return outcome(a.handleLessonPromote(ctx, c.input))
	}},
	"playbook_promote": {run: func(a *Agent, ctx context.Context, c inProcessCall) inProcessOutcome {
		return outcome(a.handlePlaybookPromote(ctx, c.input))
	}},
	"update_working_state": {run: func(a *Agent, _ context.Context, c inProcessCall) inProcessOutcome {
		return outcome(a.handleUpdateWorkingState(c.input))
	}},
	"question":  {run: (*Agent).runQuestion},
	"plan_exit": {run: (*Agent).runPlanExit},
	"plan_enter": {run: func(*Agent, context.Context, inProcessCall) inProcessOutcome {
		return inProcessOutcome{reply: `{"status":"not_supported","message":"plan_enter does not switch modes; this turn stays in its current mode. Planning runs in plan mode (orchestra apply --mode plan)."}`}
	}},
}

// inProcessHandler returns the handler for name when this run can serve it.
func (a *Agent) inProcessHandler(name string) (inProcessTool, bool) {
	t, found := inProcessTools[name]
	if !found || (t.needs != nil && !t.needs(a)) {
		return inProcessTool{}, false
	}
	return t, true
}

// runInProcessTool runs a gated in-process call and does its bookkeeping.
//
// These tools bypass tools.Runner, so the Runner's llm_log entries are
// mirrored here — otherwise a delegated subtask, a checklist or a skill is
// indistinguishable, in the log, from work the parent did inline, and the
// eval's tool_used check, which reads these entries, could never pass.
func (a *Agent) runInProcessTool(ctx context.Context, cb *CircuitBreaker, history *[]llm.Message, c inProcessCall, t inProcessTool, emitStepDone func(string)) (serialToolOutcome, error) {
	if a.opts.AgentLogger != nil {
		a.opts.AgentLogger.LogToolCall(c.name, len(c.input), string(c.input))
	}
	start := time.Now()
	res := t.run(a, ctx, c)
	content := res.reply
	switch {
	case res.early != nil:
		content = res.early.SubtaskResult
	case content == "" && res.err != nil:
		content = formatToolErrorJSON(c.name, c.input, res.err)
	case content == "":
		content = string(res.out)
	}
	if a.opts.AgentLogger != nil {
		a.opts.AgentLogger.LogToolResult(c.name, len(content), time.Since(start).Milliseconds(), errText(res.err), content)
	}
	if res.early != nil {
		emitStepDone("final")
		return serialToolOutcome{EarlyResult: res.early}, nil
	}
	a.observeWorkingTool(c.name, c.input, res.out, res.err)
	// A child's result is untrusted when the child read untrusted text.
	if source := untrustedSource(c.name, res.out); source != "" {
		a.markTainted(source)
		content = spotlight(source, content)
	}
	*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: c.id, Content: content})
	if res.err != nil {
		if cbErr := cb.RecordToolErrorDetail(c.name, res.err); cbErr != nil {
			return serialToolOutcome{}, cbErr
		}
		return serialToolOutcome{}, nil
	}
	cb.ResetToolErrors()
	return serialToolOutcome{}, nil
}

// runTaskResult ends a child's turn with its result.
func (a *Agent) runTaskResult(_ context.Context, c inProcessCall) inProcessOutcome {
	if !a.opts.IsChild {
		return inProcessOutcome{err: fmt.Errorf("task_result is only valid in subtask / skill_invoke child agents; main agents must emit a normal final response with patches")}
	}
	var req struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(c.input, &req)
	// An empty content ends the child with nothing to say, and an empty
	// answer is read downstream as a successful one: workerTaskResultSuccess
	// treats an absent status as success, so the Lead is handed
	// {"status":"verified_success","worker_result":""} — told the job is
	// done and given no account of it. The common way to get here is a
	// model guessing the argument name ({"result":...}, {"summary":...}):
	// the unmarshal above ignores the mismatch and leaves Content blank.
	// Ask for the answer again instead of finishing without one.
	if strings.TrimSpace(req.Content) == "" {
		return inProcessOutcome{err: fmt.Errorf(
			"task_result needs its content argument: the result goes in \"content\" " +
				"as a string, and every other key is ignored. Resend task_result " +
				"with {\"content\": \"<your result>\"}")}
	}
	for _, check := range []func(string) error{a.checkWorkerResultSchema, checkBlockedReasonTaxonomy, a.blockWorkerTaskResult} {
		if err := check(req.Content); err != nil {
			return inProcessOutcome{err: err}
		}
	}
	return inProcessOutcome{early: &Result{Steps: c.step, SubtaskResult: req.Content, Todos: a.todos}}
}

var errQuestionUnavailable = errors.New("question tool unavailable")

// runQuestion puts the model's questions to the user.
func (a *Agent) runQuestion(ctx context.Context, c inProcessCall) inProcessOutcome {
	if a.opts.QuestionAsker == nil {
		return inProcessOutcome{reply: `{"error":"question tool unavailable"}`, err: errQuestionUnavailable}
	}
	var req struct {
		Questions []tools.QuestionItem `json:"questions"`
	}
	if err := json.Unmarshal(c.input, &req); err != nil {
		return inProcessOutcome{err: err}
	}
	// A model that flattens the argument — {"question": "..."} instead of
	// {"questions": [{"question": "..."}]} — unmarshals cleanly into an
	// empty slice. That used to reach the asker as a prompt with no question
	// in it and come back to the model as {"answers":[]}: the user, it seemed,
	// had nothing to say. Refuse instead, and say which shape is wanted.
	if len(req.Questions) == 0 {
		return inProcessOutcome{err: fmt.Errorf(`no questions to ask: the argument is {"questions": [{"question": "...", "options": ["..."]}]} — an array under "questions", even for one question`)}
	}
	for _, item := range req.Questions {
		if strings.TrimSpace(item.Question) == "" {
			return inProcessOutcome{err: fmt.Errorf(`every entry in "questions" needs a non-empty "question"`)}
		}
	}
	answers, err := a.opts.QuestionAsker.Ask(ctx, req.Questions)
	if err != nil {
		return inProcessOutcome{err: err}
	}
	a.logQuestionAnswers(req.Questions, answers)
	b, _ := json.Marshal(map[string]any{"answers": answers})
	return inProcessOutcome{out: b}
}

// runPlanExit asks the user whether to leave plan mode for build.
func (a *Agent) runPlanExit(ctx context.Context, c inProcessCall) inProcessOutcome {
	if a.opts.QuestionAsker == nil {
		return inProcessOutcome{reply: `{"status":"refused","message":"plan_exit is unavailable in non-interactive mode. Finish with a final answer — the user will switch modes manually if needed."}`}
	}
	answers, err := a.opts.QuestionAsker.Ask(ctx, []tools.QuestionItem{{
		Question: "Plan complete. Switch to build mode to apply changes?",
		Options:  []string{"Yes, switch to build", "No, keep planning"},
	}})
	if err == nil && len(answers) > 0 {
		ans := strings.ToLower(strings.TrimSpace(answers[0]))
		if ans == "1" || ans == "yes" || ans == "y" || strings.HasPrefix(ans, "yes,") || ans == "да" || strings.HasPrefix(ans, "да,") {
			return inProcessOutcome{early: &Result{Steps: c.step, SwitchToBuild: true, Todos: a.todos}}
		}
	}
	return inProcessOutcome{reply: `{"status":"continue","message":"Continue planning. Refine the plan and call plan_exit again when ready."}`}
}
