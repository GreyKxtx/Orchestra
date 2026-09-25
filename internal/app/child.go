package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/protocol/wire"
)

// ChildEvents is how a child reports to the client. Notify receives its
// lifecycle (child_started, child_done); Stream builds the receiver of the
// events of its steps, tagged with the child's identity.
type ChildEvents struct {
	Notify func(wire.AgentEvent)
	Stream func(taskID, parentToolCallID, subagentType string) func(agent.AgentEvent)
}

// ChildRun is one child agent to run: on what, with which options, and how
// its edits and its events reach its caller.
type ChildRun struct {
	Client    llm.Client
	Validator *schema.Validator
	Tools     *tools.Runner
	// Options is what ChildOptions built for the child.
	Options agent.Options
	Goal    string
	History []llm.Message

	// Kind names the child in its events and in the request log: a role, a
	// skill ("skill:<name>"), a workflow stage ("stage:<id>").
	Kind string
	// TaskID identifies the child in its events and in the request log. Empty
	// is given one; a caller that traced the child itself (the task runner)
	// passes the id its context already carries.
	TaskID string
	// ParentToolCallID is the tool call the child answers, when one did.
	ParentToolCallID string

	// CallerOwnsLayer: the child writes where its caller does. By default a
	// child writes into a layer of its own over its caller's view, committed
	// into it when the child succeeds and dropped when it fails (ORC-1). The
	// task runner forks the layer before the child is scheduled and decides
	// itself when it commits, so it says so.
	CallerOwnsLayer bool

	Events ChildEvents
}

// ChildOutcome is what a child did: its history, its result, and the files
// its layer committed into its caller's view.
type ChildOutcome struct {
	// TaskID is the identity the child ran under.
	TaskID    string
	History   []llm.Message
	Result    *agent.Result
	Committed []string
}

// Text is the child's answer: its task_result, else its closing words.
func (o *ChildOutcome) Text() string {
	if o == nil {
		return ""
	}
	if o.Result != nil && o.Result.SubtaskResult != "" {
		return o.Result.SubtaskResult
	}
	return ClosingText(o.History)
}

// Steps is how many steps the child took.
func (o *ChildOutcome) Steps() int {
	if o == nil || o.Result == nil {
		return 0
	}
	return o.Result.Steps
}

var childSeq atomic.Int64

// RunChild builds and runs one child agent. It is the one launcher: the
// task runner, skills, workflow stages and the pipeline's stages all go
// through it, so every child gets the same layer, the same commit-or-drop of
// its edits, the same containment of a panic and the same lifecycle events.
//
// The outcome carries what the child managed even when err is set; it is
// nil only when no agent could be built. A child whose run succeeded but
// whose layer could not be committed (a merge conflict with what a sibling
// committed first, a caller cancelled on the way) is an error, and its edits
// are dropped.
func RunChild(ctx context.Context, r ChildRun) (out *ChildOutcome, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r.TaskID == "" {
		r.TaskID = fmt.Sprintf("child_%d", childSeq.Add(1))
	}
	parent := llm.TraceFrom(ctx)
	depth := parent.Depth
	parentTaskID := parent.ParentTaskID
	if parent.TaskID != r.TaskID {
		// The child's model calls and tool calls are logged under its own
		// identity: llm_log.jsonl is shared by the whole tree.
		parentTaskID = parent.TaskID
		depth = parent.Depth + 1
		ctx = llm.WithTrace(ctx, llm.Trace{
			RunID:        parent.RunID,
			TaskID:       r.TaskID,
			ParentTaskID: parentTaskID,
			Depth:        depth,
		})
	}
	if r.Options.OnEvent == nil && r.Events.Stream != nil {
		r.Options.OnEvent = r.Events.Stream(r.TaskID, r.ParentToolCallID, r.Kind)
	}
	if r.Events.Notify != nil {
		// Every child closes with exactly one child_done, however it ended.
		defer func() {
			ev := wire.AgentEvent{
				Type:             wire.EventChildDone,
				TaskID:           r.TaskID,
				ParentToolCallID: r.ParentToolCallID,
				ParentTaskID:     parentTaskID,
				SubagentType:     r.Kind,
				Depth:            depth,
				Status:           "done",
				Content:          childSummary(out.Text()),
			}
			if err != nil {
				ev.Status = "error"
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					ev.Status = "cancelled"
				}
				ev.Error = err.Error()
			}
			r.Events.Notify(ev)
		}()
	}
	// A panic in the child — the loop recovers its own, this covers the
	// build, the commit and the sinks — is the child's error, not the
	// process's: the parent, its siblings and the RPC server go on.
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "app: child %s (%s) panicked: %v\n%s\n", r.TaskID, r.Kind, rec, debug.Stack())
			err = fmt.Errorf("child agent panicked: %v", rec)
		}
	}()
	if r.Events.Notify != nil {
		r.Events.Notify(wire.AgentEvent{
			Type:             wire.EventChildStarted,
			TaskID:           r.TaskID,
			ParentToolCallID: r.ParentToolCallID,
			ParentTaskID:     parentTaskID,
			SubagentType:     r.Kind,
			Model:            r.Options.ModelLabel,
			Content:          r.Goal,
			Depth:            depth,
		})
	}

	ag, err := agent.New(r.Client, r.Validator, r.Tools, r.Options)
	if err != nil {
		return nil, err
	}

	var layer *fs.Overlay
	if !r.CallerOwnsLayer && r.Tools != nil {
		// Nil outside a dry run: the child then writes to disk as its
		// caller does.
		layer = r.Tools.ForkLayer(ctx)
	}
	if layer != nil {
		owner := tools.LayerContext(ctx)
		// A committed layer has nothing left to drop.
		defer r.Tools.DropLayer(owner, layer)
		ctx = tools.WithLayer(ctx, layer)
	}

	hist, res, runErr := ag.Run(ctx, r.History, r.Goal)
	out = &ChildOutcome{TaskID: r.TaskID, History: hist, Result: res}
	if runErr != nil {
		return out, runErr
	}
	if layer != nil {
		paths, err := CommitLayer(ctx, layer)
		if err != nil {
			return out, err
		}
		out.Committed = paths
	}
	return out, nil
}

// CommitLayer merges a child's layer into its owner's view and returns the
// paths it carried. A conflict — a file another child committed a change to
// since this one first wrote it — is the child's error, and its layer is
// dropped. So is a child that was cancelled on the way: what it did goes
// with it.
func CommitLayer(ctx context.Context, layer *fs.Overlay) ([]string, error) {
	if layer == nil {
		return nil, nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return nil, fmt.Errorf("not committed: %w", cause)
	}
	paths, err := layer.Commit()
	if err != nil {
		var conflict *fs.MergeConflict
		if errors.As(err, &conflict) {
			return nil, fmt.Errorf("merge_conflict: %w", err)
		}
		return nil, err
	}
	return paths, nil
}

// ClosingText is the child's last assistant message as prose. A final
// written in the {"type":"final","final":{...}} envelope is unwrapped to its
// summary: the parent should read "raised retryLimit to 13", not the envelope
// around it.
func ClosingText(hist []llm.Message) string {
	for i := len(hist) - 1; i >= 0; i-- {
		m := hist[i]
		if m.Role != llm.RoleAssistant || strings.TrimSpace(m.Content) == "" {
			continue
		}
		text := strings.TrimSpace(m.Content)
		var env struct {
			Type  string `json:"type"`
			Final struct {
				Summary string `json:"summary"`
			} `json:"final"`
		}
		if json.Unmarshal([]byte(text), &env) == nil && env.Type == "final" {
			return strings.TrimSpace(env.Final.Summary)
		}
		return text
	}
	return ""
}

// childSummary is what child_done carries of the child's answer.
func childSummary(s string) string {
	s = strings.TrimSpace(s)
	const max = 240
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}
