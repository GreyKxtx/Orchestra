// Package view contains the TUI's visual components.
//
// The chat layer is split across several files:
//
//	chat.go              — Chat struct, public API, dispatcher
//	message_*.go         — user/assistant/system render
//	tool_*.go            — tool kinds, icons, inline/block/group
//	diff_message.go      — RoleDiff panels
//	chat_welcome.go      — empty-state welcome screen
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
	streamCursor  bool           // when true, appends ▋ to last assistant token
	welcome       WelcomeInfo    // metadata for the empty-state welcome screen
	forceWelcome  bool           // when true, always show welcome regardless of content
	chatMode      string         // current agent mode; drives ┃ color + footer
	chatModel     string         // current model name; rendered in per-turn footer
	spinFrame     int            // current spinner frame; advanced by App every tick
	userScrolled  bool           // true while user is reading scrolled-back history
	expandedTurns map[int64]bool // mirror of Message.ToolsExpanded for cache skip / invalidation
	cache         renderCache    // per-message render cache for completed assistant turns
	msgRanges     []msgYRange    // content-line ranges for click-to-action detection
	diffReviewCursor int // >=0 when diff review hotkeys active; -1 otherwise
	actionBar        ActionBarState
	showActionBar    bool
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

// SetDiffReviewCursor highlights one file in the expanded diff panel (-1 = off).
func (c *Chat) SetDiffReviewCursor(idx int) { c.diffReviewCursor = idx }

// SetActionBar configures the inline pending-ops bar appended after the newest diff.
func (c *Chat) SetActionBar(st ActionBarState) {
	c.actionBar = st
	c.showActionBar = st.OpCount > 0 || st.FileCount > 0
}

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

// SetStreamCursor controls whether a blinking cursor is appended to the
// last message (used while agent is streaming a response).
func (c *Chat) SetStreamCursor(on bool) { c.streamCursor = on }

// SetSpinFrame sets the current spinner frame index.
func (c *Chat) SetSpinFrame(n int) { c.spinFrame = n }

// SetTurnExpanded mirrors Message.ToolsExpanded into the chat expand cache
// and invalidates the render cache entry. Prefer flipping ToolsExpanded on
// the message (persist SoT), then calling this to keep cache in sync.
func (c *Chat) SetTurnExpanded(key int64, expanded bool) {
	if key == 0 {
		return
	}
	if c.expandedTurns == nil {
		c.expandedTurns = map[int64]bool{}
	}
	if expanded {
		c.expandedTurns[key] = true
	} else {
		delete(c.expandedTurns, key)
	}
	c.cache.delete(key)
}

// IsTurnExpanded reports whether the expand cache marks the turn expanded.
// Prefer Message.ToolsExpanded for persist/UX decisions; this is for tests
// and cache-skip mirroring.
func (c *Chat) IsTurnExpanded(key int64) bool {
	return c.expandedTurns[key]
}

// InvalidateMessage drops a cached render for key (e.g. after ReasoningExpanded toggle).
func (c *Chat) InvalidateMessage(key int64) {
	if key != 0 {
		c.cache.delete(key)
	}
}

// SyncExpandFromMessages reseeds the expand cache from persisted ToolsExpanded.
func (c *Chat) SyncExpandFromMessages(msgs []state.Message) {
	c.expandedTurns = map[int64]bool{}
	for _, m := range msgs {
		if m.Role != state.RoleAssistant || !m.ToolsExpanded {
			continue
		}
		c.SetTurnExpanded(MessageKey(m), true)
	}
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
func (c *Chat) SetMessages(msgs []state.Message) {
	msgs = CollapseOldTurnsForView(msgs)
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
	lastDiffIdx := -1
	userQueryFor := make([]string, len(msgs))
	pendingUser := ""
	for i := 0; i < len(msgs); i++ {
		switch msgs[i].Role {
		case state.RoleUser:
			pendingUser = msgs[i].Text
		case state.RoleAssistant:
			userQueryFor[i] = pendingUser
			lastAssistantIdx = i
		case state.RoleDiff:
			if len(msgs[i].DiffFiles) > 0 {
				lastDiffIdx = i
			}
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
			// (Ctrl+T) — their output varies with ToolsExpanded.
			key := MessageKey(m)
			if !m.Streaming && !isLast && !m.ToolsExpanded {
				if cached, ok := c.cache.get(key, width); ok {
					rendered = cached
				} else {
					rendered = c.renderAssistantMessage(m, width, false, userQueryFor[i])
					c.cache.put(key, width, rendered)
				}
			} else {
				rendered = c.renderAssistantMessage(m, width, isLast, userQueryFor[i])
			}

		case state.RoleSystem:
			rendered = RenderSystemMessage(m, width)

		case state.RoleDiff:
			cursor := -1
			if m.DiffExpanded {
				cursor = c.diffReviewCursor
			}
			rendered = RenderDiffMessage(m.DiffFiles, width, m.DiffExpanded, cursor)
			if c.showActionBar && i == lastDiffIdx {
				if bar := RenderActionBar(c.actionBar, width); bar != "" {
					rendered += "\n" + bar
				}
			}
		}

		b.WriteString(rendered)
		linesAdded := strings.Count(rendered, "\n") + 1
		end := start + linesAdded - 1

		if m.Role == state.RoleUser || m.Role == state.RoleAssistant {
			c.msgRanges = append(c.msgRanges, msgYRange{
				role:  m.Role,
				text:  messageCopyText(m),
				start: start,
				end:   end,
			})
		}
		if m.Role == state.RoleDiff || m.Role == state.RoleSystem {
			c.msgRanges = append(c.msgRanges, msgYRange{
				role:  m.Role,
				text:  messageCopyText(m),
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

// messageCopyText builds clipboard payload in chronological segment order.
func messageCopyText(m state.Message) string {
	var b strings.Builder
	segs := m.Segments
	if len(segs) == 0 {
		if strings.TrimSpace(m.Reasoning) != "" {
			segs = append(segs, state.Segment{Kind: state.SegmentReasoning, Text: m.Reasoning})
		}
		if len(m.ToolBlocks) > 0 {
			segs = append(segs, state.Segment{Kind: state.SegmentTools, Tools: m.ToolBlocks})
		}
		if strings.TrimSpace(m.Text) != "" {
			segs = append(segs, state.Segment{Kind: state.SegmentText, Text: m.Text})
		}
	}
	for _, seg := range segs {
		switch seg.Kind {
		case state.SegmentReasoning:
			if r := strings.TrimSpace(seg.Text); r != "" {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString("Thinking:\n")
				b.WriteString(r)
			}
		case state.SegmentText:
			if t := strings.TrimSpace(seg.Text); t != "" {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(t)
			}
		case state.SegmentTools:
			if len(seg.Tools) == 0 {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString("Tools:\n")
			for _, tb := range seg.Tools {
				b.WriteString("- ")
				b.WriteString(tb.Name)
				if tb.ArgsPreview != "" {
					b.WriteString(" ")
					b.WriteString(tb.ArgsPreview)
				}
				b.WriteString(" [")
				b.WriteString(string(tb.Status))
				b.WriteString("]")
				if out := strings.TrimSpace(tb.Result); out != "" {
					b.WriteString("\n  ")
					b.WriteString(truncateForCopy(out, 2000))
				}
				b.WriteByte('\n')
			}
		case state.SegmentNotice:
			if t := strings.TrimSpace(seg.Text); t != "" {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				kind := string(seg.NoticeKind)
				if kind == "" {
					kind = "info"
				}
				b.WriteString(kind)
				b.WriteString(": ")
				b.WriteString(t)
			}
		}
	}
	if len(m.DiffFiles) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Diff:\n")
		for _, df := range m.DiffFiles {
			b.WriteString("- ")
			b.WriteString(df.Path)
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return m.Text
	}
	return strings.TrimSpace(b.String())
}

func truncateForCopy(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
