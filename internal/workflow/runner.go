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

// Run executes the workflow as a DAG: at each step every stage whose
// dependencies are satisfied runs concurrently (a "cohort"). Each stage's
// completion marker is mapped to advance / loop / redo:<id> / fail. A redo
// resets the target stage and every transitive descendant, so cohort siblings
// that already finished are also re-executed if they depend on the redo
// target.
//
// max_attempts is counted per stage across its whole lifetime, not per
// redo cycle — that means a loop_until_marker stage with max_attempts=3 can
// fire at most 3 times total even if its redos keep happening.
//
// ctx is checked between cohorts; long-running stage internals must honour
// ctx themselves (the invoker takes it).
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
	topoIdx := make(map[string]int, len(w.Stages))
	for i := range w.Stages {
		byID[w.Stages[i].ID] = &w.Stages[i]
	}
	for i, id := range order {
		topoIdx[id] = i
	}
	descendants := buildDescendants(w)

	result := &RunResult{Outputs: make(map[string]string, len(w.Stages))}
	done := make(map[string]bool, len(w.Stages))
	attempts := make(map[string]int, len(w.Stages))

	for {
		if ctx.Err() != nil {
			result.FailureReason = "cancelled: " + ctx.Err().Error()
			return result, ctx.Err()
		}
		if len(done) == len(w.Stages) {
			return result, nil
		}

		cohort := readyCohort(w, done)
		if len(cohort) == 0 {
			// Should never happen: TopoSort would've caught a cycle. Defensive.
			result.FailureReason = "internal: no ready stages but workflow not complete"
			return result, errors.New(result.FailureReason)
		}

		// Run cohort concurrently.
		type stageResult struct {
			id      string
			attempt int
			out     string
			marker  string
			action  string
			err     error
		}
		resultsCh := make(chan stageResult, len(cohort))
		var wg sync.WaitGroup
		for _, s := range cohort {
			s := s
			attempts[s.ID]++
			attempt := attempts[s.ID]
			query := buildQuery(s, result.Outputs, opts.Arguments)
			maxAttempts := effectiveMaxAttempts(s)
			markers := markersFor(s.Skill)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if opts.OnStageStart != nil {
					opts.OnStageStart(s.ID, attempt)
				}
				out, runErr := runStage(ctx, inv, s, query)
				if runErr != nil {
					resultsCh <- stageResult{id: s.ID, attempt: attempt, err: runErr}
					return
				}
				marker := detectMarker(out, markers, s.OnMarker, s.LoopUntilMarker)
				action := decideAction(s, marker, attempt, maxAttempts)
				if opts.OnStageDone != nil {
					opts.OnStageDone(s.ID, attempt, out, marker, action)
				}
				resultsCh <- stageResult{id: s.ID, attempt: attempt, out: out, marker: marker, action: action}
			}()
		}
		// Cancel-aware wait: a non-ctx-honouring stage shouldn't be able to
		// hang the whole workflow. We still block on wg in a goroutine (we
		// can't actually force a runaway goroutine to stop) but the cohort
		// loop returns immediately on ctx.Done so the caller isn't stuck.
		// Any in-flight stages keep running orphaned until they finish or
		// crash; an inv implementation that respects ctx will unwind quickly.
		waitDone := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitDone)
		}()
		select {
		case <-waitDone:
			close(resultsCh)
		case <-ctx.Done():
			result.FailureReason = "cancelled: " + ctx.Err().Error()
			return result, ctx.Err()
		}

		// Collect & route.
		var redoTargets []string
		for r := range resultsCh {
			if r.err != nil {
				result.FailureReason = fmt.Sprintf("stage %q attempt %d failed: %v", r.id, r.attempt, r.err)
				return result, fmt.Errorf("stage %q: %w", r.id, r.err)
			}
			result.StagesExecuted = append(result.StagesExecuted, StageExecution{
				StageID: r.id, Attempt: r.attempt, Marker: r.marker,
				Action: r.action, OutputKB: (len(r.out) + 1023) / 1024,
			})
			result.Outputs[r.id] = r.out
			// FinalStage tracks the highest-topo-index stage we've completed.
			if cur, ok := topoIdx[result.FinalStage]; !ok || topoIdx[r.id] > cur {
				result.FinalStage = r.id
			}
			switch {
			case r.action == "advance":
				done[r.id] = true
			case r.action == "loop":
				// Stay not-done; it will reappear in the next cohort because
				// its deps are still satisfied. attempts[] enforces the cap.
			case r.action == "fail":
				result.FailureReason = fmt.Sprintf("stage %q: emitted %q (action=fail) after %d attempt(s)",
					r.id, r.marker, r.attempt)
				return result, errors.New(result.FailureReason)
			case strings.HasPrefix(r.action, "redo:"):
				redoTargets = append(redoTargets, strings.TrimPrefix(r.action, "redo:"))
				done[r.id] = true
			}
		}

		// Apply redos: reset target + transitive descendants (which includes
		// the redo emitter, so it re-runs after the target).
		for _, tgt := range redoTargets {
			if _, ok := byID[tgt]; !ok {
				result.FailureReason = fmt.Sprintf("redo target %q not in workflow", tgt)
				return result, errors.New(result.FailureReason)
			}
			delete(done, tgt)
			for _, d := range descendants[tgt] {
				delete(done, d)
			}
		}
	}
}

// readyCohort returns every stage whose dependencies are all in `done`
// and that isn't itself done. Order is deterministic by topo index.
func readyCohort(w *Workflow, done map[string]bool) []*Stage {
	out := make([]*Stage, 0, 4)
	for i := range w.Stages {
		s := &w.Stages[i]
		if done[s.ID] {
			continue
		}
		ready := true
		for _, dep := range s.DependsOn {
			if !done[dep] {
				ready = false
				break
			}
		}
		if ready {
			out = append(out, s)
		}
	}
	return out
}

// buildDescendants returns, for each stage id, the list of all stages that
// transitively depend on it (children, grandchildren, …). Used by redo to
// know which downstream cohort siblings must also be reset.
func buildDescendants(w *Workflow) map[string][]string {
	children := make(map[string][]string, len(w.Stages))
	for _, s := range w.Stages {
		for _, dep := range s.DependsOn {
			children[dep] = append(children[dep], s.ID)
		}
	}
	out := make(map[string][]string, len(w.Stages))
	for _, s := range w.Stages {
		seen := map[string]bool{}
		var walk func(id string)
		walk = func(id string) {
			for _, c := range children[id] {
				if seen[c] {
					continue
				}
				seen[c] = true
				walk(c)
			}
		}
		walk(s.ID)
		desc := make([]string, 0, len(seen))
		for c := range seen {
			desc = append(desc, c)
		}
		out[s.ID] = desc
	}
	return out
}

func effectiveMaxAttempts(s *Stage) int {
	m := s.MaxAttempts
	if m == 0 && s.LoopUntilMarker != "" {
		m = 3
	}
	if m == 0 {
		m = 1
	}
	return m
}

func decideAction(s *Stage, marker string, attempt, maxAttempts int) string {
	if s.LoopUntilMarker == "" {
		return "advance"
	}
	if marker == s.LoopUntilMarker {
		return "advance"
	}
	if act, ok := s.OnMarker[marker]; ok {
		return act
	}
	if attempt < maxAttempts {
		return "loop"
	}
	return "fail"
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
