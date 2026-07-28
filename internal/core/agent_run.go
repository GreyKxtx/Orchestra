package core

import (
	"context"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// maybeContinueBuildAfterPlan delegates to agent.ContinueBuildAfterPlan.
func maybeContinueBuildAfterPlan(
	ctx context.Context,
	llmClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	buildOpts agent.Options,
	history []llm.Message,
	res *agent.Result,
) ([]llm.Message, *agent.Result, error) {
	return agent.ContinueBuildAfterPlan(ctx, llmClient, validator, toolRunner, buildOpts, history, res)
}
