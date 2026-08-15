package lessons

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// PromoteSuggestThreshold is how many repeated anti-pattern signals
	// trigger a lesson_promote suggestion to the Lead.
	PromoteSuggestThreshold = 3

	signalsRelDir = ".orchestra/memory/lessons/signals"
)

// AntiPatternKey normalizes verify/task text for repeat counting.
func AntiPatternKey(verify, task string) string {
	key := strings.TrimSpace(verify)
	if key == "" {
		key = strings.TrimSpace(task)
	}
	key = strings.ToLower(strings.ReplaceAll(key, "\n", " "))
	if key == "" {
		return "unknown"
	}
	if len(key) > 160 {
		key = key[:160]
	}
	return key
}

// BumpAntiPatternSignal records one anti-pattern occurrence and returns the
// running count for that key (including this bump).
func BumpAntiPatternSignal(projectRoot, dept, key string) int {
	if projectRoot == "" {
		return 0
	}
	dept = NormalizeDept(dept)
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	dir := filepath.Join(projectRoot, filepath.FromSlash(signalsRelDir))
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

func countSignalKey(path, key string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	key = strings.ToLower(key)
	n := 0
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		i := strings.Index(ln, "|")
		if i < 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(ln[i+1:]), key) {
			n++
		}
	}
	return n
}

// ClearAntiPatternSignals removes repeat counters for a dept (after promote).
func ClearAntiPatternSignals(projectRoot, dept string) {
	if projectRoot == "" {
		return
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(signalsRelDir), NormalizeDept(dept)+".log")
	_ = os.Remove(path)
}

// FormatPromoteHint returns Lead-facing guidance when threshold is reached.
func FormatPromoteHint(dept string, count int) string {
	dept = NormalizeDept(dept)
	return fmt.Sprintf(
		"Repeated anti-pattern (%d×) for dept %q — consider lesson_promote{\"dept\":%q} to draft a local playbook overlay from the latest pattern lesson",
		count, dept, dept,
	)
}
