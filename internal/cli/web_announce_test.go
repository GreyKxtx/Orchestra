package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is not a test: when ORCH_WEB_HELPER=1 it runs `orchestra
// web` with the arguments after "--" and exits with its status.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("ORCH_WEB_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		os.Stderr.WriteString("helper: " + err.Error() + "\n")
		os.Exit(2)
	}
	os.Exit(0)
}

// Under --announce, stdout carries exactly one line — the discovery JSON — and
// nothing else, even though --init on a bare folder prints several messages
// (they must land on stderr). Closing stdin must end the process cleanly.
func TestWebAnnounce_StdoutIsExactlyOneLine(t *testing.T) {
	bare := t.TempDir()
	home := t.TempDir() // the helper must not touch the real ~/.orchestra

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--",
		"web", "--workspace-root", bare, "--no-open", "--port", "0", "--init", "--announce")
	cmd.Env = append(os.Environ(), "ORCH_WEB_HELPER=1", "USERPROFILE="+home, "HOME="+home)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	var first string
	select {
	case first = <-lines:
	case <-time.After(30 * time.Second):
		t.Fatalf("no announce line within 30s; stderr:\n%s", stderr.String())
	}
	var d webDiscovery
	if err := json.Unmarshal([]byte(first), &d); err != nil {
		t.Fatalf("first stdout line is not the discovery JSON: %v\n%q", err, first)
	}
	if d.URL == "" || d.Token == "" {
		t.Fatalf("announce lacks url/token: %+v", d)
	}
	if _, err := os.Stat(filepath.Join(bare, ".orchestra.yml")); err != nil {
		t.Fatalf("--init did not create the config: %v", err)
	}

	_ = stdin.Close()
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("helper exited with error after stdin EOF: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("helper did not exit after stdin EOF; stderr:\n%s", stderr.String())
	}

	var extra []string
	for l := range lines {
		extra = append(extra, l)
	}
	if len(extra) != 0 {
		t.Fatalf("stdout carried %d extra line(s) besides the announce: %q", len(extra), extra)
	}
	// The one thing that must survive: the test binary's own "PASS" is printed
	// by the testing package only when the helper returns from the test
	// function — os.Exit above prevents it. If this assertion ever fires with
	// a "PASS" line, the helper stopped exiting early.
}
