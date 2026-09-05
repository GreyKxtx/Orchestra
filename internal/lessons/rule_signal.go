package lessons

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RuleSuggestThreshold is how many times the same anti-pattern must repeat
// on the same file before a human-facing "add a rule to ORCHESTRA.md?"
// suggestion fires.
//
// This is deliberately separate from PromoteSuggestThreshold (signals.go):
// that one is dept-scoped and LLM-facing (lesson_promote drafts a dept
// playbook overlay, gated by a Question Barrier answer). This one is
// file-scoped and human-facing — the plan's own example ("3× StaleContent
// on src/App.jsx") needs a file in the key, which AntiPatternKey doesn't
// carry, and sharing signals.go's per-dept log would let one feature's
// clear-on-resolve wipe the other's still-accumulating count.
const RuleSuggestThreshold = 3

const ruleSignalsRelDir = ".orchestra/memory/lessons/rule_signals"

// FileAntiPatternKey normalizes (file, verify/task) into a repeat-counting
// key for BumpRuleSignal — the file is part of the key, unlike
// AntiPatternKey's dept-only scope.
func FileAntiPatternKey(file, verify, task string) string {
	return strings.TrimSpace(file) + "|" + AntiPatternKey(verify, task)
}

// BumpRuleSignal records one occurrence of key for dept and returns the
// running count for that key (including this bump).
func BumpRuleSignal(projectRoot, dept, key string) int {
	if projectRoot == "" {
		return 0
	}
	dept = NormalizeDept(dept)
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	dir := filepath.Join(projectRoot, filepath.FromSlash(ruleSignalsRelDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0
	}
	path := filepath.Join(dir, dept+".log")
	line := time.Now().UTC().Format(time.RFC3339) + "|" + key + "\n"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
	return countSignalKey(path, key)
}

// ClearRuleSignal removes every recorded occurrence of key (only key, not
// the whole dept log) — used once the human accepts or declines the
// suggestion, so the same file+pattern combination needs three fresh
// occurrences before asking again instead of re-prompting immediately.
func ClearRuleSignal(projectRoot, dept, key string) {
	if projectRoot == "" {
		return
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(ruleSignalsRelDir), NormalizeDept(dept)+".log")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []string
	for _, ln := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		i := strings.Index(trimmed, "|")
		if i >= 0 && strings.EqualFold(strings.TrimSpace(trimmed[i+1:]), key) {
			continue
		}
		kept = append(kept, trimmed)
	}
	out := ""
	if len(kept) > 0 {
		out = strings.Join(kept, "\n") + "\n"
	}
	_ = os.WriteFile(path, []byte(out), 0o644)
}

// FormatRuleSuggestion is the human-facing chat prompt offering to turn a
// repeated anti-pattern into a project instruction.
func FormatRuleSuggestion(file string, count int, verify string) string {
	return fmt.Sprintf("%d× повторилась одна и та же ошибка на %s: %q — добавить правило в ORCHESTRA.md?",
		count, file, clipLine(verify, 120))
}

// FormatRuleLine is the actual line appended to ORCHESTRA.md when the human
// accepts.
func FormatRuleLine(file, verify string) string {
	return fmt.Sprintf("Перед правкой %s — прочитать файл целиком (повторяющаяся ошибка: %s)",
		file, clipLine(verify, 120))
}
