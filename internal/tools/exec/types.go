package exec

// RunRequest is the input for exec.run / bash.
type RunRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Workdir string   `json:"workdir,omitempty"` // relative to workspace root

	TimeoutMS     int `json:"timeout_ms,omitempty"`
	OutputLimitKB int `json:"output_limit_kb,omitempty"`

	// RunInBackground switches dispatch to the background path.
	RunInBackground bool `json:"run_in_background,omitempty"`
}

// RunResponse is the output of a foreground exec.run.
type RunResponse struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated"`
}

// BashBackgroundRequest is the input for the background spawn path.
type BashBackgroundRequest struct {
	Command   string
	Args      []string
	Workdir   string // absolute, already resolved
	TimeoutMS int    // 0 → no extra deadline beyond the registry-wide cap
}

// BashBackgroundResponse describes a freshly-spawned background job.
type BashBackgroundResponse struct {
	BgID    string `json:"bg_id"`
	Status  string `json:"status"`
	Command string `json:"command"`
	Started string `json:"started"` // RFC3339
}

// BashOutputRequest fetches new output for a running/finished job.
type BashOutputRequest struct {
	BgID string `json:"bg_id"`
	Peek bool   `json:"peek,omitempty"`
}

// BashOutputResponse is the polled view of a background job.
type BashOutputResponse struct {
	BgID       string `json:"bg_id"`
	Status     string `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	StdoutCap  bool   `json:"stdout_truncated,omitempty"`
	StderrCap  bool   `json:"stderr_truncated,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	StatusMsg  string `json:"status_msg,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// BashKillRequest cancels a running background job.
type BashKillRequest struct {
	BgID string `json:"bg_id"`
}

// BashKillResponse is returned by the kill RPC.
type BashKillResponse struct {
	BgID    string `json:"bg_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
