package wire

// skill.list, skill.invoke.

type SkillListParams struct{}

type SkillListResult struct {
	Skills []SkillSummary `json:"skills"`
}

type SkillSummary struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Tools             []string `json:"tools,omitempty"`
	Provider          string   `json:"provider,omitempty"`
	Model             string   `json:"model,omitempty"`
	CompletionMarkers []string `json:"completion_markers,omitempty"`
	Origin            string   `json:"origin,omitempty"`
}

type SkillInvokeResult struct {
	Skill  string `json:"skill"`
	Output string `json:"output"`
	Marker string `json:"marker,omitempty"`
	Steps  int    `json:"steps"`
}
