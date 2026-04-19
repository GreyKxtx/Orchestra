package tools

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
)

type execOutputCBKey struct{}

// WithExecOutputCallback returns a context that carries a chunk callback for exec.run streaming.
// The callback receives raw output chunks (stdout and stderr interleaved) as they are produced.
func WithExecOutputCallback(ctx context.Context, cb func(chunk string)) context.Context {
	return context.WithValue(ctx, execOutputCBKey{}, cb)
}

func execOutputCBFromCtx(ctx context.Context) func(string) {
	cb, _ := ctx.Value(execOutputCBKey{}).(func(string))
	return cb
}

func runExec(parent context.Context, workspaceRoot string, defaultTimeout time.Duration, defaultOutputLimit int, req ExecRunRequest) (*ExecRunResponse, error) {
	cmdName := strings.TrimSpace(req.Command)
	if cmdName == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "command is empty", nil)
	}

	workdir := strings.TrimSpace(req.Workdir)
	absDir := workspaceRoot
	if workdir != "" {
		p, _, err := resolveWorkspacePath(workspaceRoot, workdir)
		if err != nil {
			return nil, err
		}
		absDir = p
	}
	// Ensure the directory exists and is inside the workspace.
	if st, err := os.Stat(absDir); err != nil || !st.IsDir() {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "workdir does not exist", map[string]any{
			"workdir": workdir,
		})
	}

	timeout := defaultTimeout
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}

	limit := defaultOutputLimit
	if req.OutputLimitKB > 0 {
		limit = req.OutputLimitKB * 1024
	}
	if limit <= 0 {
		limit = 100 * 1024
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	start := time.Now()

	// Non-interactive: stdin is nil (reads as EOF).
	cmd := exec.CommandContext(ctx, cmdName, req.Args...)
	cmd.Dir = absDir
	cmd.Stdin = nil

	streamCB := execOutputCBFromCtx(parent)
	lim := &outputLimiter{limit: limit}
	stdoutBuf := &limitedBuffer{lim: lim, cb: streamCB}
	stderrBuf := &limitedBuffer{lim: lim, cb: streamCB}

	var err error
	if streamCB != nil {
		// Streaming path: use pipes so output is forwarded incrementally.
		stdoutPipe, pipeErr := cmd.StdoutPipe()
		if pipeErr != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to create stdout pipe", map[string]any{"error": pipeErr.Error()})
		}
		stderrPipe, pipeErr := cmd.StderrPipe()
		if pipeErr != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to create stderr pipe", map[string]any{"error": pipeErr.Error()})
		}
		if startErr := cmd.Start(); startErr != nil {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to start command", map[string]any{
				"error":   startErr.Error(),
				"command": cmdName,
				"args":    req.Args,
				"workdir": filepath.ToSlash(workdir),
			})
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = io.Copy(stdoutBuf, stdoutPipe) }()
		go func() { defer wg.Done(); _, _ = io.Copy(stderrBuf, stderrPipe) }()
		wg.Wait()
		err = cmd.Wait()
	} else {
		cmd.Stdout = stdoutBuf
		cmd.Stderr = stderrBuf
		err = cmd.Run()
	}
	dur := time.Since(start).Milliseconds()

	// Process exit code handling: non-zero exit is not a transport error.
	exitCode := 0
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, protocol.NewError(protocol.ExecTimeout, "command timed out", map[string]any{
				"command":   cmdName,
				"args":      req.Args,
				"workdir":   filepath.ToSlash(workdir),
				"timeoutMs": int(timeout.Milliseconds()),
				"stdout":    stdoutBuf.String(),
				"stderr":    stderrBuf.String(),
				"truncated": lim.truncated,
				"duration":  dur,
			})
		}

		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return nil, protocol.NewError(protocol.ExecFailed, "failed to run command", map[string]any{
				"error":     err.Error(),
				"command":   cmdName,
				"args":      req.Args,
				"workdir":   filepath.ToSlash(workdir),
				"stdout":    stdoutBuf.String(),
				"stderr":    stderrBuf.String(),
				"truncated": lim.truncated,
				"duration":  dur,
			})
		}
	}

	return &ExecRunResponse{
		ExitCode:   exitCode,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		DurationMS: dur,
		Truncated:  lim.truncated,
	}, nil
}

type outputLimiter struct {
	mu        sync.Mutex
	limit     int
	used      int
	truncated bool
}

type limitedBuffer struct {
	lim *outputLimiter
	b   strings.Builder
	cb  func(string) // optional streaming callback; called for each accepted chunk
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	if w.lim == nil {
		_, _ = w.b.Write(p)
		if w.cb != nil {
			w.cb(string(p))
		}
		return len(p), nil
	}

	w.lim.mu.Lock()
	remaining := w.lim.limit - w.lim.used
	take := len(p)
	if remaining <= 0 {
		w.lim.truncated = true
		w.lim.mu.Unlock()
		return len(p), nil
	}
	if take > remaining {
		take = remaining
		w.lim.truncated = true
	}
	w.lim.used += take
	w.lim.mu.Unlock()

	if take > 0 {
		chunk := string(p[:take])
		_, _ = w.b.WriteString(chunk)
		if w.cb != nil {
			w.cb(chunk)
		}
	}
	return len(p), nil
}

func (w *limitedBuffer) String() string {
	if w == nil {
		return ""
	}
	return w.b.String()
}
