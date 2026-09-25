package tools

import (
	"context"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/execpolicy"
	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/internal/tools/toolpath"
	"github.com/orchestra/orchestra/protocol"
)

func (r *Runner) ExecRun(ctx context.Context, req ExecRunRequest) (*ExecRunResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	if err := r.execBlockedInDryRun(req.Command); err != nil {
		return nil, err
	}
	req.Env = r.execEnv()
	return exec.Run(ctx, r.workspaceRoot, r.execTimeout, r.execOutputLimit, req)
}

// execEnv is the environment of a command the model runs: the core's own
// without secrets, except the ones exec.env_passthrough names.
func (r *Runner) execEnv() []string {
	return execpolicy.ScrubEnv(os.Environ(), r.execEnvPassthrough)
}

// execBlockedInDryRun is the one "no side effects in a dry-run preview" check
// for every way a command can start. It used to live inline in ExecRun only,
// so run_in_background=true ran any command the same preview refused.
func (r *Runner) execBlockedInDryRun(command string) error {
	if r.ExecRefusedByDryRun() {
		// Read by the model, which can neither press keys nor change how the
		// turn was started — so it says what to do instead.
		return protocol.NewError(protocol.ExecFailed,
			"commands cannot run in this turn: it is a dry-run preview that changes nothing (apply is off). "+
				"Answer without running commands, or tell the user that running it needs a turn with changes applied",
			map[string]any{"command": command})
	}
	return nil
}

// ExecRefusedByDryRun reports whether every command is refused right now: a
// dry-run preview on a Runner that blocks exec there (core), not unlocked by
// apply. The agent uses it to leave bash out of a turn that could never run it.
func (r *Runner) ExecRefusedByDryRun() bool {
	if r == nil {
		return false
	}
	r.dryRunMu.RLock()
	defer r.dryRunMu.RUnlock()
	return r.dryRun && r.blockExecInDryRun && !r.allowExecDespiteDryRun
}

func (r *Runner) ExecBashBackground(ctx context.Context, req ExecRunRequest) (*ExecBashBackgroundResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	if err := r.execBlockedInDryRun(req.Command); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Command) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "command is empty", nil)
	}
	absDir := r.workspaceRoot
	if w := strings.TrimSpace(req.Workdir); w != "" {
		p, _, err := toolpath.ResolveWorkspacePath(r.workspaceRoot, w)
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
		r.bg = exec.NewBackgroundRegistry()
	}
	return r.bg.SpawnBackground(ctx, exec.BashBackgroundRequest{
		Command:   req.Command,
		Args:      req.Args,
		Workdir:   absDir,
		TimeoutMS: req.TimeoutMS,
		Env:       r.execEnv(),
	})
}

func (r *Runner) ExecBashOutput(_ context.Context, req ExecBashOutputRequest) (*ExecBashOutputResponse, error) {
	if r == nil || r.bg == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "no background processes registered", nil)
	}
	return r.bg.BashOutput(req)
}

func (r *Runner) ExecBashKill(_ context.Context, req ExecBashKillRequest) (*ExecBashKillResponse, error) {
	if r == nil || r.bg == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "no background processes registered", nil)
	}
	return r.bg.BashKill(req)
}

func (r *Runner) closeBg() {
	if r.bg != nil {
		r.bg.StopAll()
	}
}
