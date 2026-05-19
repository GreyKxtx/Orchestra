package cli

import (
	"fmt"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/skills"
)

// argumentsMarker is replaced in a skill's body with the user-supplied
// task text when the skill is invoked. When the marker is absent, the
// task text is unchanged (still passed as the agent's user message by
// the regular apply flow).
const argumentsMarker = "$ARGUMENTS"

// resolveSkillAgent loads the named skill from .orchestra/skills/ (user
// + project, project wins) and converts it into a config.AgentDefinition
// that can be appended to cfg.Agents and used via the existing --mode
// resolution path. Tool names are validated against config.ValidAgentTool.
// When the skill body contains $ARGUMENTS, it is replaced with arguments.
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
	if config.IsBuiltInMode(s.Name) {
		return nil, fmt.Errorf("skill %q: name collides with a built-in agent mode", s.Name)
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
