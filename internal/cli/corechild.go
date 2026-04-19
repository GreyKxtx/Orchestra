package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/orchestra/orchestra/internal/jsonrpc"
)

// CoreChild holds a running "orchestra core" subprocess and its RPC client.
type CoreChild struct {
	cmd    *exec.Cmd
	Client *jsonrpc.Client
	stdin  io.Closer
	stdout io.Closer
}

// spawnCoreChild starts "orchestra core --workspace-root <root>" as a subprocess
// and returns a CoreChild whose Client communicates with it over stdio.
// The subprocess is associated with ctx: if ctx is cancelled, the OS kills the child.
// Call Close when done to wait for the subprocess to exit gracefully.
func spawnCoreChild(ctx context.Context, workspaceRoot string) (*CoreChild, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}

	child := exec.CommandContext(ctx, exe, "core", "--workspace-root", workspaceRoot)
	child.Stderr = os.Stderr

	stdin, err := child.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := child.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}

	return &CoreChild{
		cmd:    child,
		Client: jsonrpc.NewClient(stdout, stdin),
		stdin:  stdin,
		stdout: stdout,
	}, nil
}

// Close sends EOF to the subprocess stdin and waits up to 2 seconds for it to exit.
func (c *CoreChild) Close() {
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
		<-done
	case <-done:
	}
}
