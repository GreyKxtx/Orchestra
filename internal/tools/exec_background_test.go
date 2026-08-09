package tools

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestRunner(t *testing.T) *Runner {
	t.Helper()
	r, err := NewRunner(t.TempDir(), RunnerOptions{
		ExecTimeout:     5 * time.Second,
		ExecOutputLimit: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func echoCmd(text string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "echo " + text}
	}
	return "sh", []string{"-c", "echo " + text}
}

// longRunningCmd returns a process that will run for ~30s; used to test
// the kill path. Prefer a leaf binary (no shell) so Process.Kill /
// KillProcessTree hits the sleeper directly.
func longRunningCmd() (string, []string) {
	if runtime.GOOS == "windows" {
		return "ping", []string{"-n", "30", "127.0.0.1"}
	}
	return "sleep", []string{"30"}
}

var _ = fmt.Sprintf // keep fmt import when above helper is reverted

func waitBgDone(t *testing.T, r *Runner, bgID string, timeout time.Duration) {
	t.Helper()
	if err := r.bg.WaitDone(bgID, timeout); err != nil {
		t.Fatal(err)
	}
}

func TestExecBashBackground_StartAndComplete(t *testing.T) {
	r := newTestRunner(t)
	cmd, args := echoCmd("hello-bg")

	resp, err := r.ExecBashBackground(context.Background(), ExecRunRequest{
		Command: cmd, Args: args,
	})
	if err != nil {
		t.Fatalf("ExecBashBackground: %v", err)
	}
	if !strings.HasPrefix(resp.BgID, "bg_") {
		t.Errorf("BgID: %q", resp.BgID)
	}
	if resp.Status != "running" {
		t.Errorf("initial status: %q", resp.Status)
	}

	waitBgDone(t, r, resp.BgID, 5*time.Second)

	out, err := r.ExecBashOutput(context.Background(), ExecBashOutputRequest{BgID: resp.BgID})
	if err != nil {
		t.Fatalf("ExecBashOutput: %v", err)
	}
	if out.Status != "done" {
		t.Errorf("status: %q want done", out.Status)
	}
	if out.ExitCode == nil || *out.ExitCode != 0 {
		t.Errorf("exit code: %v", out.ExitCode)
	}
	if !strings.Contains(out.Stdout, "hello-bg") {
		t.Errorf("stdout missing payload: %q", out.Stdout)
	}
}

func TestExecBashOutput_CursorAdvances(t *testing.T) {
	r := newTestRunner(t)
	cmd, args := echoCmd("payload-1")
	resp, err := r.ExecBashBackground(context.Background(), ExecRunRequest{Command: cmd, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	waitBgDone(t, r, resp.BgID, 5*time.Second)

	first, _ := r.ExecBashOutput(context.Background(), ExecBashOutputRequest{BgID: resp.BgID})
	if !strings.Contains(first.Stdout, "payload-1") {
		t.Errorf("first call missing payload: %q", first.Stdout)
	}
	second, _ := r.ExecBashOutput(context.Background(), ExecBashOutputRequest{BgID: resp.BgID})
	if second.Stdout != "" {
		t.Errorf("expected empty stdout after cursor advance, got %q", second.Stdout)
	}
}

func TestExecBashOutput_PeekDoesNotAdvance(t *testing.T) {
	r := newTestRunner(t)
	cmd, args := echoCmd("peek-payload")
	resp, _ := r.ExecBashBackground(context.Background(), ExecRunRequest{Command: cmd, Args: args})
	waitBgDone(t, r, resp.BgID, 5*time.Second)

	for i := 0; i < 3; i++ {
		out, _ := r.ExecBashOutput(context.Background(), ExecBashOutputRequest{BgID: resp.BgID, Peek: true})
		if !strings.Contains(out.Stdout, "peek-payload") {
			t.Errorf("peek %d lost payload: %q", i, out.Stdout)
		}
	}
}

func TestExecBashOutput_UnknownID(t *testing.T) {
	r := newTestRunner(t)
	_, err := r.ExecBashOutput(context.Background(), ExecBashOutputRequest{BgID: "bg_nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown bg_id")
	}
}

func TestExecBashKill_Running(t *testing.T) {
	r := newTestRunner(t)
	cmd, args := longRunningCmd()
	resp, err := r.ExecBashBackground(context.Background(), ExecRunRequest{Command: cmd, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	killResp, err := r.ExecBashKill(context.Background(), ExecBashKillRequest{BgID: resp.BgID})
	if err != nil {
		t.Fatalf("ExecBashKill: %v", err)
	}
	if killResp.Status != "killed" {
		t.Errorf("status after kill: %q want killed", killResp.Status)
	}
}

func TestExecBashKill_AlreadyDone(t *testing.T) {
	r := newTestRunner(t)
	cmd, args := echoCmd("quick")
	resp, _ := r.ExecBashBackground(context.Background(), ExecRunRequest{Command: cmd, Args: args})
	waitBgDone(t, r, resp.BgID, 5*time.Second)

	killResp, err := r.ExecBashKill(context.Background(), ExecBashKillRequest{BgID: resp.BgID})
	if err != nil {
		t.Fatalf("ExecBashKill: %v", err)
	}
	if !strings.Contains(killResp.Message, "already") {
		t.Errorf("message: %q", killResp.Message)
	}
}

func TestRunnerClose_KillsBackgroundProcs(t *testing.T) {
	r := newTestRunner(t)
	cmd, args := longRunningCmd()
	resp, err := r.ExecBashBackground(context.Background(), ExecRunRequest{Command: cmd, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	done, err := r.bg.DoneCh(resp.BgID)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Close() did not kill background process")
	}
}

func TestExecBashBackground_EmptyCommand(t *testing.T) {
	r := newTestRunner(t)
	_, err := r.ExecBashBackground(context.Background(), ExecRunRequest{Command: "  "})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}
