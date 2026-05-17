// Package skills loads reusable agent skill bundles from
// .orchestra/skills/*.md. A skill is a Markdown file with a YAML
// frontmatter header (name, description, tools, model, provider)
// followed by a Markdown body used as the agent system prompt.
package skills

// Skill is a parsed skill definition. Source is the absolute file path
// the skill was loaded from (empty when constructed in tests).
type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools,omitempty"`
	Model       string   `yaml:"model,omitempty"`
	Provider    string   `yaml:"provider,omitempty"`

	Body   string `yaml:"-"`
	Source string `yaml:"-"`
}
