package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// mcpPromptPrefix is the slash form of an MCP prompt: /mcp:<server>:<prompt>.
// The server is in the name because two servers may both offer "review".
const mcpPromptPrefix = "/mcp:"

// parseMCPPromptCommand splits an input line into an MCP prompt command and
// its arguments. ok is false for anything else, including the built-in "/mcp"
// server dialog, which must keep working exactly as before.
func parseMCPPromptCommand(text string) (server, name, args string, ok bool) {
	line := strings.TrimSpace(text)
	if !strings.HasPrefix(line, mcpPromptPrefix) {
		return "", "", "", false
	}
	rest := line[len(mcpPromptPrefix):]

	// Arguments start at the first space; everything before it is the address.
	addr := rest
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		addr = rest[:i]
		args = strings.TrimSpace(rest[i+1:])
	}
	server, name, found := strings.Cut(addr, ":")
	if !found || strings.TrimSpace(server) == "" || strings.TrimSpace(name) == "" {
		return "", "", "", false
	}
	return server, name, args, true
}

// syncMCPPromptCommands loads the prompts the running servers offer into the
// slash palette. Best effort: a server that offers none, or a core without
// MCP at all, simply leaves the palette as it was.
func (a *App) syncMCPPromptCommands() tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		prompts, err := a.rpc.MCPPromptList(ctx)
		if err != nil {
			return nil
		}
		return mcpPromptsLoadedMsg{prompts: promptPaletteRows(prompts)}
	}
}

// promptPaletteRows turns the core's rendered prompt list into palette rows.
// The strings come from the core so the TUI and the VS Code panel cannot
// drift apart on how a prompt is labelled.
func promptPaletteRows(prompts []rpcclient.MCPPromptCommand) []view.SlashCmd {
	out := make([]view.SlashCmd, 0, len(prompts))
	for _, p := range prompts {
		if strings.TrimSpace(p.Slash) == "" {
			continue
		}
		out = append(out, view.SlashCmd{Cmd: p.Slash, Desc: p.Hint})
	}
	return out
}

// mcpPromptsLoadedMsg carries the discovered prompts back to the update loop,
// which owns the palette.
type mcpPromptsLoadedMsg struct {
	prompts []view.SlashCmd
}

// runMCPPrompt renders a server prompt and submits it as the user's turn.
//
// The rendered text is sent as an ordinary message rather than injected
// invisibly: what the model receives is then exactly what the transcript and
// the session file show, and the user can see what the server asked for.
func (a *App) runMCPPrompt(server, name, args string) tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		text, err := a.rpc.MCPPromptGet(ctx, server, name, args)
		if err != nil {
			return mcpPromptFailedMsg{command: mcpPromptPrefix + server + ":" + name, err: err}
		}
		return mcpPromptReadyMsg{text: text}
	}
}

// mcpPromptReadyMsg carries the rendered prompt text back for submission.
type mcpPromptReadyMsg struct{ text string }

// mcpPromptFailedMsg reports why a prompt could not be run. A missing
// required argument arrives here, which is why the message names the command.
type mcpPromptFailedMsg struct {
	command string
	err     error
}

// handleMCPPromptMsg applies the three prompt messages. Returns false when
// msg is not one of them.
func (a *App) handleMCPPromptMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case mcpPromptsLoadedMsg:
		a.slashPalette.SetExtra(m.prompts)
		return nil, true
	case mcpPromptReadyMsg:
		if strings.TrimSpace(m.text) == "" {
			a.showToast("Сервер вернул пустой prompt")
			return nil, true
		}
		return a.submitUserMessage(m.text), true
	case mcpPromptFailedMsg:
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: m.command + ": " + m.err.Error(),
		})
		a.chat.SetMessages(a.session.Messages)
		return nil, true
	}
	return nil, false
}
