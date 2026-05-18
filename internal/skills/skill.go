// Package skills loads reusable agent skill bundles from
// .orchestra/skills/*.md. A skill is a Markdown file with a YAML
// frontmatter header (name, description, tools, model, provider)
// followed by a Markdown body used as the agent system prompt.
package skills

// Skill is a parsed skill definition. Source is the absolute file path
// the skill was loaded from (empty when constructed in tests). Origin
// classifies where the file came from (project / user / pack-<id>) and
// is shown in `orchestra skills list` so the user can audit which
// skills are home-grown vs installed from third parties.
type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools,omitempty"`
	Model       string   `yaml:"model,omitempty"`
	Provider    string   `yaml:"provider,omitempty"`

	// CompletionMarkers lists strings the workflow runner watches for in the
	// final output to detect stage completion (e.g. "## RESEARCH COMPLETE",
	// "## ISSUES FOUND"). Order is significant only as documentation — the
	// runner reports the first marker it finds. Omitted for skills run
	// outside a workflow (apply --skill, skill_invoke).
	CompletionMarkers []string `yaml:"completion_markers,omitempty"`

	Body   string `yaml:"-"`
	Source string `yaml:"-"`
	Origin string `yaml:"-"`
}
