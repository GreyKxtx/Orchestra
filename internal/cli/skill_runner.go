package cli

import (
	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/skillrun"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
)

func newCLISkillRunner(
	cfg *config.ProjectConfig,
	discovered []*skills.Skill,
	refs map[string]string,
	baseClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	agentLogger *llm.Logger,
	maxSteps int,
	allowExec, allowWeb, allowBrowser bool,
) agent.SkillRunner {
	return skillrun.New(cfg, discovered, refs, baseClient, validator, toolRunner, agentLogger, maxSteps, allowExec, allowWeb, allowBrowser)
}

func skillSpecs(ss []*skills.Skill) []agent.SkillSpec {
	return skillrun.Specs(ss)
}
