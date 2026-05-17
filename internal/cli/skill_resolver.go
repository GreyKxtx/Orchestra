package cli

import (
	"fmt"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/skills"
)

// resolveSkillAgent loads the named skill from <projectRoot>/.orchestra/skills/
// and converts it into a config.AgentDefinition that can be appended to
// cfg.Agents and used via the existing --mode resolution path. Tool names
// are validated against config.ValidAgentTool.
func resolveSkillAgent(projectRoot, name string) (*config.AgentDefinition, error) {
	all, err := skills.Discover(projectRoot)
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
	if config.IsBuiltInMode(s.Name) {
		return nil, fmt.Errorf("skill %q: name collides with a built-in agent mode", s.Name)
	}
	return &config.AgentDefinition{
		Name:         s.Name,
		SystemPrompt: s.Body,
		Tools:        s.Tools,
		Model:        s.Model,
		Provider:     s.Provider,
	}, nil
}
