package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
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
	sessionID     string

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

// WithSession records the session id hooks are told about. Nil-safe so
// callers can chain it onto New.
func (r *Runner) WithSession(id string) *Runner {
	if r == nil {
		return nil
	}
	r.sessionID = id
	return r
}

func hasAnyHook(cfg config.HooksConfig) bool {
	for _, l := range []config.HookList{
		cfg.PreTool, cfg.PostTool,
		cfg.SessionStart, cfg.UserPromptSubmit, cfg.PreCompact, cfg.TurnEnd,
	} {
		if len(l) > 0 {
			return true
		}
	}
	return false
}

// RunPreTool executes the pre-tool hooks that match toolName and returns what
// they decided. A non-zero exit, or a JSON {"decision":"deny"} on stdout,
// denies the call.
func (r *Runner) RunPreTool(ctx context.Context, toolName string, input json.RawMessage) Decision {
	if r == nil || len(r.cfg.PreTool) == 0 {
		return Decision{}
	}
	return r.runList(ctx, r.cfg.PreTool, "pre_tool", toolName, input)
}

// RunPostTool executes the post-tool hooks that match toolName. A denial has
// nothing left to stop, so it is only logged.
func (r *Runner) RunPostTool(ctx context.Context, toolName string, output json.RawMessage) {
	if r == nil || len(r.cfg.PostTool) == 0 {
		return
	}
	if dec := r.runList(ctx, r.cfg.PostTool, "post_tool", toolName, output); dec.Denied {
		log.Printf("hooks: post-tool hook warning (tool=%s): %s", toolName, dec.Reason)
	}
}

// runList runs every hook whose matcher accepts subject, in configured order,
// and stops at the first denial. A hook that denies is the answer; running the
// rest would only produce a second opinion nobody reads.
//
// A hook that rewrote the input hands the rewritten version to the next hook,
// so a chain decides about what will actually run rather than each hook
// judging the original in isolation.
func (r *Runner) runList(ctx context.Context, list config.HookList, eventName, subject string, payload json.RawMessage) Decision {
	var out Decision
	current := payload
	for _, spec := range list {
		if !r.matches(spec, subject) {
			continue
		}
		rep, err := r.run(ctx, spec, eventName, subject, current)
		if err != nil {
			out.Denied = true
			out.Reason = err.Error()
			return out
		}
		out.apply(rep)
		if out.Denied {
			return out
		}
		if len(out.Input) > 0 {
			current = out.Input
		}
	}
	return out
}

// matches reports whether spec applies to subject — a tool name for tool
// hooks, the event name for lifecycle ones.
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

// run executes one hook. The returned error is the denial reason; the reply is
// whatever the hook said on stdout.
func (r *Runner) run(ctx context.Context, spec config.HookSpec, eventName, subject string, payload json.RawMessage) (reply, error) {
	if len(spec.Command) == 0 {
		return reply{}, nil
	}

	timeout := time.Duration(r.timeoutMS(spec)) * time.Millisecond
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(hookCtx, spec.Command[0], spec.Command[1:]...)
	cmd.Dir = r.workspaceRoot
	cmd.Env = buildEnv(subject, payload, r.workspaceRoot, r.sessionID, eventName)
	cmd.Stdin = bytes.NewReader(r.eventJSON(eventName, subject, payload))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := err.Error()
		if s := strings.TrimSpace(stderr.String()); s != "" {
			msg = s
		}
		// A hook may explain itself on stdout and still exit non-zero.
		if rep, ok := parseReply(stdout.Bytes()); ok && strings.TrimSpace(rep.Reason) != "" {
			msg = strings.TrimSpace(rep.Reason)
		}
		return reply{}, fmt.Errorf("hook exited non-zero: %s", msg)
	}

	rep, _ := parseReply(stdout.Bytes())
	return rep, nil
}

func (r *Runner) eventJSON(eventName, subject string, payload json.RawMessage) []byte {
	ev := event{
		Event:         eventName,
		SessionID:     r.sessionID,
		WorkspaceRoot: r.workspaceRoot,
	}
	if eventName == "pre_tool" || eventName == "post_tool" {
		ev.Tool = subject
	}
	if len(payload) > 0 && json.Valid(payload) {
		ev.Input = payload
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return nil
	}
	return data
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

// buildEnv extends the parent environment rather than replacing it. Hooks used
// to run with three variables and no PATH, so a hook that called git or node
// failed with an error that named neither.
func buildEnv(toolName string, payload json.RawMessage, workspaceRoot, sessionID, eventName string) []string {
	inputStr := ""
	if len(payload) > 0 {
		inputStr = string(payload)
	}
	env := append([]string(nil), os.Environ()...)
	return append(env,
		"ORCH_TOOL_NAME="+toolName,
		"ORCH_TOOL_INPUT="+inputStr,
		"ORCH_WORKSPACE_ROOT="+workspaceRoot,
		"ORCH_SESSION_ID="+sessionID,
		"ORCH_HOOK_EVENT="+eventName,
	)
}
