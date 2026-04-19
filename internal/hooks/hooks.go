package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

// Runner executes pre/post tool call hooks as subprocesses.
type Runner struct {
	cfg           config.HooksConfig
	workspaceRoot string
}

// New creates a new Runner. Returns nil when hooks are disabled or unconfigured.
func New(cfg config.HooksConfig, workspaceRoot string) *Runner {
	if !cfg.Enabled {
		return nil
	}
	if len(cfg.PreTool) == 0 && len(cfg.PostTool) == 0 {
		return nil
	}
	return &Runner{cfg: cfg, workspaceRoot: workspaceRoot}
}

// RunPreTool executes the pre-tool hook. Non-zero exit denies the tool call.
func (r *Runner) RunPreTool(ctx context.Context, toolName string, input json.RawMessage) error {
	if r == nil || len(r.cfg.PreTool) == 0 {
		return nil
	}
	return r.run(ctx, r.cfg.PreTool, toolName, input)
}

// RunPostTool executes the post-tool hook. Errors are logged but do not fail the tool.
func (r *Runner) RunPostTool(ctx context.Context, toolName string, output json.RawMessage) {
	if r == nil || len(r.cfg.PostTool) == 0 {
		return
	}
	if err := r.run(ctx, r.cfg.PostTool, toolName, output); err != nil {
		log.Printf("hooks: post-tool hook warning (tool=%s): %v", toolName, err)
	}
}

func (r *Runner) run(ctx context.Context, cmdArgs []string, toolName string, payload json.RawMessage) error {
	if len(cmdArgs) == 0 {
		return nil
	}

	timeout := time.Duration(r.cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name := cmdArgs[0]
	args := cmdArgs[1:]

	cmd := exec.CommandContext(hookCtx, name, args...)
	cmd.Dir = r.workspaceRoot
	cmd.Env = buildEnv(toolName, payload, r.workspaceRoot)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := err.Error()
		if s := stderr.String(); s != "" {
			msg = s
		}
		return fmt.Errorf("hook exited non-zero: %s", msg)
	}
	return nil
}

func buildEnv(toolName string, payload json.RawMessage, workspaceRoot string) []string {
	inputStr := ""
	if len(payload) > 0 {
		inputStr = string(payload)
	}
	return []string{
		"ORCH_TOOL_NAME=" + toolName,
		"ORCH_TOOL_INPUT=" + inputStr,
		"ORCH_WORKSPACE_ROOT=" + workspaceRoot,
	}
}
