package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/checkpoint"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/git"
	"github.com/orchestra/orchestra/internal/pipeline"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/retention"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/usage"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/spf13/cobra"
)

var (
	applyFlag           bool
	gitStrict           bool
	gitCommit           bool
	planOnly            bool
	fromPlan            string
	debugMode           bool
	allowExec           bool
	allowWeb            bool
	allowBrowser        bool
	viaCore             bool
	agentMode           string // "plan", "build", or "" (default)
	pipelineMode        bool
	pipelineMaxAttempts int
	pipelineTraceID     string
	applyProvider       string
	applySkill          string
	applyImages         []string
	applyStream         bool
	outputPatch         string // --output-patch; NoOptDefVal="AUTO"
	applyProfile        string // --profile fast|precision
	applyWorktree       string // --worktree name
	applyResume         string // --resume run_id|last
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
	applyCmd.Flags().BoolVar(&debugMode, "debug", false, "Show performance metrics and debug information")
	applyCmd.Flags().BoolVar(&allowExec, "allow-exec", false, "Allow exec.run tool (DANGEROUS; still sandboxed with limits)")
	applyCmd.Flags().BoolVar(&allowWeb, "allow-web", false, "Allow webfetch tool (fetches external URLs; private IPs blocked)")
	applyCmd.Flags().BoolVar(&allowBrowser, "allow-browser", false, "Allow browser.* tools (requires Node.js and npx)")
	applyCmd.Flags().BoolVar(&viaCore, "via-core", false, "Run via JSON-RPC core subprocess (stdio)")
	applyCmd.Flags().StringVar(&agentMode, "mode", "",
		"Agent mode: "+strings.Join(config.UserSelectableModeNames(), "|")+" (or a custom agents: name)")
	applyCmd.Flags().BoolVar(&pipelineMode, "pipeline", false, "Run multi-agent pipeline: Investigator → Coder → Critic")
	applyCmd.Flags().IntVar(&pipelineMaxAttempts, "pipeline-attempts", 2, "Max Coder→Critic cycles in pipeline mode")
	applyCmd.Flags().StringVar(&pipelineTraceID, "trace-id", "", "Trace ID for runtime evidence pre-fetch in pipeline mode")
	applyCmd.Flags().StringVar(&applyProvider, "provider", "", "Use a named provider from .orchestra.yml providers: section")
	applyCmd.Flags().StringVar(&applySkill, "skill", "", "Run with the named skill from .orchestra/skills/")
	applyCmd.Flags().StringSliceVar(&applyImages, "image", nil, "Image file(s) to attach to the user message (PNG/JPEG/GIF/WebP). Repeatable. Requires a multimodal LLM.")
	applyCmd.Flags().BoolVar(&applyStream, "stream", false, "Stream assistant tokens to stdout as they arrive (works in non-TTY pipes too)")
	applyCmd.Flags().StringVar(&outputPatch, "output-patch", "", "Export unified .patch instead of writing files (optional path; default: apply.patch_dir)")
	applyCmd.Flags().Lookup("output-patch").NoOptDefVal = "AUTO"
	applyCmd.Flags().StringVar(&applyProfile, "profile", "", "Adaptive execution profile: fast|precision")
	applyCmd.Flags().StringVar(&applyWorktree, "worktree", "", "Run in orchestra-managed git worktree (name from orchestra worktree list)")
	applyCmd.Flags().StringVar(&applyResume, "resume", "", "Resume a run that did not finish (a crash, a kill) from its checkpoint: a run id, or \"last\". The run keeps its own query, mode and --apply; consent flags (--allow-exec, --allow-web) come from this command")
	rootCmd.AddCommand(applyCmd)
}

// applyRun is one `orchestra apply`, resolved once from its flags and the
// project config: what to run, on which project, writing where, with which
// consent. The modes below take it and fill an applyOutcome.
type applyRun struct {
	cfg *config.ProjectConfig
	cwd string
	// query is the task; --from-plan takes it from the plan when the command
	// line has none.
	query string

	dryRun bool
	backup bool
	// applyOutput is config.ApplyOutputDisk or config.ApplyOutputPatch;
	// patchOutPath is the --output-patch path when the user gave one.
	applyOutput  string
	patchOutPath string

	profile string
	// mode is --mode, or the agent a --skill was materialised as.
	mode string

	allowExec    bool
	allowWeb     bool
	allowBrowser bool

	usage *usage.Tracker
}

// applyOutcome is what a mode produced: the plan for plan.json, the apply
// response for diff.txt and the summary, and the patch path a core already
// wrote. A mode fills it as it goes, so a run that fails midway still
// records what it had.
type applyOutcome struct {
	mode          string
	steps         int
	plan          planArtifact
	applyResp     *tools.FSApplyOpsResponse
	corePatchPath string
}

func runApply(cmd *cobra.Command, args []string) (retErr error) {
	r, err := resolveApplyRun(cmd, args)
	if err != nil {
		return err
	}
	startedAt := time.Now()
	out := &applyOutcome{mode: "direct", plan: newPlanArtifact(r.query, startedAt)}
	r.usage = newUsageTracker("apply", r.cfg)

	defer func() {
		// Always write artifacts once we know projectRoot.
		if err := writeApplyArtifacts(r.cfg.ProjectRoot, out.plan, out.applyResp, r.dryRun, startedAt, time.Now(), out.mode, out.steps, retErr); err != nil {
			fmt.Fprintf(os.Stderr, "[orchestra] %v\n", err)
			if retErr == nil {
				retErr = err
			}
		}
		finalizeUsage(r.usage, r.cfg)
		if retErr != nil {
			if pe, ok := protocol.AsError(retErr); ok {
				fmt.Fprintf(os.Stderr, "error_code=%s reason=%s\n", pe.Code, pe.Message)
			}
		}
	}()

	if err := r.checkGitStatus(); err != nil {
		retErr = err
		return retErr
	}
	if debugMode {
		fmt.Fprintf(os.Stderr, "[orchestra] debug: llm_timeout_s=%d\n", r.cfg.LLM.TimeoutS)
	}

	// cmd.Context is nil when the command runs outside cobra's Execute
	// (tests call runApply directly).
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	switch {
	case strings.TrimSpace(fromPlan) != "":
		err = r.runFromPlan(ctx, out)
	case viaCore:
		err = r.runViaCore(cmd, out)
	case pipelineMode:
		err = r.runPipeline(ctx, out)
	default:
		err = r.runDirect(ctx, out)
	}
	if err != nil {
		retErr = err
		return retErr
	}
	return r.report(out)
}

// resolveApplyRun reads the flags and the project config into an applyRun:
// the checks that refuse a command before it does anything live here.
func resolveApplyRun(cmd *cobra.Command, args []string) (*applyRun, error) {
	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}
	if strings.TrimSpace(fromPlan) == "" && query == "" && strings.TrimSpace(applyResume) == "" {
		return nil, fmt.Errorf("missing query (or use --from-plan, or --resume)")
	}

	r := &applyRun{query: query}
	r.dryRun = planOnly || !applyFlag
	r.backup = !r.dryRun

	// 1. Load config
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}
	r.cwd = cwd

	configPath := filepath.Join(cwd, ".orchestra.yml")
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w (run 'orchestra init' first)", err)
	}
	r.cfg = cfg
	warnUntrusted(cfg)
	cfg.FprintWarnings(os.Stderr)

	if wt := strings.TrimSpace(applyWorktree); wt != "" {
		wtPath, wtErr := git.ResolveManagedWorktree(cfg.ProjectRoot, wt)
		if wtErr != nil {
			return nil, fmt.Errorf("--worktree %q: %w", wt, wtErr)
		}
		cfg.ProjectRoot = wtPath
	}

	// Resolve the real model context window before anything derives a byte
	// budget from it (EffectiveMaxPromptBytes / EffectiveCompactThresholdPct).
	// Without this the direct apply path never learns the window and falls back
	// to the flat limits.context_kb default — on a 200k model that is ~15% of
	// the usable context, so the agent starts compacting a dozen steps in.
	if getTestLLMClient() == nil {
		limParent := cmd.Context()
		if limParent == nil {
			limParent = context.Background()
		}
		limCtx, limCancel := context.WithTimeout(limParent, 8*time.Second)
		if lim, ok := llm.ResolveModelLimits(limCtx, &cfg.LLM); ok && debugMode {
			fmt.Fprintf(os.Stderr, "[apply] model window: %d tokens (%s)\n", lim.ContextTokens, lim.Source)
		}
		limCancel()
	}

	r.applyOutput = strings.ToLower(strings.TrimSpace(cfg.Apply.Output))
	if r.applyOutput == "" {
		r.applyOutput = config.ApplyOutputDisk
	}
	if cmd.Flags().Changed("output-patch") {
		r.applyOutput = config.ApplyOutputPatch
		if outputPatch != "" && outputPatch != "AUTO" {
			r.patchOutPath = outputPatch
		}
	}
	if r.applyOutput == config.ApplyOutputPatch {
		if applyFlag {
			return nil, fmt.Errorf("--output-patch / apply.output=patch is mutually exclusive with --apply")
		}
		r.dryRun = true
		r.backup = false
	}

	r.profile = strings.TrimSpace(cfg.Agent.Profile)
	if applyProfile != "" {
		r.profile = applyProfile
	}
	if !agent.IsKnownProfile(r.profile) {
		return nil, fmt.Errorf("unknown --profile / agent.profile %q (want fast|precision)", r.profile)
	}

	if applyProvider != "" {
		provCfg, ok := cfg.FindProvider(applyProvider)
		if !ok {
			return nil, fmt.Errorf("--provider %q: not found in .orchestra.yml providers: section\nAvailable: %s",
				applyProvider, providerNames(cfg))
		}
		cfg.LLM = provCfg
	}

	r.mode = agentMode
	if applySkill != "" {
		if r.mode != "" {
			return nil, fmt.Errorf("--skill and --mode are mutually exclusive")
		}
		def, err := resolveSkillAgent(cfg.ProjectRoot, applySkill, query)
		if err != nil {
			return nil, err
		}
		if cfg.FindAgent(def.Name) != nil {
			return nil, fmt.Errorf("--skill %q: name collides with an existing entry in agents: in .orchestra.yml", def.Name)
		}
		cfg.Agents = append(cfg.Agents, *def)
		r.mode = def.Name
	}

	if r.mode != "" {
		if kind, builtIn := config.BuiltInModeKind(r.mode); builtIn {
			// worker/verifier/product/documentation get their input from a
			// WorkOrder and answer through task_result; started top-level they
			// have neither, so refuse with the reason instead of running a
			// half-wired agent.
			if kind != config.ModeKindTopLevel {
				return nil, fmt.Errorf("agent mode %q runs only as a subagent (spawned via task / task_spawn), not from --mode; available modes: %s",
					r.mode, strings.Join(config.UserSelectableModeNames(), ", "))
			}
		} else if cfg.FindAgent(r.mode) == nil {
			return nil, fmt.Errorf("unknown agent mode %q: not a built-in mode and not defined in agents: in .orchestra.yml", r.mode)
		}
	}

	// exec.confirm=false / web.confirm=false in config stand for the flag;
	// the browser always needs the flag.
	r.allowExec = allowExec || (cfg.Exec.Confirm != nil && !*cfg.Exec.Confirm)
	r.allowWeb = allowWeb || (cfg.Web.Confirm != nil && !*cfg.Web.Confirm)
	r.allowBrowser = allowBrowser
	return r, nil
}

// newPlanArtifact is the plan.json header of a run that has produced no
// ops yet.
func newPlanArtifact(query string, at time.Time) planArtifact {
	return planArtifact{
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
		Query:           query,
		GeneratedAtUnix: at.Unix(),
	}
}

// checkGitStatus warns about, or with --git-strict refuses, a dirty repository.
func (r *applyRun) checkGitStatus() error {
	if !git.IsRepo(r.cfg.ProjectRoot) {
		return nil
	}
	clean, status, err := git.IsClean(r.cfg.ProjectRoot)
	if err != nil || clean {
		return nil
	}
	if gitStrict {
		return fmt.Errorf("git repo has uncommitted changes:\n%s\n\nCommit or stash changes before running orchestra, or remove --git-strict flag", status)
	}
	fmt.Fprintf(os.Stderr, "[orchestra] WARNING: git repo has uncommitted changes:\n%s\n\n", status)
	return nil
}

// runFromPlan replays a saved plan.json through the applier, with no model.
func (r *applyRun) runFromPlan(ctx context.Context, out *applyOutcome) error {
	out.mode = "from_plan"
	p := strings.TrimSpace(fromPlan)
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.cwd, p)
	}
	p, _ = filepath.Abs(p)
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var loaded planArtifact
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("failed to parse plan file: %w", err)
	}
	if r.query == "" {
		r.query = strings.TrimSpace(loaded.Query)
	}
	if r.query == "" {
		r.query = "(from plan)"
	}
	out.plan = loaded
	out.plan.ProtocolVersion = protocol.ProtocolVersion
	out.plan.OpsVersion = protocol.OpsVersion
	out.plan.ToolsVersion = protocol.ToolsVersion
	out.plan.Query = r.query
	out.plan.GeneratedAtUnix = time.Now().Unix()

	runner, err := tools.NewRunner(r.cfg.ProjectRoot, cliRunnerOptions(r.cfg, r.dryRun, r.allowBrowser))
	if err != nil {
		return err
	}
	defer runner.Close()

	resp, err := runner.FSApplyOps(ctx, tools.FSApplyOpsRequest{
		Ops:    out.plan.Ops,
		DryRun: r.dryRun,
		Backup: r.backup,
	})
	if err != nil {
		return err
	}
	out.applyResp = resp
	return nil
}

// runViaCore drives an `orchestra core` subprocess over stdio JSON-RPC.
func (r *applyRun) runViaCore(cmd *cobra.Command, out *applyOutcome) error {
	out.mode = "via_core"
	res, err := runApplyViaCore(cmd, r.cfg, r.query, r.allowExec, r.dryRun, r.backup, r.applyOutput, r.patchOutPath, r.profile)
	if err != nil {
		return err
	}
	out.steps, out.plan, out.applyResp, out.corePatchPath = takeCoreResult(r.query, res)
	return nil
}

// runPipeline runs the Investigator → Coder → Critic pipeline.
func (r *applyRun) runPipeline(ctx context.Context, out *applyOutcome) error {
	out.mode = "pipeline"
	cfg := r.cfg

	var llmClient llm.Client
	if getTestLLMClient() != nil {
		llmClient = getTestLLMClient()
	} else {
		c, _, err := app.ClientFor(cfg, "", "", llm.NewLogger(cfg.ProjectRoot))
		if err != nil {
			return err
		}
		llmClient = c
	}

	validator, err := schema.NewValidator()
	if err != nil {
		return err
	}
	runner, err := tools.NewRunner(cfg.ProjectRoot, cliRunnerOptions(cfg, r.dryRun, r.allowBrowser))
	if err != nil {
		return err
	}
	defer runner.Close()

	respFmt := agent.ResolveResponseFormat(cfg.LLM, providerLabelFor(cfg, applyProvider), agent.ResponseFormatToolAgent)
	agentLogger := llm.LoggerOf(llmClient)

	cliRenderer := buildCLIRenderer()
	var onPipelineEvent func(stage string, ev agent.AgentEvent)
	if cliRenderer != nil {
		var lastStage string
		onPipelineEvent = func(stage string, ev agent.AgentEvent) {
			if stage != lastStage {
				fmt.Fprintf(os.Stderr, "\n[pipeline:%s]\n", stage)
				lastStage = stage
			}
			cliRenderer(ev)
		}
	}

	var traceCtx *pipeline.TraceContext
	if pipelineTraceID != "" {
		traceCtx = &pipeline.TraceContext{TraceID: pipelineTraceID}
	}

	compactionClient, compactionCtxTokens := compactionClientFor(cfg, agentLogger)
	pipeRes, err := pipeline.Run(ctx, llmClient, validator, runner, r.query, pipeline.Options{
		UsageTracker:            r.usage,
		ProviderLabel:           providerLabelFor(cfg, applyProvider),
		ModelLabel:              cfg.LLM.Model,
		MaxCoderAttempts:        pipelineMaxAttempts,
		Apply:                   !r.dryRun,
		Backup:                  r.backup,
		TraceCtx:                traceCtx,
		MaxStepsCoder:           cfg.Agent.MaxSteps,
		MaxInvalidRetries:       cfg.Agent.MaxInvalidRetries,
		MaxDeniedToolRepeats:    cfg.Agent.MaxDeniedRepeats,
		MaxToolErrorRepeats:     cfg.Agent.MaxToolErrors,
		MaxFinalFailures:        cfg.Agent.MaxFinalFailures,
		MaxPromptBytes:          cfg.EffectiveMaxPromptBytes(),
		CompactThresholdPct:     cfg.EffectiveCompactThresholdPct(),
		ModelContextTokens:      int(cfg.EffectiveNumCtx()),
		CompletionMaxTokens:     cfg.LLM.MaxTokens,
		LLMStepTimeout:          time.Duration(cfg.LLM.TimeoutS) * time.Second,
		PromptFamily:            promptpkg.ResolvePromptFamily(cfg.LLM.PromptFamily, cfg.LLM.Model),
		CompactionClient:        compactionClient,
		CompactionContextTokens: compactionCtxTokens,
		ResponseFormat:          respFmt,
		Debug:                   debugMode,
		AgentLogger:             agentLogger,
		OnEvent:                 onPipelineEvent,
		PermissionRules:         cfg.Permissions.Rules,
	})
	if err != nil {
		return err
	}

	for _, sr := range pipeRes.StageResults {
		out.steps += sr.Steps
	}
	if !pipeRes.Accepted {
		fmt.Fprintln(os.Stderr, "[pipeline] WARNING: Critic did not accept after all attempts — using last Coder output")
	} else {
		fmt.Fprintf(os.Stderr, "[pipeline] Critic accepted after %d attempt(s)\n", pipeRes.Attempts)
	}
	out.plan = newPlanArtifact(r.query, time.Now())
	out.plan.Patches = pipeRes.Patches
	out.plan.Ops = pipeRes.Ops
	out.applyResp = pipeRes.ApplyResponse
	return nil
}

// runDirect runs the turn through a core in this process.
func (r *applyRun) runDirect(ctx context.Context, out *applyOutcome) error {
	out.mode = "direct"
	imageParts, err := loadImageParts(applyImages)
	if err != nil {
		return err
	}
	if len(imageParts) > 0 && !r.cfg.LLM.Multimodal {
		return fmt.Errorf("--image: configured LLM is not marked multimodal in .orchestra.yml (set llm.multimodal: true after switching to a VL model)")
	}
	res, err := runApplyInProcess(ctx, r.cfg, core.AgentRunParams{
		Query:             r.query,
		Apply:             !r.dryRun,
		Backup:            r.backup,
		MaxSteps:          r.cfg.Agent.MaxSteps,
		MaxInvalidRetries: r.cfg.Agent.MaxInvalidRetries,
		MaxPromptBytes:    r.cfg.EffectiveMaxPromptBytes(),
		AllowExec:         r.allowExec,
		AllowWeb:          r.allowWeb,
		AllowBrowser:      r.allowBrowser,
		Debug:             debugMode,
		Mode:              r.mode,
		ApplyOutput:       r.applyOutput,
		PatchPath:         r.patchOutPath,
		Profile:           r.profile,
		QuestionAsker:     buildQuestionAsker(r.mode, len(r.cfg.Orchestra.RequiredGates()) > 0),
		OnAgentEvent:      buildCLIRenderer(),
		UserImages:        imageParts,
		Resume:            strings.TrimSpace(applyResume),
	})
	if err != nil {
		printResumeHint(r.cfg.ProjectRoot)
		return err
	}
	out.steps, out.plan, out.applyResp, out.corePatchPath = takeCoreResult(r.query, res)
	return nil
}

// report writes the patch a patch-mode run asked for, prints the summary
// and makes the --git-commit.
func (r *applyRun) report(out *applyOutcome) error {
	changed := []string(nil)
	if out.applyResp != nil {
		changed = out.applyResp.ChangedFiles
	}

	if r.applyOutput == config.ApplyOutputPatch {
		resolvedPatch := out.corePatchPath
		if resolvedPatch == "" {
			var err error
			resolvedPatch, err = resolvePatchOutputPath(r.cfg, r.cwd, r.patchOutPath)
			if err != nil {
				return err
			}
			var diffs []applier.FileDiff
			if out.applyResp != nil {
				diffs = out.applyResp.Diffs
			}
			if err := applier.WriteUnifiedPatch(resolvedPatch, diffs); err != nil {
				return fmt.Errorf("write patch: %w", err)
			}
			// apply.patch_dir keeps the newest retention.patches files; a
			// --output-patch path of the user's own is left alone.
			if r.patchOutPath == "" {
				retention.PruneFiles(filepath.Dir(resolvedPatch), ".patch", r.cfg.Retention.Patches)
			}
		}
		fmt.Printf("Patch mode: workspace untouched\n")
		fmt.Printf("Patch saved to: %s\n", resolvedPatch)
	}

	if len(changed) == 0 {
		fmt.Println("Changed files: (none)")
	} else {
		fmt.Printf("Changed files: %s\n", strings.Join(changed, ", "))
	}
	fmt.Printf("Dry-run: %v\n", r.dryRun)
	fmt.Printf("Plan saved to: %s\n", filepath.Join(r.cfg.ProjectRoot, ".orchestra", "plan.json"))
	fmt.Printf("Diff saved to: %s\n", filepath.Join(r.cfg.ProjectRoot, ".orchestra", "diff.txt"))

	// Git commit (if requested).
	if gitCommit {
		if r.dryRun {
			return fmt.Errorf("--git-commit requires --apply (not dry-run)")
		}
		if !git.IsRepo(r.cfg.ProjectRoot) {
			return fmt.Errorf("--git-commit requires a git repository")
		}
		commitMsg := fmt.Sprintf("feat(orchestra): %s", r.query)
		if err := git.CommitAll(r.cfg.ProjectRoot, commitMsg); err != nil {
			fmt.Fprintf(os.Stderr, "[orchestra] WARNING: failed to create git commit: %v\n", err)
		} else {
			fmt.Printf("✓ Created git commit: %s\n", commitMsg)
		}
	}
	return nil
}

// runApplyInProcess runs an apply turn through a core in this process: the
// same launch agent.run gets over RPC — options, subagents, skills, hooks,
// MCP, the run journal — with the terminal as its client.
//
// The direct path used to build all of that itself, and it had drifted from
// the core's: it lost exec.allow / exec.deny, the worker LLM verifier and
// BytesPerContextToken, and resolved clients and tools its own way (ARCH-1).
func runApplyInProcess(ctx context.Context, cfg *config.ProjectConfig, params core.AgentRunParams) (*core.AgentRunResult, error) {
	c, err := core.New(cfg.ProjectRoot, core.Options{
		Config:       cfg,
		LLMClient:    getTestLLMClient(),
		Debug:        debugMode,
		ExecInDryRun: true,
	})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	// Without a terminal there is nobody to ask: MCP sampling is refused and
	// elicitation declined, which is the intended non-interactive behaviour.
	if isTTY() {
		c.BindInteractive(&terminalConsent{in: os.Stdin, out: os.Stderr}, &tools.StdinQuestionAsker{})
	}
	params.OnEvent = printModeRoute
	return c.AgentRun(ctx, params)
}

// printModeRoute tells the terminal where mode=agent sent the turn.
func printModeRoute(method string, params any) {
	m, ok := params.(map[string]any)
	if method != "agent/event" || !ok || m["type"] != "mode_route" {
		return
	}
	data, _ := m["data"].(map[string]any)
	conf, _ := data["confidence"].(float64)
	fmt.Fprintf(os.Stderr, "[auto_router] agent → %v (%.0f%%) %v\n", data["to"], conf*100, data["reason"])
}

// takeCoreResult turns a core run's result into what apply records and
// prints. The core wrote its own usage record; the totals are shown here.
func takeCoreResult(query string, out *core.AgentRunResult) (int, planArtifact, *tools.FSApplyOpsResponse, string) {
	if out.Usage != nil {
		base := fmt.Sprintf("tokens: %d in + %d out = %d (%d call%s)",
			out.Usage.PromptTokens, out.Usage.CompletionTokens,
			out.Usage.TotalTokens, out.Usage.Calls, pluralS(out.Usage.Calls))
		if out.Usage.CostUSD > 0 {
			base = fmt.Sprintf("%s | $%.4f", base, out.Usage.CostUSD)
		}
		fmt.Fprintf(os.Stderr, "[usage] %s\n", base)
	}
	plan := planArtifact{
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
		Query:           query,
		GeneratedAtUnix: time.Now().Unix(),
		Patches:         out.Patches,
		Ops:             out.Ops,
	}
	return out.Steps, plan, out.ApplyResponse, out.PatchPath
}

func runApplyViaCore(cmd *cobra.Command, cfg *config.ProjectConfig, query string, allowExec bool, dryRun bool, backup bool, applyOutput, patchPath, profile string) (*core.AgentRunResult, error) {
	child, err := spawnCoreChild(cmd.Context(), cfg.ProjectRoot)
	if err != nil {
		return nil, err
	}
	defer child.Close()

	rpc := child.Client

	projectID, err := fsutil.ComputeProjectID(cfg.ProjectRoot)
	if err != nil {
		return nil, err
	}
	var initRes core.InitializeResult
	if err := rpc.Call(cmd.Context(), "initialize", core.InitializeParams{
		ProjectRoot:        cfg.ProjectRoot,
		ProjectID:          projectID,
		ProtocolVersion:    protocol.ProtocolVersion,
		MinProtocolVersion: protocol.MinProtocolVersion,
		OpsVersion:         protocol.OpsVersion,
		ToolsVersion:       protocol.ToolsVersion,
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
		MaxPromptBytes:    cfg.EffectiveMaxPromptBytes(),
		AllowExec:         allowExec,
		Debug:             debugMode,
		Mode:              agentMode,
		ApplyOutput:       applyOutput,
		PatchPath:         patchPath,
		Profile:           profile,
		Resume:            strings.TrimSpace(applyResume),
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

func resolvePatchOutputPath(cfg *config.ProjectConfig, cwd, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		p := explicit
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		return filepath.Abs(p)
	}
	dir := cfg.Apply.PatchDir
	if dir == "" {
		dir = ".orchestra/patches"
	}
	p := applier.DefaultPatchPath(dir)
	if !filepath.IsAbs(p) {
		p = filepath.Join(cfg.ProjectRoot, p)
	}
	return filepath.Abs(p)
}

type planArtifact struct {
	ProtocolVersion int `json:"protocol_version"`
	OpsVersion      int `json:"ops_version"`
	ToolsVersion    int `json:"tools_version"`

	Query           string `json:"query,omitempty"`
	GeneratedAtUnix int64  `json:"generated_at_unix"`

	// Optional: raw external patches from the model (if running with LLM).
	Patches []patches.Patch `json:"patches,omitempty"`
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
	// plan.json is the artifact --from-plan replays, so its write is the one
	// that fails the command; the diff, the result and the run log are for
	// reading and only warn (ARCH-11: every write here used to be ignored).
	warn := func(what string, err error) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "[orchestra] %s not written: %v\n", what, err)
		}
	}

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
		if err := fsutil.AtomicWriteFile(planPath, planJSON, 0600); err != nil {
			return fmt.Errorf("plan.json not written: %w", err)
		}
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
	warn("diff.txt", fsutil.AtomicWriteFile(diffPath, []byte(diffText.String()), 0600))

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
		warn("last_result.json", fsutil.AtomicWriteFile(resultPath, b, 0600))
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
	warn("last_run.jsonl", fsutil.AtomicWriteFile(runPath, []byte(jsonl.String()), 0600))

	return nil
}

// providerNames returns a sorted, comma-separated list of provider names from cfg.
// Used in error messages for --provider flag validation.
func providerNames(cfg *config.ProjectConfig) string {
	if len(cfg.Providers) == 0 {
		return "(none configured)"
	}
	names := make([]string, 0, len(cfg.Providers))
	for k := range cfg.Providers {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// buildCLIRenderer returns an OnEvent callback that renders streaming events to stderr
// when stderr or stdout is an interactive terminal. Returns nil (display only) when
// both are piped/redirected and --stream is not set. LLM streaming still runs without
// this callback — OnEvent controls UI output only.
func buildCLIRenderer() func(agent.AgentEvent) {
	if !isInteractiveTerminal() && !applyStream {
		return nil
	}
	// In --stream mode, route deltas to stdout so they're pipeable; otherwise
	// keep them on stderr where the TTY renderer originally wrote.
	dst := os.Stderr
	if applyStream {
		dst = os.Stdout
	}
	_ = dst // explicit no-op to keep dst in scope when callers below use it
	var lastStep int
	return func(ev agent.AgentEvent) {
		switch ev.Stream.Kind {
		case llm.StreamEventMessageDelta:
			fmt.Fprint(dst, ev.Stream.Content)
		case llm.StreamEventToolCallStart:
			if ev.Step != lastStep {
				fmt.Fprintln(dst)
				lastStep = ev.Step
			}
			fmt.Fprintf(dst, "\n→ %s", ev.Stream.ToolCallName)
		case llm.StreamEventToolCallCompleted:
			preview := strings.TrimSpace(ev.Stream.Content)
			if preview == "" {
				preview = "ok"
			}
			if len(preview) > 80 {
				preview = preview[:80] + "…"
			}
			fmt.Fprintf(dst, " ← %s\n", preview)
		case llm.StreamEventDone:
			if ev.Stream.Response != nil && ev.Stream.Response.Message.Content != "" {
				fmt.Fprintln(dst) // newline after streamed text
			}
		case llm.StreamEventExecOutput:
			fmt.Fprint(dst, ev.Stream.Content)
		}
	}
}

// buildQuestionAsker returns a StdinQuestionAsker when mode requires it and stdin is a terminal.
// hasGates forces the asker in any mode: required human gates (G2/G3) must be
// confirmable interactively, otherwise they deny fail-closed.
// Returns nil otherwise (disables the question tool) to avoid corrupting stdio JSON-RPC in core mode.
func buildQuestionAsker(mode string, hasGates bool) tools.QuestionAsker {
	if !isTTY() {
		return nil
	}
	if agent.Mode(mode) == agent.ModePlan || hasGates {
		return &tools.StdinQuestionAsker{}
	}
	return nil
}

// isInteractiveTerminal reports whether stderr or stdout is a character device.
func isInteractiveTerminal() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isCharDevice(os.Stdout) || isCharDevice(os.Stderr)
}

// isTTY reports whether os.Stdout is connected to an interactive terminal.
// Returns false when NO_COLOR is set or when stdout is piped/redirected.
func isTTY() bool {
	return isInteractiveTerminal() && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// compactionClientFor resolves the cheap model used for history compaction and
// auto-summaries: llm.router.fast_provider, else providers.fast. Returns nil
// when neither is configured (the caller then compacts with the main model),
// and never resolves a client when a test LLM is injected.
//
// Mirrors Core.compactionClientWithContext for the CLI paths.
func compactionClientFor(cfg *config.ProjectConfig, logger *llm.Logger) (llm.Client, int) {
	if cfg == nil || getTestLLMClient() != nil {
		return nil, 0
	}
	provider := strings.TrimSpace(cfg.LLM.Router.FastProvider)
	if provider == "" {
		if _, ok := cfg.FindProvider("fast"); ok {
			provider = "fast"
		}
	}
	if provider == "" {
		return nil, 0
	}
	client, _, err := app.ClientFor(cfg, provider, "", logger)
	if err != nil || client == nil {
		return nil, 0
	}
	ctxTok := 0
	if pcfg, ok := cfg.FindProvider(provider); ok {
		ctxTok = llm.ContextTokensFromConfig(pcfg)
	}
	return client, ctxTok
}

// printResumeHint tells the user how to go on with a run that ended in an
// error: it keeps a checkpoint, and resuming continues from its last step.
func printResumeHint(projectRoot string) {
	if cp, err := checkpoint.Latest(projectRoot); err == nil {
		fmt.Fprintf(os.Stderr, "The run can be resumed from its last step: orchestra apply --resume %s\n", cp.RunID)
	}
}
