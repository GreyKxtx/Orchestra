package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/daemon"
	"github.com/orchestra/orchestra/internal/externalpatch"
	"github.com/orchestra/orchestra/internal/git"
	"github.com/orchestra/orchestra/internal/jsonrpc"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/store"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/spf13/cobra"
)

var (
	applyFlag bool
	gitStrict bool
	gitCommit bool
	planOnly  bool
	fromPlan  string
	noDaemon  bool
	debugMode bool
	allowExec bool
	viaCore   bool
)

var applyCmd = &cobra.Command{
	Use:   "apply [query]",
	Short: "Apply changes suggested by LLM",
	Long:  "Analyzes the project and applies LLM-suggested changes",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runApply,
}

func init() {
	applyCmd.Flags().BoolVar(&applyFlag, "apply", false, "Actually apply changes (default is dry-run)")
	applyCmd.Flags().BoolVar(&gitStrict, "git-strict", false, "Fail if git repo has uncommitted changes")
	applyCmd.Flags().BoolVar(&gitCommit, "git-commit", false, "Create git commit after applying changes (requires --apply)")
	applyCmd.Flags().BoolVar(&planOnly, "plan-only", false, "Show only plan of changes, without generating code")
	applyCmd.Flags().StringVar(&fromPlan, "from-plan", "", "Apply from a saved plan.json without calling LLM")
	applyCmd.Flags().BoolVar(&noDaemon, "no-daemon", false, "Deprecated (vNext agent uses tools). Kept for compatibility.")
	applyCmd.Flags().BoolVar(&debugMode, "debug", false, "Show performance metrics and debug information")
	applyCmd.Flags().BoolVar(&allowExec, "allow-exec", false, "Allow exec.run tool (DANGEROUS; still sandboxed with limits)")
	applyCmd.Flags().BoolVar(&viaCore, "via-core", false, "Run via JSON-RPC core subprocess (stdio)")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) (retErr error) {
	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}
	if strings.TrimSpace(fromPlan) == "" && query == "" {
		return fmt.Errorf("missing query (or use --from-plan)")
	}

	dryRun := planOnly || !applyFlag
	backup := !dryRun

	// 1. Load config
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	configPath := filepath.Join(cwd, ".orchestra.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w (run 'orchestra init' first)", err)
	}

	startedAt := time.Now()
	mode := "direct"
	steps := 0
	plan := planArtifact{
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
		Query:           query,
		GeneratedAtUnix: startedAt.Unix(),
	}
	var applyResp *tools.FSApplyOpsResponse

	defer func() {
		// Always write artifacts once we know projectRoot.
		_ = writeApplyArtifacts(cfg.ProjectRoot, plan, applyResp, dryRun, startedAt, time.Now(), mode, steps, retErr)
		if retErr != nil {
			if pe, ok := protocol.AsError(retErr); ok {
				fmt.Fprintf(os.Stderr, "error_code=%s reason=%s\n", pe.Code, pe.Message)
			}
		}
	}()

	// 1.5. Check git status (if in git repo)
	if git.IsRepo(cfg.ProjectRoot) {
		clean, status, err := git.IsClean(cfg.ProjectRoot)
		if err == nil && !clean {
			if gitStrict {
				retErr = fmt.Errorf("git repo has uncommitted changes:\n%s\n\nCommit or stash changes before running orchestra, or remove --git-strict flag", status)
				return retErr
			}
			fmt.Fprintf(os.Stderr, "[orchestra] WARNING: git repo has uncommitted changes:\n%s\n\n", status)
		}
	}

	// 2. vNext: the agent uses tools directly; no monolithic context.
	if noDaemon {
		fmt.Fprintln(os.Stderr, "[orchestra] NOTE: --no-daemon is deprecated in vNext")
	}

	// If exec.confirm=false in config, we can allow exec without interactive consent.
	allowExecEffective := allowExec
	if cfg.Exec.Confirm != nil && !*cfg.Exec.Confirm {
		allowExecEffective = true
	}
	if debugMode {
		fmt.Fprintf(os.Stderr, "[orchestra] debug: llm_timeout_s=%d\n", cfg.LLM.TimeoutS)
	}

	// --- Mode: --from-plan (no LLM) ---
	if strings.TrimSpace(fromPlan) != "" {
		mode = "from_plan"
		p := strings.TrimSpace(fromPlan)
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		p, _ = filepath.Abs(p)
		data, err := os.ReadFile(p)
		if err != nil {
			retErr = err
			return retErr
		}
		var loaded planArtifact
		if err := json.Unmarshal(data, &loaded); err != nil {
			retErr = fmt.Errorf("failed to parse plan file: %w", err)
			return retErr
		}
		if query == "" {
			query = strings.TrimSpace(loaded.Query)
		}
		if query == "" {
			query = "(from plan)"
		}
		plan = loaded
		plan.ProtocolVersion = protocol.ProtocolVersion
		plan.OpsVersion = protocol.OpsVersion
		plan.ToolsVersion = protocol.ToolsVersion
		plan.Query = query
		plan.GeneratedAtUnix = time.Now().Unix()

		runner, err := tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{
			ExcludeDirs:     cfg.ExcludeDirs,
			ExecTimeout:     time.Duration(cfg.Exec.TimeoutS) * time.Second,
			ExecOutputLimit: cfg.Exec.OutputLimitKB * 1024,
		})
		if err != nil {
			retErr = err
			return retErr
		}

		resp, err := runner.FSApplyOps(cmd.Context(), tools.FSApplyOpsRequest{
			Ops:    plan.Ops,
			DryRun: dryRun,
			Backup: backup,
		})
		if err != nil {
			retErr = err
			return retErr
		}
		applyResp = resp

	} else if viaCore {
		// --- Mode: via core subprocess (stdio JSON-RPC) ---
		mode = "via_core"
		out, err := runApplyViaCore(cmd, cfg, query, allowExecEffective, dryRun, backup)
		if err != nil {
			retErr = err
			return retErr
		}
		steps = out.Steps
		plan = planArtifact{
			ProtocolVersion: protocol.ProtocolVersion,
			OpsVersion:      protocol.OpsVersion,
			ToolsVersion:    protocol.ToolsVersion,
			Query:           query,
			GeneratedAtUnix: time.Now().Unix(),
			Patches:         out.Patches,
			Ops:             out.Ops,
		}
		applyResp = out.ApplyResponse

	} else {
		// --- Mode: direct (agent + tools) ---
		mode = "direct"

		// LLM client: use test client if set, otherwise create real client.
		var llmClient llm.Client
		if testLLMClient != nil {
			llmClient = testLLMClient
		} else {
			llmClient = llm.NewOpenAIClient(cfg.LLM)
			// Set logger for LLM requests
			if openAIClient, ok := llmClient.(*llm.OpenAIClient); ok {
				logger := llm.NewLogger(cfg.ProjectRoot)
				openAIClient.SetLogger(logger)
			}
		}

		validator, err := schema.NewValidator()
		if err != nil {
			retErr = err
			return retErr
		}
		runner, err := tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{
			ExcludeDirs:     cfg.ExcludeDirs,
			ExecTimeout:     time.Duration(cfg.Exec.TimeoutS) * time.Second,
			ExecOutputLimit: cfg.Exec.OutputLimitKB * 1024,
		})
		if err != nil {
			retErr = err
			return retErr
		}

		var respFmt *llm.ResponseFormat
		if cfg.LLM.ResponseFormatType != "" {
			respFmt = &llm.ResponseFormat{Type: cfg.LLM.ResponseFormatType}
			if cfg.LLM.ResponseFormatType == "json_schema" {
				respFmt.Schema = schema.AgentStepSchemaRaw()
				respFmt.SchemaName = "agent_step"
			}
		}

		var agentLogger *llm.Logger
		if openAIClient, ok := llmClient.(*llm.OpenAIClient); ok {
			agentLogger = openAIClient.GetLogger()
		}

		ag, err := agent.New(llmClient, validator, runner, agent.Options{
			MaxSteps:             cfg.Agent.MaxSteps,
			MaxInvalidRetries:    cfg.Agent.MaxInvalidRetries,
			MaxDeniedToolRepeats: cfg.Agent.MaxDeniedRepeats,
			MaxToolErrorRepeats:  cfg.Agent.MaxToolErrors,
			MaxFinalFailures:     cfg.Agent.MaxFinalFailures,
			MaxPromptBytes:       cfg.Limits.ContextKB * 1024,
			LLMStepTimeout:       time.Duration(cfg.LLM.TimeoutS) * time.Second,
			Apply:                !dryRun,
			Backup:               backup,
			AllowExec:            allowExecEffective,
			Debug:                debugMode,
			ResponseFormat:       respFmt,
			PromptFamily:         cfg.LLM.PromptFamily,
			OnEvent:              buildCLIRenderer(),
			AgentLogger:          agentLogger,
		})
		if err != nil {
			retErr = err
			return retErr
		}

		_, res, err := ag.Run(cmd.Context(), nil, query)
		if err != nil {
			retErr = err
			return retErr
		}
		steps = res.Steps
		plan = planArtifact{
			ProtocolVersion: protocol.ProtocolVersion,
			OpsVersion:      protocol.OpsVersion,
			ToolsVersion:    protocol.ToolsVersion,
			Query:           query,
			GeneratedAtUnix: time.Now().Unix(),
			Patches:         res.Patches,
			Ops:             res.Ops,
		}
		applyResp = res.ApplyResponse
	}

	changed := []string(nil)
	if applyResp != nil {
		changed = applyResp.ChangedFiles
	}
	if len(changed) == 0 {
		fmt.Println("Changed files: (none)")
	} else {
		fmt.Printf("Changed files: %s\n", strings.Join(changed, ", "))
	}
	fmt.Printf("Dry-run: %v\n", dryRun)
	fmt.Printf("Plan saved to: %s\n", filepath.Join(cfg.ProjectRoot, ".orchestra", "plan.json"))
	fmt.Printf("Diff saved to: %s\n", filepath.Join(cfg.ProjectRoot, ".orchestra", "diff.txt"))

	// Git commit (if requested).
	if gitCommit {
		if dryRun {
			return fmt.Errorf("--git-commit requires --apply (not dry-run)")
		}
		if !git.IsRepo(cfg.ProjectRoot) {
			return fmt.Errorf("--git-commit requires a git repository")
		}
		commitMsg := fmt.Sprintf("feat(orchestra): %s", query)
		if err := git.CommitAll(cfg.ProjectRoot, commitMsg); err != nil {
			fmt.Fprintf(os.Stderr, "[orchestra] WARNING: failed to create git commit: %v\n", err)
		} else {
			fmt.Printf("✓ Created git commit: %s\n", commitMsg)
		}
	}

	return nil
}

func runApplyViaCore(cmd *cobra.Command, cfg *config.ProjectConfig, query string, allowExec bool, dryRun bool, backup bool) (*core.AgentRunResult, error) {
	child, err := spawnCoreChild(cmd.Context(), cfg.ProjectRoot)
	if err != nil {
		return nil, err
	}
	defer child.Close()

	rpc := child.Client

	projectID, err := store.ComputeProjectID(cfg.ProjectRoot)
	if err != nil {
		return nil, err
	}
	var initRes core.InitializeResult
	if err := rpc.Call(cmd.Context(), "initialize", core.InitializeParams{
		ProjectRoot:     cfg.ProjectRoot,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
	}, &initRes); err != nil {
		return nil, err
	}

	var out core.AgentRunResult
	err = rpc.Call(cmd.Context(), "agent.run", core.AgentRunParams{
		Query:             query,
		Apply:             !dryRun,
		Backup:            backup,
		MaxSteps:          cfg.Agent.MaxSteps,
		MaxInvalidRetries: cfg.Agent.MaxInvalidRetries,
		MaxPromptBytes:    cfg.Limits.ContextKB * 1024,
		AllowExec:         allowExec,
		Debug:             debugMode,
	}, &out)
	if err != nil {
		if rpcErr, ok := err.(*jsonrpc.RPCError); ok && rpcErr.Data != nil {
			if dataMap, ok := rpcErr.Data.(map[string]any); ok {
				if errorDetail, ok := dataMap["error"].(string); ok {
					return nil, fmt.Errorf("%s: %s", rpcErr.Message, errorDetail)
				}
			}
		}
		return nil, err
	}

	return &out, nil
}

type planArtifact struct {
	ProtocolVersion int `json:"protocol_version"`
	OpsVersion      int `json:"ops_version"`
	ToolsVersion    int `json:"tools_version"`

	Query           string `json:"query,omitempty"`
	GeneratedAtUnix int64  `json:"generated_at_unix"`

	// Optional: raw external patches from the model (if running with LLM).
	Patches []externalpatch.Patch `json:"patches,omitempty"`
	// Deterministic internal ops (apply --from-plan uses this).
	Ops []ops.AnyOp `json:"ops,omitempty"`
}

type lastResult struct {
	Query        string   `json:"query,omitempty"`
	Mode         string   `json:"mode"`
	DryRun       bool     `json:"dry_run"`
	Applied      bool     `json:"applied"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Steps        int      `json:"steps,omitempty"`

	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type runEvent struct {
	TSUnix int64  `json:"ts_unix"`
	Event  string `json:"event"`

	Query  string `json:"query,omitempty"`
	Mode   string `json:"mode,omitempty"`
	DryRun *bool  `json:"dry_run,omitempty"`
	Steps  *int   `json:"steps,omitempty"`

	ChangedFiles []string `json:"changed_files,omitempty"`

	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	DurationMS *int64 `json:"duration_ms,omitempty"`
}

func writeApplyArtifacts(projectRoot string, plan planArtifact, applyResp *tools.FSApplyOpsResponse, dryRun bool, startedAt, finishedAt time.Time, mode string, steps int, runErr error) error {
	baseDir := filepath.Join(projectRoot, ".orchestra")
	planPath := filepath.Join(baseDir, "plan.json")
	diffPath := filepath.Join(baseDir, "diff.txt")
	runPath := filepath.Join(baseDir, "last_run.jsonl")
	resultPath := filepath.Join(baseDir, "last_result.json")

	if plan.ProtocolVersion == 0 {
		plan.ProtocolVersion = protocol.ProtocolVersion
	}
	if plan.OpsVersion == 0 {
		plan.OpsVersion = protocol.OpsVersion
	}
	if plan.ToolsVersion == 0 {
		plan.ToolsVersion = protocol.ToolsVersion
	}
	if plan.GeneratedAtUnix == 0 {
		plan.GeneratedAtUnix = startedAt.Unix()
	}

	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err == nil {
		planJSON = append(planJSON, '\n')
		_ = daemon.AtomicWriteFile(planPath, planJSON, 0600)
	}

	// Build a human-readable diff file (best-effort).
	var diffText strings.Builder
	if applyResp != nil {
		for _, d := range applyResp.Diffs {
			diffText.WriteString("===== ")
			diffText.WriteString(d.Path)
			diffText.WriteString(" =====\n")
			diffText.WriteString("--- before\n")
			diffText.WriteString(d.Before)
			if !strings.HasSuffix(d.Before, "\n") {
				diffText.WriteString("\n")
			}
			diffText.WriteString("--- after\n")
			diffText.WriteString(d.After)
			if !strings.HasSuffix(d.After, "\n") {
				diffText.WriteString("\n")
			}
			diffText.WriteString("\n")
		}
	}
	_ = daemon.AtomicWriteFile(diffPath, []byte(diffText.String()), 0600)

	changed := []string(nil)
	if applyResp != nil {
		changed = applyResp.ChangedFiles
	}

	// last_result.json (always).
	lr := lastResult{
		Query:        plan.Query,
		Mode:         mode,
		DryRun:       dryRun,
		Applied:      runErr == nil && !dryRun,
		ChangedFiles: changed,
		Steps:        steps,
	}
	if runErr != nil {
		if pe, ok := protocol.AsError(runErr); ok {
			lr.ErrorCode = string(pe.Code)
			lr.ErrorMessage = pe.Message
		} else {
			lr.ErrorMessage = runErr.Error()
		}
	}
	if b, err := json.MarshalIndent(lr, "", "  "); err == nil {
		b = append(b, '\n')
		_ = daemon.AtomicWriteFile(resultPath, b, 0600)
	}

	// last_run.jsonl (always, minimal event log).
	dryRunCopy := dryRun
	stepsCopy := steps
	durationMS := finishedAt.Sub(startedAt).Milliseconds()
	events := []runEvent{
		{
			TSUnix: startedAt.Unix(),
			Event:  "start",
			Query:  plan.Query,
			Mode:   mode,
			DryRun: &dryRunCopy,
		},
		{
			TSUnix:       finishedAt.Unix(),
			Event:        "finish",
			Query:        plan.Query,
			Mode:         mode,
			DryRun:       &dryRunCopy,
			Steps:        &stepsCopy,
			ChangedFiles: changed,
			DurationMS:   &durationMS,
		},
	}
	if runErr != nil {
		if pe, ok := protocol.AsError(runErr); ok {
			events[1].ErrorCode = string(pe.Code)
			events[1].ErrorMessage = pe.Message
		} else {
			events[1].ErrorMessage = runErr.Error()
		}
	}

	var jsonl strings.Builder
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			continue
		}
		jsonl.Write(b)
		jsonl.WriteByte('\n')
	}
	_ = daemon.AtomicWriteFile(runPath, []byte(jsonl.String()), 0600)

	return nil
}

// buildCLIRenderer returns an OnEvent callback that renders streaming events to stderr
// when stdout is an interactive terminal. Returns nil (disables streaming display) when
// stdout is piped, redirected, or NO_COLOR is set.
func buildCLIRenderer() func(agent.AgentEvent) {
	if !isTTY() {
		return nil
	}
	var lastStep int
	return func(ev agent.AgentEvent) {
		switch ev.Stream.Kind {
		case llm.StreamEventMessageDelta:
			fmt.Fprint(os.Stderr, ev.Stream.Content)
		case llm.StreamEventToolCallStart:
			if ev.Step != lastStep {
				fmt.Fprintln(os.Stderr)
				lastStep = ev.Step
			}
			fmt.Fprintf(os.Stderr, "\n→ %s", ev.Stream.ToolCallName)
		case llm.StreamEventDone:
			if ev.Stream.Response != nil && ev.Stream.Response.Message.Content != "" {
				fmt.Fprintln(os.Stderr) // newline after streamed text
			}
		case llm.StreamEventExecOutput:
			fmt.Fprint(os.Stderr, ev.Stream.Content)
		}
	}
}

// isTTY reports whether os.Stdout is connected to an interactive terminal.
// Returns false when NO_COLOR is set or when stdout is piped/redirected.
func isTTY() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
