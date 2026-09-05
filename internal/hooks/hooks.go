package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

// Runner executes tool-call and lifecycle hooks as subprocesses.
type Runner struct {
	cfg           config.HooksConfig
	workspaceRoot string

	mu     sync.Mutex
	res    map[string]*regexp.Regexp // compiled matchers, keyed by pattern
	warned map[string]bool           // patterns already reported as unparsable
}

// New creates a new Runner. Returns nil when hooks are disabled or unconfigured.
func New(cfg config.HooksConfig, workspaceRoot string) *Runner {
	if !cfg.Enabled {
		return nil
	}
	if !hasAnyHook(cfg) {
		return nil
	}
	return &Runner{
		cfg:           cfg,
		workspaceRoot: workspaceRoot,
		res:           map[string]*regexp.Regexp{},
		warned:        map[string]bool{},
	}
}

func hasAnyHook(cfg config.HooksConfig) bool {
	for _, l := range [][]config.HookSpec{
		cfg.PreTool, cfg.PostTool,
		cfg.SessionStart, cfg.UserPromptSubmit, cfg.PreCompact, cfg.TurnEnd,
	} {
		if len(l) > 0 {
			return true
		}
	}
	return false
}

// RunPreTool executes the pre-tool hooks that match toolName. Non-zero exit
// denies the tool call.
func (r *Runner) RunPreTool(ctx context.Context, toolName string, input json.RawMessage) error {
	if r == nil || len(r.cfg.PreTool) == 0 {
		return nil
	}
	return r.runList(ctx, r.cfg.PreTool, toolName, input)
}

// RunPostTool executes the post-tool hooks that match toolName. Errors are
// logged but do not fail the tool.
func (r *Runner) RunPostTool(ctx context.Context, toolName string, output json.RawMessage) {
	if r == nil || len(r.cfg.PostTool) == 0 {
		return
	}
	if err := r.runList(ctx, r.cfg.PostTool, toolName, output); err != nil {
		log.Printf("hooks: post-tool hook warning (tool=%s): %v", toolName, err)
	}
}

// runList runs every hook whose matcher accepts subject, in configured order,
// and stops at the first failure. A hook that denies is the answer; running
// the rest would only produce a second opinion nobody reads.
func (r *Runner) runList(ctx context.Context, list config.HookList, subject string, payload json.RawMessage) error {
	for _, spec := range list {
		if !r.matches(spec, subject) {
			continue
		}
		if err := r.run(ctx, spec, subject, payload); err != nil {
			return err
		}
	}
	return nil
}

// matches reports whether spec applies to subject — a tool name for tool
// hooks, an event name for lifecycle ones.
func (r *Runner) matches(spec config.HookSpec, subject string) bool {
	pattern := strings.TrimSpace(spec.Match)
	if pattern == "" {
		return true
	}
	re, ok := r.compiled(pattern)
	if !ok {
		// An unparsable matcher must not silently switch the hook off: a gate
		// that stops running is the failure that costs something, while one
		// that runs too often only costs a spawn.
		return true
	}
	return re.MatchString(subject)
}

func (r *Runner) compiled(pattern string) (*regexp.Regexp, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if re, ok := r.res[pattern]; ok {
		return re, re != nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		r.res[pattern] = nil
		if !r.warned[pattern] {
			r.warned[pattern] = true
			log.Printf("hooks: matcher %q does not compile (%v); the hook will run for every tool", pattern, err)
		}
		return nil, false
	}
	r.res[pattern] = re
	return re, true
}

func (r *Runner) run(ctx context.Context, spec config.HookSpec, subject string, payload json.RawMessage) error {
	if len(spec.Command) == 0 {
		return nil
	}

	timeout := time.Duration(r.timeoutMS(spec)) * time.Millisecond
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name := spec.Command[0]
	args := spec.Command[1:]

	cmd := exec.CommandContext(hookCtx, name, args...)
	cmd.Dir = r.workspaceRoot
	cmd.Env = buildEnv(subject, payload, r.workspaceRoot)

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

// timeoutMS resolves the per-hook override against the global setting.
func (r *Runner) timeoutMS(spec config.HookSpec) int {
	if spec.TimeoutMS > 0 {
		return spec.TimeoutMS
	}
	if r.cfg.TimeoutMS > 0 {
		return r.cfg.TimeoutMS
	}
	return 5000
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
