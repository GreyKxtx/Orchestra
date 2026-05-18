package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// StageInvoker runs one stage and returns its final output text. Implementations
// live in the CLI layer (workflow_runner.go) — this interface keeps the
// workflow package free of cross-package agent/skill imports.
type StageInvoker interface {
	// Invoke spawns the named skill with the given user query (which is the
	// fully-substituted input string) and returns the agent's final output
	// (last assistant message content). When the skill produced no text
	// final but did execute tool calls, the implementation may return a
	// short synthetic summary.
	Invoke(ctx context.Context, skillName, userQuery string) (output string, err error)
}

// RunOptions controls Run behaviour.
type RunOptions struct {
	// Arguments is substituted for $ARGUMENTS in every stage input.
	Arguments string

	// OnStageStart, if non-nil, is called before each stage begins.
	// stageID identifies the stage; attempt is 1-based (for looped stages).
	OnStageStart func(stageID string, attempt int)

	// OnStageDone, if non-nil, is called after a stage produces output.
	// marker is the completion marker detected in output (may be empty when
	// no skill marker matched). nextAction is one of "advance" | "loop" |
	// "redo:<id>" | "fail" to surface routing decisions for logging.
	OnStageDone func(stageID string, attempt int, output, marker, nextAction string)
}

// RunResult is the aggregate outcome of a workflow.
type RunResult struct {
	// Outputs maps stage_id → final output text. Multiple parallel runs are
	// joined into a single string with `\n\n---\n\n` separators.
	Outputs map[string]string

	// Stages records the order stages actually executed in (includes redos
	// so post-mortem can reconstruct the loop trace).
	StagesExecuted []StageExecution

	// FinalStage is the last stage that ran; useful for callers that want
	// to print its output as the "main" result.
	FinalStage string

	// FailureReason is non-empty when the workflow aborted before finishing
	// (max_attempts exhausted, unknown marker with fail action, etc.).
	FailureReason string
}

// StageExecution records one invocation of a stage.
type StageExecution struct {
	StageID  string
	Attempt  int
	Marker   string
	Action   string // advance | loop | redo:<id> | fail
	OutputKB int    // size of the output text in KB, for logging
}

// Run executes the workflow stages in topological order, threading outputs
// forward via {ID.output} substitution and looping on completion markers when
// configured. ctx is checked between stages; long-running stage internals
// must honour context themselves (the invoker takes ctx).
func Run(ctx context.Context, w *Workflow, inv StageInvoker, markersFor func(skill string) []string, opts RunOptions) (*RunResult, error) {
	if w == nil {
		return nil, errors.New("workflow: nil workflow")
	}
	if inv == nil {
		return nil, errors.New("workflow: nil invoker")
	}
	order, err := TopoSort(w)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*Stage, len(w.Stages))
	for i := range w.Stages {
		byID[w.Stages[i].ID] = &w.Stages[i]
	}

	result := &RunResult{Outputs: make(map[string]string, len(w.Stages))}

	// We walk in topo order but allow "redo:<id>" to splice us back. To make
	// that predictable we use an index pointer and only honour redos that go
	// strictly backwards (a forward redo is a config error caught by Validate).
	idx := 0
	for idx < len(order) {
		if ctx.Err() != nil {
			result.FailureReason = "cancelled: " + ctx.Err().Error()
			return result, ctx.Err()
		}
		stageID := order[idx]
		stage := byID[stageID]

		// Build user query: join all interpolated inputs with newlines.
		query := buildQuery(stage, result.Outputs, opts.Arguments)

		maxAttempts := stage.MaxAttempts
		if maxAttempts == 0 && stage.LoopUntilMarker != "" {
			maxAttempts = 3
		}
		if maxAttempts == 0 {
			maxAttempts = 1
		}
		markers := markersFor(stage.Skill)

		var lastOutput, lastMarker, nextAction string
		var redoTarget string

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if opts.OnStageStart != nil {
				opts.OnStageStart(stageID, attempt)
			}
			out, err := runStage(ctx, inv, stage, query)
			if err != nil {
				result.FailureReason = fmt.Sprintf("stage %q attempt %d failed: %v", stageID, attempt, err)
				return result, fmt.Errorf("stage %q: %w", stageID, err)
			}
			lastOutput = out
			lastMarker = detectMarker(out, markers, stage.OnMarker, stage.LoopUntilMarker)
			nextAction = "advance"

			// Decide next routing.
			if stage.LoopUntilMarker != "" {
				if lastMarker == stage.LoopUntilMarker {
					nextAction = "advance"
				} else if action, ok := stage.OnMarker[lastMarker]; ok {
					nextAction = action
				} else if attempt < maxAttempts {
					nextAction = "loop"
				} else {
					nextAction = "fail"
				}
			}

			if opts.OnStageDone != nil {
				opts.OnStageDone(stageID, attempt, lastOutput, lastMarker, nextAction)
			}
			result.StagesExecuted = append(result.StagesExecuted, StageExecution{
				StageID: stageID, Attempt: attempt, Marker: lastMarker,
				Action: nextAction, OutputKB: (len(lastOutput) + 1023) / 1024,
			})

			if nextAction == "advance" {
				break
			}
			if nextAction == "loop" {
				// Re-run same stage with the same inputs; the skill is expected
				// to converge on subsequent attempts via internal state.
				continue
			}
			if strings.HasPrefix(nextAction, "redo:") {
				redoTarget = strings.TrimPrefix(nextAction, "redo:")
				// Carry forward this stage's output so the redo target's
				// {stage.output} substitution sees the latest critique.
				result.Outputs[stageID] = lastOutput
				break
			}
			if nextAction == "fail" {
				result.Outputs[stageID] = lastOutput
				result.FailureReason = fmt.Sprintf("stage %q: emitted %q (action=fail) after %d attempt(s)",
					stageID, lastMarker, attempt)
				return result, errors.New(result.FailureReason)
			}
		}

		result.Outputs[stageID] = lastOutput
		result.FinalStage = stageID

		if redoTarget != "" {
			// Find redoTarget in order; splice idx back to it.
			found := -1
			for i, id := range order {
				if id == redoTarget {
					found = i
					break
				}
			}
			if found < 0 {
				// Validator should have caught this; defensive.
				result.FailureReason = fmt.Sprintf("redo target %q not in topo order", redoTarget)
				return result, errors.New(result.FailureReason)
			}
			if found >= idx {
				result.FailureReason = fmt.Sprintf("forward redo target %q (must be a prior stage)", redoTarget)
				return result, errors.New(result.FailureReason)
			}
			idx = found
			continue
		}
		idx++
	}
	return result, nil
}

// runStage executes the stage, honouring Parallel > 1 by fanning out.
func runStage(ctx context.Context, inv StageInvoker, s *Stage, query string) (string, error) {
	if s.Parallel <= 1 {
		return inv.Invoke(ctx, s.Skill, query)
	}
	type res struct {
		idx int
		out string
		err error
	}
	results := make(chan res, s.Parallel)
	var wg sync.WaitGroup
	for i := 0; i < s.Parallel; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := inv.Invoke(ctx, s.Skill, query)
			results <- res{idx: i, out: out, err: err}
		}(i)
	}
	wg.Wait()
	close(results)
	gathered := make([]res, 0, s.Parallel)
	for r := range results {
		if r.err != nil {
			return "", fmt.Errorf("parallel worker %d: %w", r.idx, r.err)
		}
		gathered = append(gathered, r)
	}
	sort.Slice(gathered, func(i, j int) bool { return gathered[i].idx < gathered[j].idx })
	parts := make([]string, len(gathered))
	for i, g := range gathered {
		parts[i] = g.out
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

// buildQuery assembles the user query for a stage by joining its inputs with
// newlines after applying $ARGUMENTS and {ID.output} substitutions. When the
// stage has no inputs, Arguments alone is used (compatibility with the trivial
// linear pipeline).
func buildQuery(s *Stage, outputs map[string]string, args string) string {
	if len(s.Inputs) == 0 {
		return args
	}
	parts := make([]string, 0, len(s.Inputs))
	for _, in := range s.Inputs {
		parts = append(parts, interpolate(in, outputs, args))
	}
	return strings.Join(parts, "\n\n")
}

// interpolate replaces $ARGUMENTS and {ID.output} tokens in s.
func interpolate(s string, outputs map[string]string, args string) string {
	out := strings.ReplaceAll(s, "$ARGUMENTS", args)
	// Replace {ID.output} for every known id. Cheap to do unconditionally
	// — outputs map is small (≤ stage count).
	for id, val := range outputs {
		out = strings.ReplaceAll(out, "{"+id+".output}", val)
	}
	return out
}

// detectMarker scans output for the first marker line that matches any known
// marker for this stage. Order of precedence:
//  1. stage.LoopUntilMarker (so the happy path is detected first),
//  2. keys of stage.OnMarker,
//  3. skill's CompletionMarkers list (markersFor).
// Returns the matched marker text (with the leading "## " preserved) or "".
func detectMarker(output string, skillMarkers []string, onMarker map[string]string, loopMarker string) string {
	candidates := make([]string, 0, len(skillMarkers)+len(onMarker)+1)
	if loopMarker != "" {
		candidates = append(candidates, loopMarker)
	}
	for k := range onMarker {
		candidates = append(candidates, k)
	}
	candidates = append(candidates, skillMarkers...)

	// Tokenise output into lines and look for any line that EQUALS a known
	// marker (after trimming trailing whitespace). Equality (not substring)
	// avoids matching markers inside code blocks or quoted examples.
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		for _, c := range candidates {
			if trimmed == c {
				return c
			}
		}
	}
	return ""
}
