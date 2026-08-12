package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/ui/tui/state"
)

type sessionCompactDoneMsg struct {
	before, after int
	err           error
}

func (a *App) cmdSessionCompact() tea.Cmd {
	rpc := a.rpc
	sid := a.coreSessionID
	if rpc == nil || sid == "" {
		a.session.AppendSystemNotice(state.SystemKindError, "нет активной core-сессии для /compact")
		a.chat.SetMessages(a.session.Messages)
		return nil
	}
	a.showToast("сжимаю контекст…")
	return func() tea.Msg {
		// Compaction runs an LLM summarization — give it generous headroom,
		// but never let a hung core leak this goroutine forever.
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		res, err := rpc.SessionCompact(ctx, sid, "")
		if err != nil {
			return sessionCompactDoneMsg{err: err}
		}
		return sessionCompactDoneMsg{before: res.BeforeMsgs, after: res.AfterMsgs}
	}
}

func (a *App) handleSessionCompactDone(m sessionCompactDoneMsg) {
	if m.err != nil {
		a.session.AppendSystemNotice(state.SystemKindError, "compact: "+m.err.Error())
	} else {
		a.session.AppendSystemNotice(state.SystemKindInfo,
			fmt.Sprintf("Контекст сжат: %d → %d msgs (LLM history)", m.before, m.after))
		a.chrome.promptTokensUsed = 0
		a.syncStatusBar()
	}
	a.chat.SetMessages(a.session.Messages)
	a.layout()
}

func (a *App) cmdShowMemory() tea.Cmd {
	root := a.cfg.WorkspaceRoot
	var b strings.Builder
	b.WriteString("Слои памяти:\n")
	paths := []struct {
		label, path string
	}{
		{"orchestra", filepath.Join(root, "ORCHESTRA.md")},
		{"agent", filepath.Join(root, ".orchestra", "memory", "agent.md")},
		{"global", ""},
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths[2].path = filepath.Join(home, ".orchestra", "memory.md")
	}
	if a.coreSessionID != "" {
		paths = append(paths, struct{ label, path string }{
			"session", filepath.Join(root, ".orchestra", "memory", "sessions", a.coreSessionID+".md"),
		})
	}
	for _, p := range paths {
		if p.path == "" {
			b.WriteString(fmt.Sprintf("  %s: (n/a)\n", p.label))
			continue
		}
		st, err := os.Stat(p.path)
		if err != nil {
			b.WriteString(fmt.Sprintf("  %s: —\n", p.label))
			continue
		}
		b.WriteString(fmt.Sprintf("  %s: %s (%d B)\n", p.label, p.path, st.Size()))
	}
	// Pinned facts preview
	agentPath := filepath.Join(root, ".orchestra", "memory", "agent.md")
	if raw, err := os.ReadFile(agentPath); err == nil {
		pins := memory.PinnedEntries(string(raw))
		if len(pins) > 0 {
			b.WriteString("\nPinned facts:\n")
			for i, p := range pins {
				if i >= 5 {
					b.WriteString(fmt.Sprintf("  … +%d more\n", len(pins)-5))
					break
				}
				line := strings.SplitN(strings.TrimSpace(p), "\n", 2)[0]
				if len(line) > 80 {
					line = line[:80] + "…"
				}
				b.WriteString("  • " + line + "\n")
			}
		}
	}
	a.session.AppendSystemNotice(state.SystemKindInfo, strings.TrimSpace(b.String()))
	a.chat.SetMessages(a.session.Messages)
	return nil
}
