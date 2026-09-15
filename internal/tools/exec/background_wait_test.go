package exec

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func lateEchoCmd(text string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "ping -n 3 127.0.0.1 >nul & echo " + text}
	}
	return "sh", []string{"-c", "sleep 2; echo " + text}
}

// bash.output answered at once, so a model waiting on a background job polled
// it as fast as it could send calls — seen in the eval: four identical calls
// within three seconds, each "running" with no output, until the duplicate
// guard stopped the turn. A poll on a running process with nothing new now
// waits for output or for the process to end, up to a bound.
func TestBashOutput_WaitsForOutputFromARunningProcess(t *testing.T) {
	bg, root := newTestRegistry(t)
	cmd, args := lateEchoCmd("late-payload")
	resp, err := bg.SpawnBackground(context.Background(), BashBackgroundRequest{Command: cmd, Args: args, Workdir: root})
	if err != nil {
		t.Fatal(err)
	}

	out, err := bg.BashOutput(BashOutputRequest{BgID: resp.BgID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Stdout, "late-payload") && out.Status == "running" {
		t.Errorf("bash.output came back running with nothing instead of waiting for the output: %+v", out)
	}
}

// The wait is bounded: a process that stays silent returns "running" after it.
func TestBashOutput_WaitIsBounded(t *testing.T) {
	bg, root := newTestRegistry(t)
	bg.outputWait = 300 * time.Millisecond
	cmd, args := longRunningCmd()
	if runtime.GOOS == "windows" {
		// ping prints every second; a silent command keeps this about the bound.
		cmd, args = "powershell", []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 30"}
	}
	resp, err := bg.SpawnBackground(context.Background(), BashBackgroundRequest{Command: cmd, Args: args, Workdir: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = bg.BashKill(BashKillRequest{BgID: resp.BgID}) })

	start := time.Now()
	out, err := bg.BashOutput(BashOutputRequest{BgID: resp.BgID})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("a silent process held bash.output for %s; the wait must be bounded", took)
	}
	if out.Status != "running" {
		t.Errorf("status = %q, want running", out.Status)
	}
}
