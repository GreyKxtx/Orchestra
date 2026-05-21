package core

import (
	"context"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

const planApprovedQuery = "The plan has been approved. Execute the plan."

// maybeContinueBuildAfterPlan runs a second agent turn in build mode when
// plan_exit was approved (SwitchToBuild). Merges steps, patches/ops, todos
// from both turns into a single result.
func maybeContinueBuildAfterPlan(
	ctx context.Context,
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	buildOpts agent.Options,
	history []llm.Message,
	res *agent.Result,
) ([]llm.Message, *agent.Result, error) {
	if res == nil || !res.SwitchToBuild {
		return history, res, nil
	}
	buildOpts.Mode = agent.ModeBuild
	buildOpts.JustSwitchedFromPlan = true
	buildAg, err := agent.New(llmClient, validator, toolRunner, buildOpts)
	if err != nil {
		return history, res, err
	}
	outHist, buildRes, err := buildAg.Run(ctx, history, planApprovedQuery)
	if err != nil {
		return history, res, err
	}
	return outHist, mergeAgentResults(res, buildRes), nil
}

func mergeAgentResults(first, second *agent.Result) *agent.Result {
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
