package exec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orchestra/orchestra/internal/subproc"
	"github.com/orchestra/orchestra/protocol"
)

type bgStatus string

const (
	bgRunning  bgStatus = "running"
	bgDone     bgStatus = "done"
	bgKilled   bgStatus = "killed"
	bgError    bgStatus = "error"
	bgTimedOut bgStatus = "timed_out"
)

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
	stdoutCap  bool
	stderrCap  bool
	exitCode   int
	status     bgStatus
	statusMsg  string
	finishedAt time.Time
}

// BackgroundRegistry holds background processes for one Runner.
type BackgroundRegistry struct {
	mu     sync.Mutex
	seq    uint64
	procs  map[string]*bgProcess
	bufCap int
}

// NewBackgroundRegistry creates an empty background process registry.
func NewBackgroundRegistry() *BackgroundRegistry {
	return &BackgroundRegistry{procs: make(map[string]*bgProcess)}
}

func (r *BackgroundRegistry) nextID() string {
	n := atomic.AddUint64(&r.seq, 1)
	return fmt.Sprintf("bg_%d", n)
}

func (r *BackgroundRegistry) add(p *bgProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.procs[p.id] = p
}

func (r *BackgroundRegistry) get(id string) (*bgProcess, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[id]
	return p, ok
}

// StopAll kills every running background process and waits for cleanup.
func (r *BackgroundRegistry) StopAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	procs := make([]*bgProcess, 0, len(r.procs))
	for _, p := range r.procs {
		procs = append(procs, p)
	}
	r.procs = make(map[string]*bgProcess)
	r.mu.Unlock()
	for _, p := range procs {
		p.cancel()
		subproc.KillProcessTree(p.cmd)
	}
	deadline := time.After(10 * time.Second)
	for _, p := range procs {
		select {
		case <-p.done:
		case <-deadline:
			return
		}
	}
}

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

// SpawnBackground starts a process under context cancellation.
func (r *BackgroundRegistry) SpawnBackground(parent context.Context, req BashBackgroundRequest) (*BashBackgroundResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "background registry is nil", nil)
	}
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
	subproc.SetProcessGroup(cmd)

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

	p.mu.Lock()
	status := string(p.status)
	p.mu.Unlock()
	return &BashBackgroundResponse{
		BgID:    p.id,
		Status:  status,
		Command: p.command,
		Started: p.started.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (p *bgProcess) fetchOutput(peek bool) *BashOutputResponse {
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
	resp := &BashOutputResponse{
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
	}
	code := p.exitCode
	resp.ExitCode = &code
	resp.DurationMS = end.Sub(p.started).Milliseconds()
	return resp
}

// DoneCh returns the process completion channel (tests may wait after StopAll clears the registry map).
func (r *BackgroundRegistry) DoneCh(bgID string) (<-chan struct{}, error) {
	p, ok := r.get(bgID)
	if !ok {
		return nil, fmt.Errorf("unknown bg_id: %s", bgID)
	}
	return p.done, nil
}

// WaitDone blocks until the background process finishes or timeout elapses.
func (r *BackgroundRegistry) WaitDone(bgID string, timeout time.Duration) error {
	p, ok := r.get(bgID)
	if !ok {
		return fmt.Errorf("unknown bg_id: %s", bgID)
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("background process %s did not finish within %s", bgID, timeout)
	}
}

// BashOutput fetches new output for a background process.
func (r *BackgroundRegistry) BashOutput(req BashOutputRequest) (*BashOutputResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "no background processes registered", nil)
	}
	p, ok := r.get(req.BgID)
	if !ok {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "unknown bg_id", map[string]any{
			"bg_id": req.BgID,
		})
	}
	return p.fetchOutput(req.Peek), nil
}

// BashKill cancels a running background process.
func (r *BackgroundRegistry) BashKill(req BashKillRequest) (*BashKillResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "no background processes registered", nil)
	}
	p, ok := r.get(req.BgID)
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
		return &BashKillResponse{
			BgID:    p.id,
			Status:  string(curStatus),
			Message: "already finished",
		}, nil
	}
	p.cancel()
	subproc.KillProcessTree(p.cmd)
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status == bgRunning {
		p.status = bgKilled
		p.statusMsg = "killed"
	}
	return &BashKillResponse{
		BgID:   p.id,
		Status: string(p.status),
	}, nil
}
