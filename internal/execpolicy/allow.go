// Package execpolicy decides whether a command line the model wants to run is
// covered by the user's exec allowlist, and which environment it runs with.
//
// A command whose text needs a shell runs as `sh -c <line>`, so the allowlist
// has to judge every command the line starts, not the line as one string. It
// used to take filepath.Base of the whole line — the text after its last "/"
// — which let any chain ending in ".../go" pass an allowlist of ["go"], and
// refused the ordinary "go test ./..." whose last segment is "...".
package execpolicy

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// CommandAllowed reports whether every command that command (with args, when
// it runs without a shell) would start is on allow and none is on deny. An
// empty allow list allows nothing. reason says why a line was refused.
func CommandAllowed(command string, args []string, allow, deny []string) (ok bool, reason string) {
	return commandAllowedFor(command, args, allow, deny, runtime.GOOS == "windows")
}

// commandAllowedFor is CommandAllowed for the shell of the given platform.
func commandAllowedFor(command string, args []string, allow, deny []string, windows bool) (ok bool, reason string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return false, "empty command"
	}
	if len(allow) == 0 {
		return false, "no exec.allow list"
	}
	var names []string
	if !NeedsShell(command) {
		names = []string{command}
	} else {
		if len(args) > 0 {
			return false, "a shell line cannot take args"
		}
		cmds, err := commandsFor(command, windows)
		if err != nil {
			return false, err.Error()
		}
		for _, c := range cmds {
			names = append(names, c[0])
		}
	}
	if len(names) == 0 {
		return false, "no command"
	}
	for _, n := range names {
		if strings.ContainsAny(n, `/\`) {
			// A path names a particular file, not the program the allowlist
			// means: "./go" or "tools/go" is whatever the repository put there.
			return false, fmt.Sprintf("%q is a path; the allowlist names programs found on PATH", n)
		}
		base := strings.TrimSuffix(strings.ToLower(n), ".exe")
		if listed(deny, base) {
			return false, fmt.Sprintf("%q is on exec.deny", n)
		}
		if !listed(allow, base) {
			return false, fmt.Sprintf("%q is not on exec.allow", n)
		}
	}
	return true, ""
}

func listed(list []string, base string) bool {
	for _, e := range list {
		e = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(e)), ".exe")
		if e != "" && filepath.Base(e) == base {
			return true
		}
	}
	return false
}

// NeedsShell mirrors the exec tool's dispatch (exec.MaybeShellExec): a
// command containing any of these characters runs through sh -c (cmd /c on
// Windows).
func NeedsShell(command string) bool {
	return strings.ContainsAny(command, " \t|&;<>()*?`$\"'")
}

// dangerousAssignments may not prefix an allowlisted command: each changes
// which program runs or what the shell executes on its own.
var dangerousAssignments = map[string]bool{
	"PATH": true, "IFS": true, "ENV": true, "BASH_ENV": true, "SHELLOPTS": true,
	"BASHOPTS": true, "PS4": true, "PROMPT_COMMAND": true, "CDPATH": true,
	"GIT_SSH": true, "GIT_SSH_COMMAND": true, "GIT_EXEC_PATH": true, "GIT_PAGER": true,
	"PAGER": true, "EDITOR": true, "VISUAL": true,
}

func dangerousAssignment(name string) bool {
	u := strings.ToUpper(name)
	return dangerousAssignments[u] || strings.HasPrefix(u, "LD_") || strings.HasPrefix(u, "DYLD_") ||
		strings.HasPrefix(u, "GIT_CONFIG")
}

// commandsFor splits a shell line into the simple commands it runs, each as
// its words with leading VAR=value assignments removed. It refuses — with an
// error naming the construct — what it cannot see through: command and
// process substitution, subshells and groups, here-documents, a command word
// that is not literal, a dangerous assignment, and output redirection to a
// file (only /dev/null and descriptor duplication are allowed).
//
// It lexes line the way the shell that will run it does: sh on Unix,
// cmd.exe on Windows, where '^' escapes, a backslash and a single quote are
// ordinary characters, and %VAR% / !VAR! expand.
func commandsFor(line string, windows bool) ([][]string, error) {
	var (
		cmds    [][]string
		words   []string
		cur     strings.Builder
		inWord  bool
		quoted  bool // the current word had quotes: never an fd number or "#"
		pending string
	)
	endWord := func() error {
		if !inWord {
			return nil
		}
		w := cur.String()
		cur.Reset()
		inWord, quoted = false, false
		if pending != "" {
			op := pending
			pending = ""
			return checkRedirect(op, w)
		}
		words = append(words, w)
		return nil
	}
	endCommand := func() error {
		if err := endWord(); err != nil {
			return err
		}
		if pending != "" {
			return fmt.Errorf("redirection %q has no target", pending)
		}
		// Drop leading assignments; the first remaining word runs.
		i := 0
		for ; i < len(words); i++ {
			name, _, ok := strings.Cut(words[i], "=")
			if !ok || !isName(name) {
				break
			}
			if dangerousAssignment(name) {
				return fmt.Errorf("assigning %s changes what runs", name)
			}
		}
		if i < len(words) {
			cmd := words[i:]
			notLiteral := "$*?[]{}~"
			if windows {
				notLiteral += "%!"
			}
			if strings.ContainsAny(cmd[0], notLiteral) {
				return fmt.Errorf("command %q is not a literal name", cmd[0])
			}
			cmds = append(cmds, cmd)
		}
		words = nil
		return nil
	}

	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		next := func() rune {
			if i+1 < len(rs) {
				return rs[i+1]
			}
			return 0
		}
		if windows {
			switch c {
			case '\\', '\'':
				cur.WriteRune(c) // ordinary characters to cmd.exe
				inWord = true
				continue
			case '^':
				if i+1 < len(rs) {
					cur.WriteRune(rs[i+1])
					inWord = true
					i++
				}
				continue
			}
		}
		switch {
		case c == '\\':
			if i+1 < len(rs) {
				if rs[i+1] != '\n' {
					cur.WriteRune(rs[i+1])
					inWord = true
				}
				i++
			}
		case c == '\'':
			end := indexRune(rs, i+1, '\'')
			if end < 0 {
				return nil, fmt.Errorf("unterminated single quote")
			}
			cur.WriteString(string(rs[i+1 : end]))
			inWord, quoted = true, true
			i = end
		case c == '"':
			j := i + 1
			for ; j < len(rs) && rs[j] != '"'; j++ {
				switch rs[j] {
				case '\\':
					if j+1 < len(rs) {
						j++
						cur.WriteRune(rs[j])
					}
					continue
				case '`':
					return nil, fmt.Errorf("command substitution")
				case '$':
					if j+1 < len(rs) && rs[j+1] == '(' {
						return nil, fmt.Errorf("command substitution")
					}
				}
				cur.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, fmt.Errorf("unterminated double quote")
			}
			inWord, quoted = true, true
			i = j
		case c == '`':
			return nil, fmt.Errorf("command substitution")
		case c == '$' && next() == '(':
			return nil, fmt.Errorf("command substitution")
		case c == '(' || c == ')':
			return nil, fmt.Errorf("subshell")
		case c == '#' && !inWord:
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			i--
		case c == ' ' || c == '\t':
			if err := endWord(); err != nil {
				return nil, err
			}
		case c == ';' || c == '\n' || c == '|' || c == '&':
			if c == '&' && next() == '>' {
				// &> / &>> redirect stdout and stderr together.
				if err := endWord(); err != nil {
					return nil, err
				}
				i++
				op := "&>"
				if next() == '>' {
					op = "&>>"
					i++
				}
				pending = op
				continue
			}
			if err := endCommand(); err != nil {
				return nil, err
			}
			if (c == '|' || c == '&') && next() == c {
				i++ // && and ||
			} else if c == '|' && next() == '&' {
				i++ // |&
			}
		case c == '<' || c == '>':
			// A word of digits right before the operator is its fd.
			if inWord && !quoted && isDigits(cur.String()) {
				cur.Reset()
				inWord = false
			} else if err := endWord(); err != nil {
				return nil, err
			}
			if pending != "" {
				return nil, fmt.Errorf("redirection %q has no target", pending)
			}
			op := string(c)
			for i+1 < len(rs) && (rs[i+1] == '>' || rs[i+1] == '<' || rs[i+1] == '&' || rs[i+1] == '|') {
				i++
				op += string(rs[i])
			}
			if next() == '(' {
				return nil, fmt.Errorf("process substitution")
			}
			if strings.HasPrefix(op, "<<") {
				if op == "<<<" {
					pending = op
					continue
				}
				return nil, fmt.Errorf("here-document")
			}
			pending = op
		default:
			cur.WriteRune(c)
			inWord = true
		}
	}
	if err := endCommand(); err != nil {
		return nil, err
	}
	return cmds, nil
}

// checkRedirect admits input redirection and here-strings, output only to
// /dev/null or another descriptor. Writing a file is what edit and write are
// for; through an allowlisted command it would skip every check they make.
func checkRedirect(op, target string) error {
	switch {
	case op == "<" || op == "<<<":
		return nil
	case strings.HasSuffix(op, "&") && (isDigits(target) || target == "-"):
		return nil // 2>&1, >&2, <&0, >&-
	case target == "/dev/null":
		return nil
	}
	return fmt.Errorf("redirection %s %s writes a file", op, target)
}

func isName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func indexRune(rs []rune, from int, r rune) int {
	for i := from; i < len(rs); i++ {
		if rs[i] == r {
			return i
		}
	}
	return -1
}
