package tools

import (
	"context"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/tools/exec"
	"github.com/orchestra/orchestra/internal/tools/toolpath"
	"github.com/orchestra/orchestra/protocol"
)

func (r *Runner) ExecRun(ctx context.Context, req ExecRunRequest) (*ExecRunResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
	}
	r.dryRunMu.RLock()
	dry := r.dryRun
	block := r.blockExecInDryRun
	allowDespite := r.allowExecDespiteDryRun
	r.dryRunMu.RUnlock()
	if dry && block && !allowDespite {
		return nil, protocol.NewError(protocol.ExecFailed,
			"exec.run disabled in dry-run preview: use apply:true (TUI always applies) and shell allow (Shift+Tab) for commands",
			map[string]any{"command": req.Command})
	}
	return exec.Run(ctx, r.workspaceRoot, r.execTimeout, r.execOutputLimit, req)
}

func (r *Runner) ExecBashBackground(ctx context.Context, req ExecRunRequest) (*ExecBashBackgroundResponse, error) {
	if r == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "runner is nil", nil)
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
