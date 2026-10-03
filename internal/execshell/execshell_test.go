package execshell

import (
	"errors"
	"path/filepath"
	"testing"
)

type fakeMachine struct {
	env   map[string]string
	git   string
	files map[string]bool
}

func (m fakeMachine) getenv(k string) string { return m.env[k] }
func (m fakeMachine) lookPath(name string) (string, error) {
	if name == "git" && m.git != "" {
		return m.git, nil
	}
	return "", errors.New("not found")
}
func (m fakeMachine) exists(p string) bool { return m.files[p] }

func (m fakeMachine) resolve(goos string) Shell {
	return resolve(goos, m.getenv, m.lookPath, m.exists)
}

func TestResolve_NotWindowsIsSh(t *testing.T) {
	if s := (fakeMachine{}).resolve("linux"); s.Kind != KindSh || s.Path != "sh" {
		t.Fatalf("got %+v", s)
	}
}

func TestResolve_GitBashBesideTheGitOnPath(t *testing.T) {
	root := filepath.Join("X", "Program Files", "Git")
	bash := filepath.Join(root, "bin", "bash.exe")
	m := fakeMachine{git: filepath.Join(root, "cmd", "git.exe"), files: map[string]bool{bash: true}}
	if s := m.resolve("windows"); s.Kind != KindBash || s.Path != bash {
		t.Fatalf("got %+v, want %s", s, bash)
	}
	if got := (Shell{Path: bash, Kind: KindBash}).Args("echo 'a b'"); len(got) != 2 || got[0] != "-c" || got[1] != "echo 'a b'" {
		t.Fatalf("Args = %v", got)
	}
}

func TestResolve_GitBashWhereTheInstallerPutsIt(t *testing.T) {
	pf := filepath.Join("X", "PF")
	bash := filepath.Join(pf, "Git", "bin", "bash.exe")
	m := fakeMachine{env: map[string]string{"ProgramFiles": pf}, files: map[string]bool{bash: true}}
	if s := m.resolve("windows"); s.Path != bash {
		t.Fatalf("got %+v, want %s", s, bash)
	}
}

// The bash.exe in the Windows directory starts WSL: another machine, another
// filesystem. It is never the shell.
func TestResolve_NeverTheWSLBash(t *testing.T) {
	sys := filepath.Join("X", "Windows")
	wsl := filepath.Join(sys, "System32", "bin", "bash.exe")
	m := fakeMachine{
		env:   map[string]string{"SystemRoot": sys},
		git:   filepath.Join(sys, "System32", "git.exe"),
		files: map[string]bool{wsl: true},
	}
	if s := m.resolve("windows"); s.Kind != KindCmd {
		t.Fatalf("got %+v, want cmd", s)
	}
}

func TestResolve_NoGitBashIsCmd(t *testing.T) {
	s := (fakeMachine{}).resolve("windows")
	if s.Kind != KindCmd || s.Path != "cmd" {
		t.Fatalf("got %+v", s)
	}
	if got := s.Args("dir"); got[0] != "/c" {
		t.Fatalf("Args = %v", got)
	}
}

func TestResolve_TheEnvironmentDecides(t *testing.T) {
	root := filepath.Join("X", "Git")
	bash := filepath.Join(root, "bin", "bash.exe")
	m := fakeMachine{env: map[string]string{"ORCHESTRA_EXEC_SHELL": "cmd"}, git: filepath.Join(root, "cmd", "git.exe"), files: map[string]bool{bash: true}}
	if s := m.resolve("windows"); s.Kind != KindCmd {
		t.Fatalf("ORCHESTRA_EXEC_SHELL=cmd: got %+v", s)
	}
	other := filepath.Join("Y", "bash.exe")
	m = fakeMachine{env: map[string]string{"ORCHESTRA_EXEC_SHELL": other}, files: map[string]bool{other: true}}
	if s := m.resolve("windows"); s.Path != other || s.Kind != KindBash {
		t.Fatalf("ORCHESTRA_EXEC_SHELL=<path>: got %+v", s)
	}
}
