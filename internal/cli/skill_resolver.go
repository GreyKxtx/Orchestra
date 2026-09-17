package cli

import (
	"fmt"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/skills"
)

// When the skill body contains $ARGUMENTS, it is replaced with arguments via skills.PrepareBody.
func resolveSkillAgent(projectRoot, name, arguments string) (*config.AgentDefinition, error) {
	all, err := skills.DiscoverCached(projectRoot)
	if err != nil {
		return nil, err
	}
	s := skills.Find(all, name)
	if s == nil {
		return nil, fmt.Errorf("skill %q not found under %s/%s", name, projectRoot, skills.SkillsDir)
	}
	for _, t := range s.Tools {
		if !config.ValidAgentTool(t) {
			return nil, fmt.Errorf("skill %q: invalid tool name %q", name, t)
		}
	}
	// Built-in modes only: the caller holds the config and checks agents:.
	var noCfg *config.ProjectConfig
	if err := noCfg.CheckSkillNameFree(s.Name); err != nil {
		return nil, err
	}
	refs, err := skills.DiscoverRefs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("skill %q: discover refs: %w", name, err)
	}
	body, err := skills.PrepareBody(s.Body, arguments, refs)
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", name, err)
	}
	return &config.AgentDefinition{
		Name:         s.Name,
		SystemPrompt: body,
		Tools:        s.Tools,
		Model:        s.Model,
		Provider:     s.Provider,
	}, nil
}
