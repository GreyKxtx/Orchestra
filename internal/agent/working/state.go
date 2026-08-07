// Package working provides rule-based turn digests and an in-turn working-state
// ledger for local token economy (no LLM).
package working

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	maxFilesListed     = 12
	maxErrorsListed    = 4
	maxTodosListed     = 6
	maxWorkingBytes    = 1200
	maxDigestBytes     = 600
	defaultKeepDigests = 3
)

// TodoView is a minimal todo snapshot for formatting (avoids importing tools).
type TodoView struct {
	Content string
	Status  string
}

// State is the mutable ledger for one Agent.Run.
type State struct {
	mu sync.Mutex

	Goal string

	files  map[string]struct{}
	tools  map[string]int
	errors []string
	todos  []TodoView
	done   []string // short outcome lines
}

// New returns an empty working state for goal.
func New(goal string) *State {
	return &State{
		Goal:  strings.TrimSpace(goal),
		files: make(map[string]struct{}),
		tools: make(map[string]int),
	}
}

// SetTodos replaces the todo snapshot used in Format.
func (s *State) SetTodos(todos []TodoView) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = append([]TodoView(nil), todos...)
}

// ActiveFiles returns paths observed this Run (read/edit/write/…), sorted.
// Used to protect still-relevant tool results from retroactive prune.
func (s *State) ActiveFiles() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.files) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.files))
	for p := range s.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ObserveTool records a tool call outcome into the ledger.
func (s *State) ObserveTool(name string, input json.RawMessage, out []byte, callErr error) {
	if s == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[name]++
	if p := pathFromInput(input); p != "" {
		s.files[p] = struct{}{}
	}
	if callErr != nil {
		s.addErrorLocked(shortErr(callErr.Error()))
		return
	}
	switch name {
	case "write", "edit":
		if p := pathFromInput(input); p != "" {
			s.done = appendUnique(s.done, name+" "+p)
		}
		for _, e := range extractDiagErrors(out) {
			s.addErrorLocked(e)
		}
	case "bash", "exec.run":
		if cmd := cmdFromInput(input); cmd != "" {
			s.done = appendUnique(s.done, "bash "+clip(cmd, 40))
		}
	case "read", "grep", "explore", "semantic_search":
		// navigation — files already tracked
	}
}

func (s *State) addErrorLocked(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	for _, e := range s.errors {
		if e == msg {
			return
		}
	}
	if len(s.errors) >= maxErrorsListed {
		return
	}
	s.errors = append(s.errors, msg)
}

// FormatWorkingState returns a compact <working_state> block (may be empty).
func (s *State) FormatWorkingState() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Goal == "" && len(s.files) == 0 && len(s.tools) == 0 && len(s.errors) == 0 && len(s.todos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<working_state>\n")
	if s.Goal != "" {
		b.WriteString("goal: ")
		b.WriteString(clip(s.Goal, 120))
		b.WriteString("\n")
	}
	if files := sortedKeys(s.files); len(files) > 0 {
		if len(files) > maxFilesListed {
			files = append(files[:maxFilesListed], fmt.Sprintf("…+%d", len(files)-maxFilesListed))
		}
		b.WriteString("files: ")
		b.WriteString(strings.Join(files, ", "))
		b.WriteString("\n")
	}
	if len(s.tools) > 0 {
		b.WriteString("tools: ")
		b.WriteString(formatToolCounts(s.tools))
		b.WriteString("\n")
	}
	if len(s.errors) > 0 {
		b.WriteString("errors: ")
		b.WriteString(strings.Join(s.errors, "; "))
		b.WriteString("\n")
	}
	if len(s.todos) > 0 {
		b.WriteString("todos:")
		n := 0
		for _, t := range s.todos {
			if n >= maxTodosListed {
				b.WriteString(fmt.Sprintf(" …+%d", len(s.todos)-maxTodosListed))
				break
			}
			b.WriteString(" [")
			b.WriteString(t.Status)
			b.WriteString("] ")
			b.WriteString(clip(t.Content, 40))
			b.WriteString(";")
			n++
		}
		b.WriteString("\n")
	}
	b.WriteString("</working_state>")
	out := b.String()
	if len(out) > maxWorkingBytes {
		out = out[:maxWorkingBytes-20] + "\n…(truncated)\n</working_state>"
	}
	return out
}

// BuildTurnDigest returns a rule-based turn digest body (without --- wrappers).
// When step > 0, a step: line marks a mid-run micro-summary (history untouched).
func (s *State) BuildTurnDigest(step int) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	b.WriteString("[turn_digest]\n")
	if step > 0 {
		b.WriteString(fmt.Sprintf("step: %d\n", step))
	}
	b.WriteString("goal: ")
	b.WriteString(clip(s.Goal, 120))
	b.WriteString("\n")
	if len(s.done) > 0 {
		b.WriteString("done: ")
		b.WriteString(strings.Join(clipList(s.done, 6, 40), "; "))
		b.WriteString("\n")
	}
	open := openTodos(s.todos)
	if len(open) > 0 {
		b.WriteString("open: ")
		b.WriteString(strings.Join(clipList(open, 4, 40), "; "))
		b.WriteString("\n")
	}
	if files := sortedKeys(s.files); len(files) > 0 {
		if len(files) > maxFilesListed {
			files = files[:maxFilesListed]
		}
		b.WriteString("files: ")
		b.WriteString(strings.Join(files, ", "))
		b.WriteString("\n")
	}
	if len(s.tools) > 0 {
		b.WriteString("tools: ")
		b.WriteString(formatToolCounts(s.tools))
		b.WriteString("\n")
	}
	if len(s.errors) > 0 {
		b.WriteString("errors: ")
		b.WriteString(strings.Join(s.errors, "; "))
		b.WriteString("\n")
	}
	out := b.String()
	if len(out) > maxDigestBytes {
		out = out[:maxDigestBytes-4] + "…\n"
	}
	return out
}

const maxStoredTurnDigests = 24

// PersistTurnDigest appends a turn digest to sessions/<id>.turns.md and trims
// the file to the last maxStoredTurnDigests entries (unbounded growth guard).
func PersistTurnDigest(workspaceRoot, sessionID, digest string) error {
	digest = strings.TrimSpace(digest)
	sessionID = strings.TrimSpace(sessionID)
	if digest == "" || sessionID == "" || workspaceRoot == "" {
		return nil
	}
	dir := filepath.Join(workspaceRoot, ".orchestra", "memory", "sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, sessionID+".turns.md")
	entry := "\n---\n" + digest + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err = f.WriteString(entry); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()
	return trimTurnDigestFile(path, maxStoredTurnDigests)
}

func trimTurnDigestFile(path string, keep int) error {
	if keep <= 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	parts := strings.Split(string(data), "\n---\n")
	var digests []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.Contains(p, "[turn_digest]") {
			digests = append(digests, p)
		}
	}
	if len(digests) <= keep {
		return nil
	}
	digests = digests[len(digests)-keep:]
	var b strings.Builder
	for i, d := range digests {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(d)
		b.WriteString("\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}

// FormatRecentTurnDigests loads last keep digests for prompt inject.
func FormatRecentTurnDigests(workspaceRoot, sessionID string, keep int) string {
	if keep <= 0 || sessionID == "" {
		return ""
	}
	if keep > 8 {
		keep = 8
	}
	path := filepath.Join(workspaceRoot, ".orchestra", "memory", "sessions", sessionID+".turns.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	parts := strings.Split(string(data), "\n---\n")
	var digests []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.Contains(p, "[turn_digest]") {
			digests = append(digests, p)
		}
	}
	if len(digests) == 0 {
		return ""
	}
	if len(digests) > keep {
		digests = digests[len(digests)-keep:]
	}
	var b strings.Builder
	b.WriteString("<turn_digests>\n")
	b.WriteString(strings.Join(digests, "\n---\n"))
	b.WriteString("\n</turn_digests>")
	out := b.String()
	if len(out) > 2000 {
		out = out[len(out)-2000:]
		if i := strings.Index(out, "[turn_digest]"); i > 0 {
			out = out[i:]
		}
		out = "<turn_digests>\n" + out + "\n</turn_digests>"
	}
	return out
}

func pathFromInput(input json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	for _, k := range []string{"path", "file", "file_path", "target"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return filepath.ToSlash(strings.TrimSpace(v))
		}
	}
	return ""
}

func cmdFromInput(input json.RawMessage) string {
	var m struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
	}
	_ = json.Unmarshal(input, &m)
	if m.Command != "" {
		return m.Command
	}
	return m.Cmd
}

func extractDiagErrors(out []byte) []string {
	var resp struct {
		Diagnostics []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
			Path     string `json:"path"`
		} `json:"diagnostics"`
	}
	if json.Unmarshal(out, &resp) != nil {
		return nil
	}
	var errs []string
	for _, d := range resp.Diagnostics {
		if !strings.EqualFold(d.Severity, "error") {
			continue
		}
		msg := d.Message
		if d.Path != "" {
			msg = d.Path + ": " + msg
		}
		errs = append(errs, clip(msg, 80))
		if len(errs) >= maxErrorsListed {
			break
		}
	}
	return errs
}

func openTodos(todos []TodoView) []string {
	var out []string
	for _, t := range todos {
		st := strings.ToLower(t.Status)
		if st == "completed" || st == "cancelled" || st == "done" {
			continue
		}
		out = append(out, t.Content)
	}
	return out
}

func formatToolCounts(m map[string]int) string {
	keys := sortedKeysInt(m)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysInt(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func appendUnique(list []string, item string) []string {
	for _, x := range list {
		if x == item {
			return list
		}
	}
	if len(list) >= 8 {
		return list
	}
	return append(list, item)
}

func clipList(items []string, n, each int) []string {
	if len(items) > n {
		items = items[:n]
	}
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = clip(s, each)
	}
	return out
}

func clip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func shortErr(s string) string {
	s = clip(s, 100)
	if i := strings.Index(s, "\n"); i > 0 {
		s = s[:i]
	}
	return s
}
