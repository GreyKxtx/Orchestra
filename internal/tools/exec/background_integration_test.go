package exec

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T) (*BackgroundRegistry, string) {
	t.Helper()
	return NewBackgroundRegistry(), t.TempDir()
}

func echoCmd(text string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "echo " + text}
	}
	return "sh", []string{"-c", "echo " + text}
}

func longRunningCmd() (string, []string) {
	if runtime.GOOS == "windows" {
		return "ping", []string{"-n", "30", "127.0.0.1"}
	}
	return "sleep", []string{"30"}
}

var _ = fmt.Sprintf

func waitBgDone(t *testing.T, bg *BackgroundRegistry, bgID string, timeout time.Duration) {
	t.Helper()
	if err := bg.WaitDone(bgID, timeout); err != nil {
		t.Fatal(err)
	}
}

func TestExecBashBackground_StartAndComplete(t *testing.T) {
	bg, root := newTestRegistry(t)
	cmd, args := echoCmd("hello-bg")

	resp, err := bg.SpawnBackground(context.Background(), BashBackgroundRequest{
		Command: cmd, Args: args, Workdir: root,
	})
	if err != nil {
		t.Fatalf("SpawnBackground: %v", err)
	}
	if !strings.HasPrefix(resp.BgID, "bg_") {
		t.Errorf("BgID: %q", resp.BgID)
	}
	if resp.Status != "running" {
		t.Errorf("initial status: %q", resp.Status)
	}

	waitBgDone(t, bg, resp.BgID, 5*time.Second)

	out, err := bg.BashOutput(BashOutputRequest{BgID: resp.BgID})
	if err != nil {
		t.Fatalf("BashOutput: %v", err)
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
	bg, root := newTestRegistry(t)
	cmd, args := echoCmd("payload-1")
	resp, err := bg.SpawnBackground(context.Background(), BashBackgroundRequest{Command: cmd, Args: args, Workdir: root})
	if err != nil {
		t.Fatal(err)
	}
	waitBgDone(t, bg, resp.BgID, 5*time.Second)

	first, _ := bg.BashOutput(BashOutputRequest{BgID: resp.BgID})
	if !strings.Contains(first.Stdout, "payload-1") {
		t.Errorf("first call missing payload: %q", first.Stdout)
	}
	second, _ := bg.BashOutput(BashOutputRequest{BgID: resp.BgID})
	if second.Stdout != "" {
		t.Errorf("expected empty stdout after cursor advance, got %q", second.Stdout)
	}
}

func TestExecBashOutput_PeekDoesNotAdvance(t *testing.T) {
	bg, root := newTestRegistry(t)
	cmd, args := echoCmd("peek-payload")
	resp, _ := bg.SpawnBackground(context.Background(), BashBackgroundRequest{Command: cmd, Args: args, Workdir: root})
	waitBgDone(t, bg, resp.BgID, 5*time.Second)

	for i := 0; i < 3; i++ {
		out, _ := bg.BashOutput(BashOutputRequest{BgID: resp.BgID, Peek: true})
		if !strings.Contains(out.Stdout, "peek-payload") {
			t.Errorf("peek %d lost payload: %q", i, out.Stdout)
		}
	}
}

func TestExecBashOutput_UnknownID(t *testing.T) {
	bg, _ := newTestRegistry(t)
	_, err := bg.BashOutput(BashOutputRequest{BgID: "bg_nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown bg_id")
	}
}

func TestExecBashKill_Running(t *testing.T) {
	bg, root := newTestRegistry(t)
	cmd, args := longRunningCmd()
	resp, err := bg.SpawnBackground(context.Background(), BashBackgroundRequest{Command: cmd, Args: args, Workdir: root})
	if err != nil {
		t.Fatal(err)
	}
	killResp, err := bg.BashKill(BashKillRequest{BgID: resp.BgID})
	if err != nil {
		t.Fatalf("BashKill: %v", err)
	}
	if killResp.Status != "killed" {
		t.Errorf("status after kill: %q want killed", killResp.Status)
	}
}

func TestExecBashKill_AlreadyDone(t *testing.T) {
	bg, root := newTestRegistry(t)
	cmd, args := echoCmd("quick")
	resp, _ := bg.SpawnBackground(context.Background(), BashBackgroundRequest{Command: cmd, Args: args, Workdir: root})
	waitBgDone(t, bg, resp.BgID, 5*time.Second)

	killResp, err := bg.BashKill(BashKillRequest{BgID: resp.BgID})
	if err != nil {
		t.Fatalf("BashKill: %v", err)
	}
	if !strings.Contains(killResp.Message, "already") {
		t.Errorf("message: %q", killResp.Message)
	}
}

func TestBackgroundRegistry_StopAll_KillsRunning(t *testing.T) {
	bg, root := newTestRegistry(t)
	cmd, args := longRunningCmd()
	resp, err := bg.SpawnBackground(context.Background(), BashBackgroundRequest{Command: cmd, Args: args, Workdir: root})
	if err != nil {
		t.Fatal(err)
	}
	done, err := bg.DoneCh(resp.BgID)
	if err != nil {
		t.Fatal(err)
	}
	bg.StopAll()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("StopAll() did not kill background process")
	}
}

func TestExecBashBackground_EmptyCommand(t *testing.T) {
	bg, root := newTestRegistry(t)
	_, err := bg.SpawnBackground(context.Background(), BashBackgroundRequest{Command: "  ", Workdir: root})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}
