package prompt

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// WorkspaceSnapshot is a minimal "IDE snapshot" attached to each user request.
// In CLI mode, most fields are best-effort (env-driven).
type WorkspaceSnapshot struct {
	OS            string
	Shell         string
	WorkspaceRoot string
	IsGitRepo     bool

	ActiveFile string
	CursorLine int
	CursorCol  int
	OpenFiles  []string

	TerminalsPath string
	ChangedFiles  []string
}

// BuildUserInfoSnapshot builds a best-effort snapshot of the current environment.
// It intentionally avoids expensive calls (no git status, no filesystem scan beyond ".git").
func BuildUserInfoSnapshot(workspaceRoot string) WorkspaceSnapshot {
	root := strings.TrimSpace(workspaceRoot)
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}

	osID := runtime.GOOS + "/" + runtime.GOARCH

	shell := strings.TrimSpace(os.Getenv("ORCH_SHELL"))
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = strings.TrimSpace(os.Getenv("ComSpec"))
		} else {
			shell = strings.TrimSpace(os.Getenv("SHELL"))
		}
	}

	isGit := false
	if root != "" {
		if st, err := os.Stat(filepath.Join(root, ".git")); err == nil && st != nil {
			isGit = true
		}
	}

	return WorkspaceSnapshot{
		OS:            osID,
		Shell:         shell,
		WorkspaceRoot: root,
		IsGitRepo:     isGit,
		ActiveFile:    strings.TrimSpace(os.Getenv("ORCH_ACTIVE_FILE")),
		CursorLine:    parseIntEnv("ORCH_CURSOR_LINE"),
		CursorCol:     parseIntEnv("ORCH_CURSOR_COL"),
		OpenFiles:     splitListEnv("ORCH_OPEN_FILES"),
		TerminalsPath: strings.TrimSpace(os.Getenv("ORCH_TERMINALS_PATH")),
		ChangedFiles:  splitListEnv("ORCH_CHANGED_FILES"),
	}
}

func parseIntEnv(key string) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func splitListEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
