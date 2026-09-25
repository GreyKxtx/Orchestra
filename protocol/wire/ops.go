package wire

// ops.apply.

// OpsApplyResult reports the result of applying pending ops.
type OpsApplyResult struct {
	Applied      bool     `json:"applied"`
	ChangedFiles []string `json:"changed_files"`
}
