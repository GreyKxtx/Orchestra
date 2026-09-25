package wire

// workflow.list, workflow.run.

type WorkflowListParams struct{}

type WorkflowListResult struct {
	Workflows []WorkflowSummary `json:"workflows"`
}

type WorkflowSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Stages      []string `json:"stages"`
	Source      string   `json:"source,omitempty"`
}

type WorkflowRunResult struct {
	Name          string            `json:"name"`
	Outputs       map[string]string `json:"outputs"`
	FinalStage    string            `json:"final_stage,omitempty"`
	FailureReason string            `json:"failure_reason,omitempty"`
	Stages        []StageRecord     `json:"stages"`
	DurationMS    int64             `json:"duration_ms"`
}

type StageRecord struct {
	StageID  string `json:"stage_id"`
	Attempt  int    `json:"attempt"`
	Marker   string `json:"marker,omitempty"`
	Action   string `json:"action"`
	OutputKB int    `json:"output_kb"`
}
