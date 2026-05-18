package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
)

// maybeRunSkillOrWorkflow inspects the submitted text for a /workflow* or
// /skill* prefix and, when matched, kicks off the corresponding RPC call.
// Returns nil when the text is a regular chat message (caller falls through
// to the agent.run path).
//
// Recognised forms:
//
//	/workflows                — list available workflows (sync, prints)
//	/skills                   — list available skills (sync, prints)
//	/workflow <name> <args…>  — run workflow.run; stage events stream into chat
//	/skill <name> <args…>     — invoke a single skill via skill.invoke
func (a *App) maybeRunSkillOrWorkflow(text string) tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	trimmed := strings.TrimSpace(text)

	switch {
	case trimmed == "/workflows":
		return a.cmdListWorkflows()
	case trimmed == "/skills":
		return a.cmdListSkills()
	case strings.HasPrefix(trimmed, "/workflow "):
		name, args, ok := splitNameAndArgs(strings.TrimPrefix(trimmed, "/workflow "))
		if !ok {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem,
				Text: "usage: /workflow <name> <arguments…>\nRun `/workflows` to see available names."})
			return nil
		}
		return a.cmdRunWorkflow(name, args)
	case strings.HasPrefix(trimmed, "/skill "):
		name, args, ok := splitNameAndArgs(strings.TrimPrefix(trimmed, "/skill "))
		if !ok {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem,
				Text: "usage: /skill <name> <arguments…>\nRun `/skills` to see available names."})
			return nil
		}
		return a.cmdInvokeSkill(name, args)
	}
	return nil
}

// splitNameAndArgs takes the text after "/workflow " (or "/skill ") and
// returns the first token as `name`, the rest joined as `args`. ok=false
// when the name is empty or when there are no arguments (skills/workflows
// require both).
func splitNameAndArgs(s string) (name, args string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	parts := strings.SplitN(s, " ", 2)
	name = strings.TrimSpace(parts[0])
	if name == "" {
		return "", "", false
	}
	if len(parts) < 2 {
		return name, "", false
	}
	args = strings.TrimSpace(parts[1])
	if args == "" {
		return name, "", false
	}
	return name, args, true
}

// workflowResultMsg is the tea.Msg produced when workflow.run completes.
type workflowResultMsg struct {
	res *rpcclient.WorkflowRunResult
	err error
}

// skillResultMsg is the tea.Msg produced when skill.invoke completes.
type skillResultMsg struct {
	res *rpcclient.SkillInvokeResult
	err error
}

func (a *App) cmdListWorkflows() tea.Cmd {
	rpc := a.rpc
	return func() tea.Msg {
		ws, err := rpc.WorkflowList(context.Background())
		if err != nil {
			return systemMsgMsg{text: "[error] workflow.list: " + err.Error()}
		}
		if len(ws) == 0 {
			return systemMsgMsg{text: "no workflows found — add YAMLs to .orchestra/workflows/ or ~/.orchestra/workflows/"}
		}
		var b strings.Builder
		b.WriteString("Workflows:\n")
		for _, w := range ws {
			b.WriteString(fmt.Sprintf("  /workflow %-20s — %s\n", w.Name, w.Description))
		}
		return systemMsgMsg{text: strings.TrimRight(b.String(), "\n")}
	}
}

func (a *App) cmdListSkills() tea.Cmd {
	rpc := a.rpc
	return func() tea.Msg {
		ss, err := rpc.SkillList(context.Background())
		if err != nil {
			return systemMsgMsg{text: "[error] skill.list: " + err.Error()}
		}
		if len(ss) == 0 {
			return systemMsgMsg{text: "no skills found — add MDs to .orchestra/skills/ or ~/.orchestra/skills/"}
		}
		var b strings.Builder
		b.WriteString("Skills:\n")
		for _, s := range ss {
			b.WriteString(fmt.Sprintf("  /skill %-20s — %s\n", s.Name, s.Description))
		}
		return systemMsgMsg{text: strings.TrimRight(b.String(), "\n")}
	}
}

func (a *App) cmdRunWorkflow(name, args string) tea.Cmd {
	rpc := a.rpc
	a.agentBusy = true
	a.statusBar.SetAgentBusy(true)
	a.chat.SetAgentBusy(true)
	a.layout()
	return func() tea.Msg {
		res, err := rpc.WorkflowRun(context.Background(), name, args, rpcclient.WorkflowRunOptions{
			// TODO: thread --apply / --allow-exec from TUI settings once we add
			// those toggles. Default: dry-run + no exec.
		})
		return workflowResultMsg{res: res, err: err}
	}
}

func (a *App) cmdInvokeSkill(name, args string) tea.Cmd {
	rpc := a.rpc
	a.agentBusy = true
	a.statusBar.SetAgentBusy(true)
	a.chat.SetAgentBusy(true)
	a.layout()
	return func() tea.Msg {
		res, err := rpc.SkillInvoke(context.Background(), name, args, rpcclient.SkillInvokeOptions{})
		return skillResultMsg{res: res, err: err}
	}
}

// systemMsgMsg is a tea.Msg that appends a system message to the chat.
type systemMsgMsg struct {
	text string
}

// handleSystemMsg appends a system message and clears busy state.
func (a *App) handleSystemMsg(m systemMsgMsg) tea.Cmd {
	a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: m.text})
	a.chat.SetMessages(a.session.Messages)
	return nil
}

// handleWorkflowResult appends the workflow's final stage output (or error)
// to the chat and clears busy state.
func (a *App) handleWorkflowResult(m workflowResultMsg) tea.Cmd {
	a.agentBusy = false
	a.statusBar.SetAgentBusy(false)
	a.chat.SetAgentBusy(false)
	a.layout()
	if m.err != nil {
		a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] workflow.run: " + m.err.Error()})
		a.chat.SetMessages(a.session.Messages)
		return a.persistSessionCmd()
	}
	summary := fmt.Sprintf("[workflow:%s] finished in %dms · final stage: %s",
		m.res.Name, m.res.DurationMS, m.res.FinalStage)
	if m.res.FailureReason != "" {
		summary += "\n[workflow:" + m.res.Name + "] FAILED: " + m.res.FailureReason
	} else if out, ok := m.res.Outputs[m.res.FinalStage]; ok && out != "" {
		summary += "\n---\n" + out
	}
	a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: summary})
	a.chat.SetMessages(a.session.Messages)
	return a.persistSessionCmd()
}

// handleSkillResult appends the skill's output (or error) and clears busy.
func (a *App) handleSkillResult(m skillResultMsg) tea.Cmd {
	a.agentBusy = false
	a.statusBar.SetAgentBusy(false)
	a.chat.SetAgentBusy(false)
	a.layout()
	if m.err != nil {
		a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] skill.invoke: " + m.err.Error()})
		a.chat.SetMessages(a.session.Messages)
		return a.persistSessionCmd()
	}
	marker := m.res.Marker
	if marker == "" {
		marker = "(no marker)"
	}
	summary := fmt.Sprintf("[skill:%s] %d step(s) · marker=%s\n---\n%s",
		m.res.Skill, m.res.Steps, marker, m.res.Output)
	a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: summary})
	a.chat.SetMessages(a.session.Messages)
	return a.persistSessionCmd()
}
