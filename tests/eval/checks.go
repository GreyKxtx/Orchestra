package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Substring checks cannot tell work from something that looks like work.
//
// Every one of these passed a `file_contains` suite while being wrong, and
// each came out of a real run against a local model:
//
//   - a test function containing no assertion at all;
//   - a rename applied in the file that defines the symbol and not in the one
//     that calls it, so the workspace no longer compiles;
//   - the broken function deleted rather than fixed — every "must not
//     contain" check then passes, because the text really is gone;
//   - a 300-line file truncated to its constants, which still contains every
//     string the checks looked for;
//   - an int concatenated onto a string, which reads correctly and does not
//     compile.
//
// The checks below are the ones that catch those: they compile the workspace,
// run its tests, match shapes rather than substrings, and compare a file
// against the bytes the task itself wrote.

// toolchainTimeout caps `go build` and `go test`. A task that hangs the
// toolchain is a failure to report, not a run to wait out.
const toolchainTimeout = 2 * time.Minute

// checkEnv is what a check is evaluated against: the workspace as the agent
// left it, and the files as the task wrote them.
type checkEnv struct {
	root string
	// original holds the task's own Files, so a check can prove the agent
	// left something alone. Keyed by slash-separated relative path.
	original map[string]string
	// answer is the prose the model produced, for the checks that grade what
	// it said rather than what it wrote.
	answer string
}

func (e checkEnv) abs(rel string) string {
	return filepath.Join(e.root, filepath.FromSlash(rel))
}

// runGo runs one go subcommand in the workspace and returns its combined
// output. A missing toolchain is reported, never silently treated as success:
// "go build passes" is a claim, and a harness that cannot run the compiler has
// not established it.
func runGo(root string, args ...string) (string, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return "", fmt.Errorf("go is not on PATH, so this check cannot be established: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolchainTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	// A workspace the agent just wrote is not trusted to have a sane
	// environment; inherit the parent's but pin the module to the workspace.
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("go %s timed out after %s", strings.Join(args, " "), toolchainTimeout)
	}
	return string(out), err
}

// firstToolchainError picks the line a reader needs out of go's output. The
// first line is often just the package banner ("# evalws"), which names
// nothing about what went wrong.
func firstToolchainError(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return strings.TrimSpace(out)
}

// evaluateMechanicalCheck handles the check types that go beyond substrings.
// It returns ("", false) when the type is not one of its own, so the caller
// can fall through to the simple checks.
func evaluateMechanicalCheck(env checkEnv, c Check) (string, bool) {
	switch c.Type {
	case "go_build":
		pattern := c.Content
		if pattern == "" {
			pattern = "./..."
		}
		if out, err := runGo(env.root, "build", pattern); err != nil {
			return fmt.Sprintf("go_build: %s (%v)", firstToolchainError(out), err), true
		}
		return "", true

	case "go_test":
		pattern := c.Content
		if pattern == "" {
			pattern = "./..."
		}
		if out, err := runGo(env.root, "test", pattern); err != nil {
			return fmt.Sprintf("go_test: %s (%v)", firstToolchainError(out), err), true
		}
		return "", true

	case "file_matches", "file_not_matches":
		re, err := regexp.Compile(c.Pattern)
		if err != nil {
			return fmt.Sprintf("%s %q: bad pattern %q: %v", c.Type, c.Path, c.Pattern, err), true
		}
		data, err := os.ReadFile(env.abs(c.Path))
		if err != nil {
			return fmt.Sprintf("%s %q: read error: %v", c.Type, c.Path, err), true
		}
		matched := re.Match(data)
		if c.Type == "file_matches" && !matched {
			return fmt.Sprintf("file_matches %q: nothing matches %q", c.Path, c.Pattern), true
		}
		if c.Type == "file_not_matches" && matched {
			return fmt.Sprintf("file_not_matches %q: %q matches but should not", c.Path, c.Pattern), true
		}
		return "", true

	case "file_unchanged":
		want, ok := env.original[c.Path]
		if !ok {
			// A task cannot assert that a file it never wrote was left alone;
			// saying "unchanged" about nothing would pass for free.
			return fmt.Sprintf("file_unchanged %q: the task does not define this file, so there is nothing to compare against", c.Path), true
		}
		data, err := os.ReadFile(env.abs(c.Path))
		if err != nil {
			return fmt.Sprintf("file_unchanged %q: read error: %v", c.Path, err), true
		}
		if string(data) != want {
			return fmt.Sprintf("file_unchanged %q: the agent modified it", c.Path), true
		}
		return "", true

	case "answer_matches", "answer_not_matches":
		re, err := regexp.Compile(c.Pattern)
		if err != nil {
			return fmt.Sprintf("%s: bad pattern %q: %v", c.Type, c.Pattern, err), true
		}
		matched := re.MatchString(env.answer)
		if c.Type == "answer_matches" && !matched {
			return fmt.Sprintf("answer_matches: nothing in the answer matches %q; it said: %s",
				c.Pattern, excerpt(env.answer)), true
		}
		if c.Type == "answer_not_matches" && matched {
			return fmt.Sprintf("answer_not_matches: %q matches but should not; it said: %s",
				c.Pattern, excerpt(env.answer)), true
		}
		return "", true

	case "answer_not_empty":
		if strings.TrimSpace(env.answer) == "" {
			return "answer_not_empty: the model said nothing", true
		}
		return "", true

	case "answer_invents_no_path":
		// The failure this catches is the one that reads best: a confident
		// description of directories the project does not have. It cost a
		// whole turn once — the model named a path, then spent five tool
		// calls trying to read variations of it.
		if invented := inventedPaths(env, env.answer); len(invented) > 0 {
			return fmt.Sprintf("answer_invents_no_path: named %s, which the workspace does not have",
				strings.Join(invented, ", ")), true
		}
		return "", true

	case "workspace_unchanged":
		// Every file the task wrote, still as it was: what a read-only task
		// needs, and what no per-file check can state in one line.
		for rel, want := range env.original {
			data, err := os.ReadFile(env.abs(rel))
			if err != nil {
				return fmt.Sprintf("workspace_unchanged: %q: read error: %v", rel, err), true
			}
			if string(data) != want {
				return fmt.Sprintf("workspace_unchanged: the agent modified %q", rel), true
			}
		}
		return "", true
	}
	return "", false
}

// pathLike matches the path-shaped tokens in prose: a/b, a/b.go, dir/. Bare
// words are not candidates — "the internal package" names no path.
var pathLike = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+/?`)

// inventedPaths returns the path-shaped things the answer names that the
// workspace does not contain.
//
// Deliberately conservative. It only judges a token that looks like a path
// AND whose first segment is a real entry of the workspace — so "net/http",
// "github.com/x/y" and "encoding/json" are not counted as inventions, while
// "internal/telemetry" in a project whose internal/ holds only store is.
// A check that cries wolf on import paths would be turned off within a day.
func inventedPaths(env checkEnv, answer string) []string {
	roots := map[string]bool{}
	entries, err := os.ReadDir(env.root)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		roots[e.Name()] = true
	}
	seen := map[string]bool{}
	var bad []string
	for _, tok := range pathLike.FindAllString(answer, -1) {
		clean := strings.TrimSuffix(tok, "/")
		first := clean
		if i := strings.Index(clean, "/"); i >= 0 {
			first = clean[:i]
		}
		if !roots[first] || seen[clean] {
			continue
		}
		seen[clean] = true
		if _, statErr := os.Stat(env.abs(clean)); statErr != nil {
			bad = append(bad, clean)
		}
	}
	return bad
}

// excerpt trims an answer down to something a failure line can carry.
func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		return s[:160] + "…"
	}
	if s == "" {
		return "(nothing)"
	}
	return s
}
