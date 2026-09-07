package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		// orchestra-user is the user's own standing instructions; global is
		// the agent's own notes. Different authors, different lifetimes —
		// listing them apart is the whole point of the split.
		{"orchestra-user", ""},
		{"orchestra", filepath.Join(root, "ORCHESTRA.md")},
		{"agent", filepath.Join(root, ".orchestra", "memory", "agent.md")},
		{"global", ""},
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths[0].path = filepath.Join(home, ".orchestra", "ORCHESTRA.md")
		paths[3].path = filepath.Join(home, ".orchestra", "memory.md")
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

// parseMemorySlashCommand matches "/memory open", "/memory refresh" and
// "/memory search <query>". Bare "/memory" is the existing view-only command
// (executePaletteCmd's exact-match switch) and is deliberately not handled
// here.
//
// arg is empty for the argument-less verbs and carries the whole remainder for
// search — everything after "search", not just the next field, because a
// memory query is normally a phrase.
func parseMemorySlashCommand(text string) (verb, arg string, ok bool) {
	fields := strings.Fields(text)
	if len(fields) < 2 || fields[0] != "/memory" {
		return "", "", false
	}
	switch fields[1] {
	case "open", "refresh":
		if len(fields) != 2 {
			return "", "", false
		}
		return fields[1], "", true
	case "search":
		// Rejoined from the fields rather than sliced out of the raw text: the
		// user may have typed runs of spaces, and the query is matched as a
		// substring, so the spacing has to be normalised.
		q := strings.Join(fields[2:], " ")
		if q == "" {
			return "", "", false
		}
		return "search", q, true
	default:
		return "", "", false
	}
}

// maybeRunMemoryCommand runs the /memory subcommand text matches, or nil for
// anything else (including a bare /memory, and plain chat text).
func (a *App) maybeRunMemoryCommand(text string) tea.Cmd {
	verb, arg, ok := parseMemorySlashCommand(text)
	if !ok {
		return nil
	}
	switch verb {
	case "open":
		return a.cmdMemoryOpen()
	case "refresh":
		return a.cmdMemoryRefresh()
	case "search":
		return a.cmdMemorySearch(arg)
	}
	return nil
}

// memorySearchHitLimit caps what one /memory search prints. Memory files grow
// for the life of a project, and a broad query would otherwise push the
// conversation out of the pane.
const memorySearchHitLimit = 8

// memoryHit is one matching memory entry.
type memoryHit struct {
	Layer   string
	Snippet string
}

// searchMemoryLayers runs the substring search the model's memory_search tool
// runs — same layers, same order — for the user.
//
// Deliberately the substring path only. memory_search additionally ranks
// semantically when embed.model is set, but that needs an embedding endpoint
// and the project config the TUI does not carry. A palette command that
// answers instantly and offline is worth more here, and it cannot degrade
// silently because it never promised ranking.
func searchMemoryLayers(root, sessionID, query string, limit int) []memoryHit {
	query = strings.TrimSpace(query)
	if query == "" || limit <= 0 {
		return nil
	}
	// GlobalEnabled mirrors memory_read, whose layer set includes global — a
	// fact recorded in ~/.orchestra/memory.md is still a remembered fact.
	store := memory.NewStore(root, sessionID, memory.Config{GlobalEnabled: true})

	var hits []memoryHit
	for _, layer := range []string{"repo", "session", "global", "orchestra", "orchestra-user"} {
		if len(hits) >= limit {
			break
		}
		// Store.Read reports its failure modes as CONTENT, not as errors: the
		// session layer with no active session answers with the literal string
		// "no active session_id". Searching that finds a hit indistinguishable
		// from a remembered fact, attributed to a layer that does not exist
		// yet. Skipping the layer beats matching the sentinel by name, which
		// would break the moment the wording changed.
		if layer == "session" && sessionID == "" {
			continue
		}
		res := store.Read(layer, "", 256*1024)
		if res.Content == "" {
			continue
		}
		for _, e := range memory.SearchEntries(res.Content, query, limit-len(hits)) {
			hits = append(hits, memoryHit{Layer: layer, Snippet: strings.TrimSpace(e)})
		}
	}
	return hits
}

func (a *App) cmdMemorySearch(query string) tea.Cmd {
	hits := searchMemoryLayers(a.cfg.WorkspaceRoot, a.coreSessionID, query, memorySearchHitLimit)
	if len(hits) == 0 {
		a.session.AppendSystemNotice(state.SystemKindInfo,
			fmt.Sprintf("Память: по запросу %q ничего не найдено", query))
		a.chat.SetMessages(a.session.Messages)
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Память — %d совпадений по %q:\n", len(hits), query)
	for _, h := range hits {
		snippet := h.Snippet
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		// Indent continuation lines so a multi-line entry reads as one hit.
		snippet = strings.ReplaceAll(snippet, "\n", "\n    ")
		fmt.Fprintf(&b, "\n  [%s] %s\n", h.Layer, snippet)
	}
	a.session.AppendSystemNotice(state.SystemKindInfo, strings.TrimRight(b.String(), "\n"))
	a.chat.SetMessages(a.session.Messages)
	return nil
}

// resolveEditor returns the user's editor command line ($EDITOR, which may
// itself carry flags like "code --wait"), or an OS-appropriate default when
// unset — there is no existing config knob for this.
func resolveEditor() string {
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// memoryOpenTarget resolves which file /memory open should open: whichever
// file actually backs the orchestra layer today (ORCHESTRA.md or its
// AGENTS.md/CLAUDE.md/.cursorrules/ORCHESTRA.local.md fallback), defaulting
// to ORCHESTRA.md so opening it in a fresh repo creates one there.
func memoryOpenTarget(root string) string {
	_, name := memory.FindProjectInstructions(root)
	if name == "" {
		name = "ORCHESTRA.md"
	}
	return filepath.Join(root, name)
}

// editorCommand builds the *exec.Cmd /memory open runs.
func editorCommand(root string) *exec.Cmd {
	fields := strings.Fields(resolveEditor())
	target := memoryOpenTarget(root)
	args := append(append([]string{}, fields[1:]...), target)
	return exec.Command(fields[0], args...)
}

// memoryOpenDoneMsg reports whether the editor process exited cleanly.
type memoryOpenDoneMsg struct {
	path string
	err  error
}

// cmdMemoryOpen suspends the TUI and opens the project's instructions file
// in $EDITOR, resuming once the editor exits.
func (a *App) cmdMemoryOpen() tea.Cmd {
	root := a.cfg.WorkspaceRoot
	target := memoryOpenTarget(root)
	c := editorCommand(root)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return memoryOpenDoneMsg{path: target, err: err}
	})
}

func (a *App) handleMemoryOpenDone(m memoryOpenDoneMsg) {
	if m.err != nil {
		a.session.AppendSystemNotice(state.SystemKindError, "memory open "+m.path+": "+m.err.Error())
	} else {
		a.session.AppendSystemNotice(state.SystemKindInfo, "закрыт "+m.path)
	}
	a.chat.SetMessages(a.session.Messages)
}

// cmdMemoryRefresh shows what the last turn actually injected (per-layer
// byte breakdown from llm_log.jsonl's memory.inject event), then the usual
// layer list — memory.List() alone never answered "did it actually fit the
// budget", only "does the file exist".
func (a *App) cmdMemoryRefresh() tea.Cmd {
	root := a.cfg.WorkspaceRoot
	logPath := filepath.Join(root, ".orchestra", "llm_log.jsonl")
	detail, err := memory.LastInjectDetail(logPath)
	switch {
	case err != nil:
		a.session.AppendSystemNotice(state.SystemKindError, "memory refresh: "+err.Error())
	case detail == "":
		a.session.AppendSystemNotice(state.SystemKindInfo, "Инжект памяти: ходов в этом проекте ещё не было")
	default:
		a.session.AppendSystemNotice(state.SystemKindInfo, "Последний инжект памяти:\n  "+detail)
	}
	a.chat.SetMessages(a.session.Messages)
	return a.cmdShowMemory()
}
