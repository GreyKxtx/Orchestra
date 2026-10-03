// Package execshell picks the shell a command line of the bash tool runs in.
//
// The tool is called bash and models write bash: '…' strings, \" inside
// double quotes, &&, $VAR, pipes into grep. On Windows that line went to
// cmd.exe, whose quoting is different — live, every `node -e "…"` a model
// wrote failed, and the model spent its steps rewriting the check. Where Git
// for Windows is installed its bash runs the line, as it does for Claude Code
// on Windows; without it cmd.exe still does, and the model is told so.
package execshell

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Kind names the shell's language.
const (
	KindSh   = "sh"   // POSIX sh (Linux, macOS)
	KindBash = "bash" // Git Bash on Windows
	KindCmd  = "cmd"  // cmd.exe on Windows without Git Bash
)

// Shell is a shell a command line can be handed to.
type Shell struct {
	Path string // what to execute
	Kind string // KindSh, KindBash or KindCmd
}

// Args are the arguments that run line in the shell.
func (s Shell) Args(line string) []string {
	if s.Kind == KindCmd {
		return []string{"/c", line}
	}
	return []string{"-c", line}
}

// Describe is the shell as the model is told about it.
func (s Shell) Describe() string {
	switch s.Kind {
	case KindBash:
		return "bash (Git Bash: " + s.Path + "; write paths with forward slashes, or quote a Windows path)"
	case KindCmd:
		return "cmd.exe (no Git Bash found — Windows cmd quoting: double quotes only, no \\\" escapes, no single-quoted strings)"
	}
	return "sh"
}

var (
	once   sync.Once
	cached Shell
)

// Default is the shell for this machine, found once per process.
// ORCHESTRA_EXEC_SHELL=cmd keeps cmd.exe on Windows; a path to a bash.exe
// names the bash to use.
func Default() Shell {
	once.Do(func() { cached = resolve(runtime.GOOS, os.Getenv, exec.LookPath, fileExists) })
	return cached
}

func resolve(goos string, getenv func(string) string, lookPath func(string) (string, error), exists func(string) bool) Shell {
	if goos != "windows" {
		return Shell{Path: "sh", Kind: KindSh}
	}
	cmd := Shell{Path: "cmd", Kind: KindCmd}
	switch v := strings.TrimSpace(getenv("ORCHESTRA_EXEC_SHELL")); {
	case strings.EqualFold(v, "cmd"):
		return cmd
	case v != "" && exists(v):
		return Shell{Path: v, Kind: KindBash}
	}
	if p := gitBash(getenv, lookPath, exists); p != "" {
		return Shell{Path: p, Kind: KindBash}
	}
	return cmd
}

// gitBash finds Git for Windows' bash.exe: beside the git on PATH, then where
// the installer puts it. Never the bash.exe in the Windows directory: that one
// starts WSL, a different machine with a different filesystem.
func gitBash(getenv func(string) string, lookPath func(string) (string, error), exists func(string) bool) string {
	var roots []string
	if git, err := lookPath("git"); err == nil {
		// <root>\cmd\git.exe, <root>\bin\git.exe, <root>\mingw64\bin\git.exe
		dir := filepath.Dir(git)
		for i := 0; i < 3; i++ {
			roots = append(roots, dir)
			dir = filepath.Dir(dir)
		}
	}
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if v := getenv(env); v != "" {
			roots = append(roots, filepath.Join(v, "Git"))
		}
	}
	if v := getenv("LOCALAPPDATA"); v != "" {
		roots = append(roots, filepath.Join(v, "Programs", "Git"))
	}
	sysRoot := strings.ToLower(getenv("SystemRoot"))
	for _, root := range roots {
		p := filepath.Join(root, "bin", "bash.exe")
		if sysRoot != "" && strings.HasPrefix(strings.ToLower(p), sysRoot) {
			continue
		}
		if exists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
