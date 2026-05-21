// Package view contains the TUI's visual components.
//
// The chat layer is split across several files:
//
//	chat.go         — Chat struct, public API, dispatcher
//	chat_render.go  — user/assistant/reasoning render + markdown + footer
//	chat_tools.go   — tool kinds, icons, previews, inline/block/group rendering
//	chat_welcome.go — empty-state welcome screen + ASCII logo
package view

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/state"
)

// SpinnerFrames is the braille spinner animation used for running tools,
// reasoning indicators, and the "Thinking…" line. Mirrors OpenCode's frames.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// msgYRange records the content-line span of one rendered message.
type msgYRange struct {
	role  state.Role
	text  string // plain text for copy/edit
	start int    // first content line (inclusive)
	end   int    // last content line (inclusive)
}

// Chat renders the scrollable history of messages.
type Chat struct {
	vp            viewport.Model
	streamCursor  bool            // when true, appends ▋ to last assistant token
	welcome       WelcomeInfo     // metadata for the empty-state welcome screen
	forceWelcome  bool            // when true, always show welcome regardless of content
	agentBusy     bool            // affects the help line shown below messages
	chatMode      string          // current agent mode; drives ┃ color + footer
	chatModel     string          // current model name; rendered in per-turn footer
	spinFrame     int             // current spinner frame; advanced by App every tick
	userScrolled  bool            // true while user is reading scrolled-back history
	expandedTurns map[int64]bool  // assistant msg keys (StartedAt.UnixNano) the user expanded
	cache         renderCache     // per-message render cache for completed assistant turns
	msgRanges     []msgYRange     // content-line ranges for click-to-action detection
	width         int
	height        int
}

// NewChat creates an empty chat view sized to width × height.
func NewChat(width, height int) Chat {
	return Chat{vp: viewport.New(width, height), width: width, height: height}
}

// SetSize resizes the chat viewport. The viewport now fills the full
// chat-area height — the legacy in-chat help line was removed; key hints
// live in the bottom StatusBar instead.
func (c *Chat) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.vp.Width = width
	c.vp.Height = height
}

// SetMeta records the active mode/model so the per-turn footer
// (▣ <mode> · <model>) and the user-message ┃ accent stay in sync with
// what the rest of the UI displays.
func (c *Chat) SetMeta(mode, model string) {
	c.chatMode = mode
	c.chatModel = model
}

// SetWelcomeInfo updates the project metadata displayed on the welcome screen.
func (c *Chat) SetWelcomeInfo(info WelcomeInfo) { c.welcome = info }

// SetForceWelcome controls whether the welcome screen is shown regardless of chat content.
func (c *Chat) SetForceWelcome(v bool) { c.forceWelcome = v }

// SetAgentBusy controls the help line text shown below messages.
func (c *Chat) SetAgentBusy(b bool) { c.agentBusy = b }

// SetStreamCursor controls whether a blinking cursor is appended to the
// last message (used while agent is streaming a response).
func (c *Chat) SetStreamCursor(on bool) { c.streamCursor = on }

// SetSpinFrame sets the current spinner frame index.
func (c *Chat) SetSpinFrame(n int) { c.spinFrame = n }

// ExpandTurn toggles the expanded/collapsed state of the assistant turn
// identified by its StartedAt timestamp (stable across message-list mutations
// like /clear or RemoveDiff that would shift integer indices).
// The render cache entry for this key is invalidated so SetMessages rebuilds
// the output with the new expand state instead of returning stale HTML.
func (c *Chat) ExpandTurn(key int64) {
	if c.expandedTurns == nil {
		c.expandedTurns = map[int64]bool{}
	}
	c.expandedTurns[key] = !c.expandedTurns[key]
	c.cache.delete(key)
}

// MessageKey returns the stable key for a message used in expandedTurns and
// the render cache. Zero StartedAt → 0 (caller must treat as uncacheable).
func MessageKey(m state.Message) int64 {
	if m.StartedAt.IsZero() {
		return 0
	}
	return m.StartedAt.UnixNano()
}

// ScrollUp moves the chat viewport up by n lines (half-page when n=0).
func (c *Chat) ScrollUp(n int) {
	if n <= 0 {
		n = c.vp.Height / 2
	}
	c.vp.ScrollUp(n)
	c.userScrolled = !c.vp.AtBottom()
}

// ScrollDown moves the chat viewport down by n lines (half-page when n=0).
func (c *Chat) ScrollDown(n int) {
	if n <= 0 {
		n = c.vp.Height / 2
	}
	c.vp.ScrollDown(n)
	c.userScrolled = !c.vp.AtBottom()
}

// ScrollToBottom snaps to the latest message and clears the user-scrolled flag.
func (c *Chat) ScrollToBottom() {
	c.vp.GotoBottom()
	c.userScrolled = false
}

// SetMessages re-renders the viewport content from the session messages.
// Visual structure mirrors OpenCode's session view (see chat_render.go,
// chat_tools.go for the per-element implementations).
func (c *Chat) SetMessages(msgs []state.Message) {
	if len(msgs) == 0 {
		c.vp.SetContent("")
		c.userScrolled = false // cleared chat → next stream should auto-scroll
		c.cache.purge()
		c.msgRanges = c.msgRanges[:0]
		return
	}

	width := c.vp.Width

	// Single backward pass: find the last assistant index and pair each
	// assistant message with the user query immediately preceding it.
	// Avoids the previous O(N²) nested scan.
	lastAssistantIdx := -1
	userQueryFor := make([]string, len(msgs))
	pendingUser := ""
	for i := 0; i < len(msgs); i++ {
		switch msgs[i].Role {
		case state.RoleUser:
			pendingUser = msgs[i].Text
		case state.RoleAssistant:
			userQueryFor[i] = pendingUser
			lastAssistantIdx = i
		}
	}

	c.msgRanges = c.msgRanges[:0]
	lineCount := 0

	var b strings.Builder
	for i, m := range msgs {
		start := lineCount
		var rendered string

		switch m.Role {
		case state.RoleUser:
			rendered = c.renderUserMessage(m, width)

		case state.RoleAssistant:
			isLast := i == lastAssistantIdx
			// Cache lookup: completed (non-streaming) assistant messages render
			// to the same string forever — no need to rebuild markdown / tool
			// groups every 100ms tick. Skip cache for: streaming turns, the
			// last assistant (footer depends on isLast), and expanded turns
			// (Ctrl+T) — their output varies with expandedTurns state.
			key := MessageKey(m)
			expanded := c.expandedTurns[key]
			if !m.Streaming && !isLast && !expanded {
				if cached, ok := c.cache.get(key, width); ok {
					rendered = cached
				} else {
					rendered = c.renderAssistantMessage(m, key, width, false, userQueryFor[i])
					c.cache.put(key, width, rendered)
				}
			} else {
				rendered = c.renderAssistantMessage(m, key, width, isLast, userQueryFor[i])
			}

		case state.RoleSystem:
			s := CurrentStyles()
			rendered = s.Warning.Italic(true).PaddingLeft(2).Render(m.Text)

		case state.RoleDiff:
			s := CurrentStyles()
			rendered = s.Muted.PaddingLeft(2).Render(m.Text)
		}

		b.WriteString(rendered)
		linesAdded := strings.Count(rendered, "\n") + 1
		end := start + linesAdded - 1

		if m.Role == state.RoleUser || m.Role == state.RoleAssistant {
			c.msgRanges = append(c.msgRanges, msgYRange{
				role:  m.Role,
				text:  m.Text,
				start: start,
				end:   end,
			})
		}
		lineCount = end + 1

		if i < len(msgs)-1 {
			b.WriteString("\n\n")
			lineCount += 2
		}
	}
	// bubbles' viewport.SetContent resets YOffset to 0; when the user is
	// reading scrolled-back content we want to keep them on the same lines
	// (NOT push them down when new tokens land at the bottom). Preserve the
	// absolute YOffset across SetContent calls.
	keepOffset := c.vp.YOffset
	c.vp.SetContent(b.String())
	if c.userScrolled {
		c.vp.SetYOffset(keepOffset)
	} else {
		c.vp.GotoBottom()
	}
}

// View returns the viewport's current view, or the welcome screen when
// content is empty or forced. Key hints live in StatusBar — no in-chat
// help row is drawn here anymore.
func (c Chat) View() string {
	if c.vp.Width == 0 {
		return ""
	}
	if c.forceWelcome || c.vp.TotalLineCount() == 0 {
		return lipgloss.NewStyle().
			Width(c.width).
			Height(c.height).
			Render(c.welcomeScreen())
	}
	return c.vp.View()
}

// Render is an alias for View (keeps compatibility with app.go).
func (c Chat) Render() string { return c.View() }

// ViewportYOffset returns the current scroll offset in content lines.
func (c Chat) ViewportYOffset() int { return c.vp.YOffset }

// MessageAtContentY returns the role and plain text of the message whose
// rendered lines include content-line y. Returns ok=false for blank gaps
// between messages or when no messages have been rendered yet.
func (c Chat) MessageAtContentY(y int) (role state.Role, text string, ok bool) {
	for _, r := range c.msgRanges {
		if y >= r.start && y <= r.end {
			return r.role, r.text, true
		}
	}
	return "", "", false
}
