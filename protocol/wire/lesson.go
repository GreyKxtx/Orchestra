package wire

// lesson.rule_respond.

type RuleSuggestionRespondParams struct {
	Accept   bool   `json:"accept"`
	Dept     string `json:"dept"`
	File     string `json:"file"`
	Verify   string `json:"verify"`
	RuleLine string `json:"rule_line"`
}

type RuleSuggestionRespondResult struct {
	Applied bool `json:"applied"`
}
