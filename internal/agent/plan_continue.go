package agent

import (
	"context"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

const planApprovedQuery = "The plan has been approved. Execute the plan."

// ContinueBuildAfterPlan runs a second agent turn in build mode when plan_exit
// was approved (SwitchToBuild). Merges steps, patches/ops, and todos from both
// turns into a single result.
func ContinueBuildAfterPlan(
	ctx context.Context,
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	buildOpts Options,
	history []llm.Message,
	res *Result,
) ([]llm.Message, *Result, error) {
	if res == nil || !res.SwitchToBuild {
		return history, res, nil
	}
	buildOpts.Mode = ModeBuild
	buildOpts.JustSwitchedFromPlan = true
	// Carry todos written during the plan turn into the build agent; otherwise
	// InitialTodos stays at the pre-turn snapshot and merge can wipe the plan checklist.
	if len(res.Todos) > 0 {
		buildOpts.InitialTodos = append([]tools.TodoItem(nil), res.Todos...)
	}
	buildAg, err := New(llmClient, validator, toolRunner, buildOpts)
	if err != nil {
		return history, res, err
	}
	outHist, buildRes, err := buildAg.Run(ctx, history, planApprovedQuery)
	if err != nil {
		return history, res, err
	}
	return outHist, mergeAgentResults(res, buildRes), nil
}

func mergeAgentResults(first, second *Result) *Result {
	if second == nil {
		if first != nil {
			first.SwitchToBuild = false
		}
		return first
	}
	if first == nil {
		second.SwitchToBuild = false
		return second
	}
	merged := *first
	merged.Steps = first.Steps + second.Steps
	merged.SwitchToBuild = false
	merged.MaxStepsExceeded = second.MaxStepsExceeded
	if len(second.Patches) > 0 {
		merged.Patches = second.Patches
	}
	if len(second.Ops) > 0 {
		merged.Ops = second.Ops
	}
	if second.ApplyResponse != nil {
		merged.ApplyResponse = second.ApplyResponse
	}
	merged.Applied = second.Applied || first.Applied
	// Either run may have rewritten the shared history array; the caller
	// persists whatever the LAST one returned, so the flag has to survive a
	// rewrite in either half. OR, never "second wins".
	merged.HistoryRewritten = first.HistoryRewritten || second.HistoryRewritten
	if len(second.Todos) > 0 {
		merged.Todos = second.Todos
	} else {
		merged.Todos = first.Todos
	}
	if second.SubtaskResult != "" {
		merged.SubtaskResult = second.SubtaskResult
	}
	return &merged
}
