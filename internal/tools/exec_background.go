package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
)

// bgStatus is the lifecycle state of a background process.
type bgStatus string

const (
	bgRunning  bgStatus = "running"
	bgDone     bgStatus = "done"
	bgKilled   bgStatus = "killed"
	bgError    bgStatus = "error"
	bgTimedOut bgStatus = "timed_out"
)

// bgProcess tracks a single background command launched via bash with
// run_in_background=true. Its stdout/stderr are buffered up to a per-stream
// cap; ExecBashOutput returns content past the per-process read cursor.
type bgProcess struct {
	id      string
	command string
	args    []string
	workdir string
	started time.Time

	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}

	mu         sync.Mutex
	stdoutBuf  strings.Builder
	stderrBuf  strings.Builder
	stdoutOff  int
	stderrOff  int
	stdoutCap  bool // true when buffer was truncated due to cap
	stderrCap  bool
	exitCode   int
	status     bgStatus
	statusMsg  string // populated on error / killed / timed_out
	finishedAt time.Time
}

// bgRegistry holds all background processes for one Runner. Process IDs
// are monotonic ("bg_1", "bg_2", …). When the Runner closes, Stop()
// kills every running process and clears the registry.
type bgRegistry struct {
	mu      sync.Mutex
	seq     uint64
	procs   map[string]*bgProcess
	bufCap  int // per-stream byte cap; 0 → default 256 KB
}

func newBgRegistry() *bgRegistry {
	return &bgRegistry{procs: make(map[string]*bgProcess)}
}

// nextID returns the next monotonic id.
func (r *bgRegistry) nextID() string {
	n := atomic.AddUint64(&r.seq, 1)
	return fmt.Sprintf("bg_%d", n)
}

// add registers a new process under a fresh id.
func (r *bgRegistry) add(p *bgProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.procs[p.id] = p
}

// get returns the process or (nil, false).
func (r *bgRegistry) get(id string) (*bgProcess, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[id]
	return p, ok
}

// stopAll kills every running background process. Called from Runner.Close.
func (r *bgRegistry) stopAll() {
	r.mu.Lock()
	procs := make([]*bgProcess, 0, len(r.procs))
	for _, p := range r.procs {
		procs = append(procs, p)
	}
	r.procs = make(map[string]*bgProcess)
	r.mu.Unlock()
	for _, p := range procs {
		p.cancel()
	}
}

// bgWriter is the io.Writer that feeds a per-stream buffer with a cap.
type bgWriter struct {
	p        *bgProcess
	isStderr bool
	cap      int
}

func (w *bgWriter) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	w.p.mu.Lock()
	defer w.p.mu.Unlock()
	var buf *strings.Builder
	var capped *bool
	if w.isStderr {
		buf = &w.p.stderrBuf
		capped = &w.p.stderrCap
	} else {
		buf = &w.p.stdoutBuf
		capped = &w.p.stdoutCap
	}
	remaining := w.cap - buf.Len()
	if remaining <= 0 {
		*capped = true
		return len(b), nil
	}
	take := len(b)
	if take > remaining {
		take = remaining
		*capped = true
	}
	buf.Write(b[:take])
	return len(b), nil
}

// ExecBashBackgroundRequest is the input for the background spawn path.
type ExecBashBackgroundRequest struct {
	Command   string
	Args      []string
	Workdir   string // absolute, already resolved
	TimeoutMS int    // 0 → no extra deadline beyond the registry-wide cap
}

// ExecBashBackgroundResponse describes a freshly-spawned background job.
type ExecBashBackgroundResponse struct {
	BgID    string `json:"bg_id"`
	Status  string `json:"status"`
	Command string `json:"command"`
	Started string `json:"started"` // RFC3339
}

// spawnBackground starts a process under context cancellation and returns
// its registry entry. The process is wired to the registry-wide stream
// buffer cap; on completion the entry's status/exitCode are updated and
// done is closed.
func (r *bgRegistry) spawnBackground(parent context.Context, req ExecBashBackgroundRequest) (*bgProcess, error) {
	cap := r.bufCap
	if cap <= 0 {
		cap = 256 * 1024
	}

	ctx, cancel := context.WithCancel(parent)
	if req.TimeoutMS > 0 {
		ctx, cancel = context.WithTimeout(parent, time.Duration(req.TimeoutMS)*time.Millisecond)
	}

	cmd := exec.CommandContext(ctx, req.Command, req.Args...)
	cmd.Dir = req.Workdir
	cmd.Stdin = nil

	id := r.nextID()
	p := &bgProcess{
		id:      id,
		command: req.Command,
		args:    append([]string(nil), req.Args...),
		workdir: req.Workdir,
		started: time.Now(),
		cancel:  cancel,
		done:    make(chan struct{}),
		status:  bgRunning,
	}
	cmd.Stdout = &bgWriter{p: p, cap: cap}
	cmd.Stderr = &bgWriter{p: p, isStderr: true, cap: cap}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, protocol.NewError(protocol.ExecFailed, "failed to start background command", map[string]any{
			"error":   err.Error(),
			"command": req.Command,
			"args":    req.Args,
		})
	}
	p.cmd = cmd

	r.add(p)

	go func() {
		defer close(p.done)
		err := cmd.Wait()
		p.mu.Lock()
		p.finishedAt = time.Now()
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			p.status = bgTimedOut
			p.statusMsg = "timed out"
		case errors.Is(ctx.Err(), context.Canceled):
			p.status = bgKilled
			p.statusMsg = "killed"
		case err == nil:
			p.status = bgDone
			p.exitCode = 0
		default:
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				p.status = bgDone
				p.exitCode = ee.ExitCode()
			} else {
				p.status = bgError
				p.statusMsg = err.Error()
				p.exitCode = -1
			}
		}
		p.mu.Unlock()
	}()

	return p, nil
}

// ExecBashOutputRequest fetches new output for a running/finished job.
type ExecBashOutputRequest struct {
	BgID string `json:"bg_id"`
	Peek bool   `json:"peek,omitempty"` // when true, cursor is not advanced
}

// ExecBashOutputResponse is the polled view of a background job.
type ExecBashOutputResponse struct {
	BgID       string `json:"bg_id"`
	Status     string `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	StdoutCap  bool   `json:"stdout_truncated,omitempty"`
	StderrCap  bool   `json:"stderr_truncated,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"` // populated when status != "running"
	StatusMsg  string `json:"status_msg,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// fetchOutput drains new output past the per-process cursor and returns
// the current status snapshot. When peek is true the cursors are left
// untouched.
func (p *bgProcess) fetchOutput(peek bool) *ExecBashOutputResponse {
	p.mu.Lock()
	defer p.mu.Unlock()
	stdoutAll := p.stdoutBuf.String()
	stderrAll := p.stderrBuf.String()
	stdoutNew := stdoutAll[p.stdoutOff:]
	stderrNew := stderrAll[p.stderrOff:]
	if !peek {
		p.stdoutOff = len(stdoutAll)
		p.stderrOff = len(stderrAll)
	}
	resp := &ExecBashOutputResponse{
		BgID:      p.id,
		Status:    string(p.status),
		Stdout:    stdoutNew,
		Stderr:    stderrNew,
		StdoutCap: p.stdoutCap,
		StderrCap: p.stderrCap,
		StatusMsg: p.statusMsg,
	}
	end := time.Now()
	if !p.finishedAt.IsZero() {
		end = p.finishedAt
		code := p.exitCode
		resp.ExitCode = &code
	}
	resp.DurationMS = end.Sub(p.started).Milliseconds()
	return resp
}

// ExecBashKillRequest cancels a running background job.
type ExecBashKillRequest struct {
	BgID string `json:"bg_id"`
}

// ExecBashKillResponse is returned by the kill RPC.
type ExecBashKillResponse struct {
	BgID    string `json:"bg_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ExecBashBackground is the Runner-level entry point invoked by call.go
// when bash is called with run_in_background=true. It resolves the
// workdir (relative to workspace root) and starts the process.
func (r *Runner) ExecBashBackground(ctx context.Context, req ExecRunRequest) (*ExecBashBackgroundResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	if strings.TrimSpace(req.Command) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "command is empty", nil)
	}
	absDir := r.workspaceRoot
	if w := strings.TrimSpace(req.Workdir); w != "" {
		p, _, err := resolveWorkspacePath(r.workspaceRoot, w)
		if err != nil {
			return nil, err
		}
		absDir = p
	}
	if st, err := os.Stat(absDir); err != nil || !st.IsDir() {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "workdir does not exist", map[string]any{
			"workdir": req.Workdir,
		})
	}
	if r.bg == nil {
		r.bg = newBgRegistry()
	}
	p, err := r.bg.spawnBackground(ctx, ExecBashBackgroundRequest{
		Command:   req.Command,
		Args:      req.Args,
		Workdir:   absDir,
		TimeoutMS: req.TimeoutMS,
	})
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	status := string(p.status)
	p.mu.Unlock()
	return &ExecBashBackgroundResponse{
		BgID:    p.id,
		Status:  status,
		Command: p.command,
		Started: p.started.UTC().Format(time.RFC3339Nano),
	}, nil
}

// ExecBashOutput fetches new output (and current status) for a
// background process. Returns NotFound if the id is unknown.
func (r *Runner) ExecBashOutput(_ context.Context, req ExecBashOutputRequest) (*ExecBashOutputResponse, error) {
	if r == nil || r.bg == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "no background processes registered", nil)
	}
	p, ok := r.bg.get(req.BgID)
	if !ok {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "unknown bg_id", map[string]any{
			"bg_id": req.BgID,
		})
	}
	return p.fetchOutput(req.Peek), nil
}

// ExecBashKill cancels a running background process. If the process is
// already done the call is a no-op and reports the current status.
func (r *Runner) ExecBashKill(_ context.Context, req ExecBashKillRequest) (*ExecBashKillResponse, error) {
	if r == nil || r.bg == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "no background processes registered", nil)
	}
	p, ok := r.bg.get(req.BgID)
	if !ok {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "unknown bg_id", map[string]any{
			"bg_id": req.BgID,
		})
	}
	p.mu.Lock()
	already := p.status != bgRunning
	curStatus := p.status
	p.mu.Unlock()
	if already {
		return &ExecBashKillResponse{
			BgID:    p.id,
			Status:  string(curStatus),
			Message: "already finished",
		}, nil
	}
	p.cancel()
	// CommandContext cancel is async; an explicit Kill avoids waiting on a
	// slow graceful teardown (notably under -race where Wait can exceed 2s).
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	// Wait for the Wait() goroutine; allow extra headroom under -race/CI load.
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status == bgRunning {
		// Kill was issued but Wait hasn't observed it yet — report killed so
		// callers don't see a stale "running" after bash.kill returns.
		p.status = bgKilled
		p.statusMsg = "killed"
	}
	return &ExecBashKillResponse{
		BgID:   p.id,
		Status: string(p.status),
	}, nil
}

// closeBg kills all background processes; used from Runner.Close.
func (r *Runner) closeBg() {
	if r.bg != nil {
		r.bg.stopAll()
	}
}

// silenceIOUnused — keep io imported even if local unused after refactor.
var _ = io.Discard
