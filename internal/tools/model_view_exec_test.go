package tools_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/tools"
)

// bash.output and bash.kill were the only tools, with the three gh.* and
// git.worktree.list, that no test in the repository named at all. The registry
// behind them is well covered in internal/tools/exec — cursor, peek, kill,
// StopAll — but always from below, calling BackgroundRegistry directly. Nothing
// had called them the way the model does, through Runner.Call and the bash
// dispatch, and that is exactly the layer where foreground and background bash
// part ways:
//
//   - ExecRun refuses to run in a dry-run preview when BlockExecInDryRun is set;
//     ExecBashBackground never looks.
//   - exec.Run passes the command through MaybeShellExec, so "go test ./..."
//     reaches sh -c / cmd /c; SpawnBackground hands the whole string to
//     exec.CommandContext as a binary name.
//
// These use git, which both CI runners carry, because it is a real executable
// with a side effect a test can look for.

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

// waitBackground polls bash.output the way a model does until the process
// leaves "running", and returns everything it printed.
func waitBackground(t *testing.T, r *tools.Runner, bgID string) (status, stdout, stderr string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var out strings.Builder
	var errOut strings.Builder
	for {
		raw := mustCall(t, r, "bash.output", map[string]any{"bg_id": bgID})
		var resp tools.ExecBashOutputResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("bash.output did not answer with its documented shape: %v\n%s", err, raw)
		}
		out.WriteString(resp.Stdout)
		errOut.WriteString(resp.Stderr)
		if resp.Status != "running" {
			return resp.Status, out.String(), errOut.String()
		}
		if time.Now().After(deadline) {
			t.Fatalf("background process %s still running after 30s", bgID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func startBackground(t *testing.T, r *tools.Runner, args map[string]any) (bgID string, out string, err error) {
	t.Helper()
	args["run_in_background"] = true
	out, err = call(t, r, "bash", args)
	if err != nil {
		return "", out, err
	}
	var resp tools.ExecBashBackgroundResponse
	if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil || resp.BgID == "" {
		t.Fatalf("background bash answered without a bg_id: %v\n%s", jerr, out)
	}
	t.Cleanup(func() { _, _ = call(t, r, "bash.kill", map[string]any{"bg_id": resp.BgID}) })
	return resp.BgID, out, nil
}

// The core promises remote clients — TUI, IDE, web — that a dry-run preview has
// no side effects, and says so where it sets BlockExecInDryRun (core.go). A
// preview that can still run any command by adding run_in_background breaks
// that promise with one extra field.
func TestModelView_BackgroundBashKeepsTheDryRunPromiseForegroundBashKeeps(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{BlockExecInDryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	r.SetDryRun(true)

	// No shell needed, so the result cannot be confused with the missing-shell
	// defect below.
	gitInit := func(dir string) map[string]any {
		return map[string]any{"command": "git", "args": []string{"init", dir}}
	}

	// Control: the block is on, and foreground bash honours it.
	if _, ferr := call(t, r, "bash", gitInit("fg")); ferr == nil {
		t.Fatal("foreground bash ran in a blocked dry-run; the control is broken, not the background path")
	}
	if _, serr := os.Stat(filepath.Join(root, "fg")); serr == nil {
		t.Fatal("foreground bash left a side effect in a blocked dry-run")
	}

	bgID, out, berr := startBackground(t, r, gitInit("bg"))
	if berr == nil {
		waitBackground(t, r, bgID)
	}
	_, statErr := os.Stat(filepath.Join(root, "bg", ".git"))
	if berr == nil || statErr == nil {
		t.Errorf("run_in_background ran a command that plain bash refuses in the same dry-run "+
			"preview (side effect on disk: %v). The block is one field away from meaningless:\n%s",
			statErr == nil, out)
	}
	if berr != nil && !strings.Contains(berr.Error(), "dry-run") {
		t.Errorf("the refusal should say why, the way the foreground one does:\n%s", berr)
	}
}

// The description sends the model to the background for "build, test, dev
// server" — every one of which is a command with arguments in one string. If
// the background path cannot run what foreground bash runs, the advice in the
// description is the bug.
func TestModelView_BackgroundBashRunsTheSameCommandLineForegroundBashRuns(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	const line = "git --version"

	// Control: foreground runs it.
	fg := mustCall(t, r, "bash", map[string]any{"command": line})
	if !strings.Contains(fg, "git version") {
		t.Fatalf("foreground bash did not run %q, so this machine cannot answer the question:\n%s", line, fg)
	}

	bgID, out, berr := startBackground(t, r, map[string]any{"command": line})
	if berr != nil {
		t.Fatalf("background bash could not start %q, a command line foreground bash runs:\n%s", line, out)
	}
	status, stdout, stderr := waitBackground(t, r, bgID)
	if !strings.Contains(stdout, "git version") {
		t.Errorf("background bash did not run %q (status %s):\nstdout: %s\nstderr: %s",
			line, status, stdout, stderr)
	}
}

// bash.kill on a process that is running must stop it and say so; bash.output
// after that must not report it as still running. A model that polls a killed
// dev server forever is the failure this rules out.
func TestModelView_BashKillStopsWhatBashOutputThenReportsStopped(t *testing.T) {
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	// The same long-running command internal/tools/exec's own tests use.
	cmd, args := "sleep", []string{"30"}
	if runtime.GOOS == "windows" {
		cmd, args = "ping", []string{"-n", "30", "127.0.0.1"}
	}
	bgID, out, berr := startBackground(t, r, map[string]any{"command": cmd, "args": args})
	if berr != nil {
		t.Fatalf("could not start a long-running process: %s", out)
	}
	// Without this the test passes on a process that exited by itself.
	if before := mustCall(t, r, "bash.output", map[string]any{"bg_id": bgID, "peek": true}); !strings.Contains(before, `"status":"running"`) {
		t.Fatalf("the process was not running before the kill, so the kill proves nothing:\n%s", before)
	}

	killed := mustCall(t, r, "bash.kill", map[string]any{"bg_id": bgID})
	if !strings.Contains(killed, bgID) {
		t.Errorf("bash.kill's answer does not name the process it acted on:\n%s", killed)
	}
	after := mustCall(t, r, "bash.output", map[string]any{"bg_id": bgID})
	if strings.Contains(after, `"status":"running"`) {
		t.Errorf("bash.output still reports a killed process as running:\nkill: %s\noutput: %s", killed, after)
	}
}
