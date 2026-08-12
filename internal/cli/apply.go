package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/internal/autorouter"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/internal/git"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/internal/pipeline"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tasks"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/spf13/cobra"
)

var (
	applyFlag           bool
	gitStrict           bool
	gitCommit           bool
	planOnly            bool
	fromPlan            string
	noDaemon            bool
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
	applyCmd.Flags().BoolVar(&allowWeb, "allow-web", false, "Allow webfetch tool (fetches external URLs; private IPs blocked)")
	applyCmd.Flags().BoolVar(&allowBrowser, "allow-browser", false, "Allow browser.* tools (requires Node.js and npx)")
	applyCmd.Flags().BoolVar(&viaCore, "via-core", false, "Run via JSON-RPC core subprocess (stdio)")
	applyCmd.Flags().StringVar(&agentMode, "mode", "", "Agent mode: build|plan|explore|ask|debug|architecture|agent|orchestra (or custom agents: name)")
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

	if wt := strings.TrimSpace(applyWorktree); wt != "" {
		wtPath, wtErr := git.ResolveManagedWorktree(cfg.ProjectRoot, wt)
		if wtErr != nil {
			return fmt.Errorf("--worktree %q: %w", wt, wtErr)
		}
		cfg.ProjectRoot = wtPath
	}

	applyOutput := strings.ToLower(strings.TrimSpace(cfg.Apply.Output))
	if applyOutput == "" {
		applyOutput = config.ApplyOutputDisk
	}
	patchOutPath := ""
	if cmd.Flags().Changed("output-patch") {
		applyOutput = config.ApplyOutputPatch
		if outputPatch != "" && outputPatch != "AUTO" {
			patchOutPath = outputPatch
		}
	}
	if applyOutput == config.ApplyOutputPatch {
		if applyFlag {
			return fmt.Errorf("--output-patch / apply.output=patch is mutually exclusive with --apply")
		}
		dryRun = true
		backup = false
	}

	profileName := strings.TrimSpace(cfg.Agent.Profile)
	if applyProfile != "" {
		profileName = applyProfile
	}
	if !agent.IsKnownProfile(profileName) {
		return fmt.Errorf("unknown --profile / agent.profile %q (want fast|precision)", profileName)
	}

	if applyProvider != "" {
		provCfg, ok := cfg.FindProvider(applyProvider)
		if !ok {
			return fmt.Errorf("--provider %q: not found in .orchestra.yml providers: section\nAvailable: %s",
				applyProvider, providerNames(cfg))
		}
		cfg.LLM = provCfg
	}

	if applySkill != "" {
		if agentMode != "" {
			return fmt.Errorf("--skill and --mode are mutually exclusive")
		}
		def, err := resolveSkillAgent(cfg.ProjectRoot, applySkill, query)
		if err != nil {
			return err
		}
		if cfg.FindAgent(def.Name) != nil {
			return fmt.Errorf("--skill %q: name collides with an existing entry in agents: in .orchestra.yml", def.Name)
		}
		cfg.Agents = append(cfg.Agents, *def)
		agentMode = def.Name
	}

	if agentMode != "" && !config.IsBuiltInMode(agentMode) && cfg.FindAgent(agentMode) == nil {
		return fmt.Errorf("unknown agent mode %q: not a built-in mode and not defined in agents: in .orchestra.yml", agentMode)
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
	corePatchPath := ""
	usageTracker := newUsageTracker("apply", cfg)

	defer func() {
		// Always write artifacts once we know projectRoot.
		_ = writeApplyArtifacts(cfg.ProjectRoot, plan, applyResp, dryRun, startedAt, time.Now(), mode, steps, retErr)
		finalizeUsage(usageTracker, cfg)
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
	// If web.confirm=false in config, we can allow webfetch without --allow-web.
	allowWebEffective := allowWeb
	if cfg.Web.Confirm != nil && !*cfg.Web.Confirm {
		allowWebEffective = true
	}
	allowBrowserEffective := allowBrowser
	// (no config override for browser — always requires explicit flag)
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
			ExcludeDirs:        cfg.ExcludeDirs,
			ExecTimeout:        time.Duration(cfg.Exec.TimeoutS) * time.Second,
			ExecOutputLimit:    cfg.Exec.OutputLimitKB * 1024,
			WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
			WebMaxContentBytes: cfg.Web.MaxContentBytes,
			WebSearch:          cfg.Web.Search,
			LSP:                cfg.LSP,
			DryRun:             dryRun,
			Browser:            cfg.Browser,
			AllowBrowser:       allowBrowserEffective,
			Embed:              cfg.Embed,
		})
		if err != nil {
			retErr = err
			return retErr
		}
		defer runner.Close()

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
		out, err := runApplyViaCore(cmd, cfg, query, allowExecEffective, dryRun, backup, applyOutput, patchOutPath, profileName)
		if err != nil {
			retErr = err
			return retErr
		}
		if out.Usage != nil {
			// Child already wrote its own usage.jsonl record; just surface the
			// totals to the user. Parent tracker stays empty so finalizeUsage
			// is a no-op.
			base := fmt.Sprintf("tokens: %d in + %d out = %d (%d call%s)",
				out.Usage.PromptTokens, out.Usage.CompletionTokens,
				out.Usage.TotalTokens, out.Usage.Calls, pluralS(out.Usage.Calls))
			if out.Usage.CostUSD > 0 {
				base = fmt.Sprintf("%s | $%.4f", base, out.Usage.CostUSD)
			}
			fmt.Fprintf(os.Stderr, "[usage] %s\n", base)
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
		corePatchPath = out.PatchPath

	} else if pipelineMode {
		// --- Mode: multi-agent pipeline (Investigator → Coder → Critic) ---
		mode = "pipeline"

		var llmClient llm.Client
		if getTestLLMClient() != nil {
			llmClient = getTestLLMClient()
		} else {
			llmClient = llm.NewClient(cfg.LLM)
			if oc, ok := llmClient.(*llm.OpenAIClient); ok {
				oc.SetLogger(llm.NewLogger(cfg.ProjectRoot))
			}
			llmClient = llm.MaybeWrapRouter(llmClient, cfg.LLMRegistry(), cfg.LLM.Router)
		}

		validator, err := schema.NewValidator()
		if err != nil {
			retErr = err
			return retErr
		}
		runner, err := tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{
			ExcludeDirs:        cfg.ExcludeDirs,
			ExecTimeout:        time.Duration(cfg.Exec.TimeoutS) * time.Second,
			ExecOutputLimit:    cfg.Exec.OutputLimitKB * 1024,
			WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
			WebMaxContentBytes: cfg.Web.MaxContentBytes,
			WebSearch:          cfg.Web.Search,
			LSP:                cfg.LSP,
			DryRun:             dryRun,
			Browser:            cfg.Browser,
			AllowBrowser:       allowBrowserEffective,
			Embed:              cfg.Embed,
		})
		if err != nil {
			retErr = err
			return retErr
		}
		defer runner.Close()

	respFmt := agent.ResolveResponseFormat(cfg.LLM, providerLabelFor(cfg, applyProvider), agent.ResponseFormatToolAgent)

		var agentLogger *llm.Logger
		if openAIClient, ok := llmClient.(*llm.OpenAIClient); ok {
			agentLogger = openAIClient.GetLogger()
		}

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

		pipeRes, err := pipeline.Run(cmd.Context(), llmClient, validator, runner, query, pipeline.Options{
			UsageTracker:         usageTracker,
			ProviderLabel:        providerLabelFor(cfg, applyProvider),
			ModelLabel:           cfg.LLM.Model,
			MaxCoderAttempts:     pipelineMaxAttempts,
			Apply:                !dryRun,
			Backup:               backup,
			TraceCtx:             traceCtx,
			MaxStepsCoder:        cfg.Agent.MaxSteps,
			MaxInvalidRetries:    cfg.Agent.MaxInvalidRetries,
			MaxDeniedToolRepeats: cfg.Agent.MaxDeniedRepeats,
			MaxToolErrorRepeats:  cfg.Agent.MaxToolErrors,
			MaxFinalFailures:     cfg.Agent.MaxFinalFailures,
			MaxPromptBytes:       cfg.EffectiveMaxPromptBytes(),
			CompactThresholdPct:  cfg.Agent.CompactThresholdPct,
			ModelContextTokens:   int(cfg.EffectiveNumCtx()),
			CompletionMaxTokens:  cfg.LLM.MaxTokens,
			LLMStepTimeout:       time.Duration(cfg.LLM.TimeoutS) * time.Second,
			PromptFamily:         promptpkg.ResolvePromptFamily(cfg.LLM.PromptFamily, cfg.LLM.Model),
			ResponseFormat:       respFmt,
			Debug:                debugMode,
			AgentLogger:          agentLogger,
			OnEvent:              onPipelineEvent,
			PermissionRules:      cfg.Permissions.Rules,
		})
		if err != nil {
			retErr = err
			return retErr
		}

		totalSteps := 0
		for _, sr := range pipeRes.StageResults {
			totalSteps += sr.Steps
		}
		steps = totalSteps

		if !pipeRes.Accepted {
			fmt.Fprintln(os.Stderr, "[pipeline] WARNING: Critic did not accept after all attempts — using last Coder output")
		} else {
			fmt.Fprintf(os.Stderr, "[pipeline] Critic accepted after %d attempt(s)\n", pipeRes.Attempts)
		}

		plan = planArtifact{
			ProtocolVersion: protocol.ProtocolVersion,
			OpsVersion:      protocol.OpsVersion,
			ToolsVersion:    protocol.ToolsVersion,
			Query:           query,
			GeneratedAtUnix: time.Now().Unix(),
			Patches:         pipeRes.Patches,
			Ops:             pipeRes.Ops,
		}
		applyResp = pipeRes.ApplyResponse

	} else {
		// --- Mode: direct (agent + tools) ---
		mode = "direct"

		// LLM client: use test client if set, otherwise create real client.
		var llmClient llm.Client
		if getTestLLMClient() != nil {
			llmClient = getTestLLMClient()
		} else {
			llmClient = llm.NewClient(cfg.LLM)
			if oc, ok := llmClient.(*llm.OpenAIClient); ok {
				oc.SetLogger(llm.NewLogger(cfg.ProjectRoot))
			}
			llmClient = llm.MaybeWrapRouter(llmClient, cfg.LLMRegistry(), cfg.LLM.Router)
		}

		validator, err := schema.NewValidator()
		if err != nil {
			retErr = err
			return retErr
		}
		runner, err := tools.NewRunner(cfg.ProjectRoot, tools.RunnerOptions{
			ExcludeDirs:        cfg.ExcludeDirs,
			ExecTimeout:        time.Duration(cfg.Exec.TimeoutS) * time.Second,
			ExecOutputLimit:    cfg.Exec.OutputLimitKB * 1024,
			WebFetchTimeout:    time.Duration(cfg.Web.FetchTimeoutS) * time.Second,
			WebMaxContentBytes: cfg.Web.MaxContentBytes,
			WebSearch:          cfg.Web.Search,
			LSP:                cfg.LSP,
			DryRun:             dryRun,
			Browser:            cfg.Browser,
			AllowBrowser:       allowBrowserEffective,
			Embed:              cfg.Embed,
		})
		if err != nil {
			retErr = err
			return retErr
		}
		defer runner.Close()

		// Wire MCP servers if configured.
		var mcpExtraTools []llm.ToolDef
		if len(cfg.MCP.Servers) > 0 {
			mcpMgr, mcpErrs := mcp.NewManager(cmd.Context(), cfg.MCP)
			for _, e := range mcpErrs {
				fmt.Fprintf(os.Stderr, "orchestra: mcp startup warning: %v\n", e)
			}
			if !mcpMgr.IsEmpty() {
				runner.SetMCPCaller(mcpMgr)
				mcpExtraTools = mcpMgr.ListToolDefs()
				defer mcpMgr.Close()
			}
		}

		// Expose semantic_search only when an embedding model is configured.
		// The tool short-circuits with a clear error if the CKG index is empty;
		// gating just by config keeps the model from "discovering" it on every
		// run that doesn't have embeddings set up.
		if cfg.Embed.Model != "" {
			mcpExtraTools = append(mcpExtraTools, tools.ToolSemanticSearch())
		}
		mcpExtraTools = append(mcpExtraTools, tools.ToolRepoMap())

	respFmt := agent.ResolveResponseFormat(cfg.LLM, providerLabelFor(cfg, applyProvider), agent.ResponseFormatToolAgent)

		var agentLogger *llm.Logger
		if openAIClient, ok := llmClient.(*llm.OpenAIClient); ok {
			agentLogger = openAIClient.GetLogger()
		}

		requestedMode := agentMode
		if strings.EqualFold(agentMode, string(agent.ModeAgent)) {
			routerClient := llmClient
			if getTestLLMClient() == nil && cfg.AutoRouter.ResolvedEnabled() {
				prov := strings.TrimSpace(cfg.AutoRouter.Provider)
				model := strings.TrimSpace(cfg.AutoRouter.Model)
				if prov == "" {
					prov = strings.TrimSpace(cfg.LLM.Router.FastProvider)
				}
				if prov == "" && model == "" {
					if _, ok := cfg.FindProvider("fast"); ok {
						prov = "fast"
					}
				}
				if prov != "" || model != "" {
					if c, err := namedLLMClient(cfg, prov, model, agentLogger); err == nil {
						routerClient = c
					}
				}
			}
			dec := autorouter.Classify(cmd.Context(), routerClient, query)
			agentMode = dec.Mode
			fmt.Fprintf(os.Stderr, "[auto_router] agent → %s (%.0f%%) %s\n", dec.Mode, dec.Confidence*100, dec.Reason)
		}

		if strings.EqualFold(agentMode, string(agent.ModeOrchestra)) && getTestLLMClient() == nil {
			p := strings.TrimSpace(cfg.Orchestra.Planner.Provider)
			m := strings.TrimSpace(cfg.Orchestra.Planner.Model)
			if p == "" && m == "" {
				if role, ok := cfg.Routing.ResolveRole("L5"); ok {
					p = strings.TrimSpace(role.Provider)
					m = strings.TrimSpace(role.Model)
				}
			}
			if p != "" || m != "" {
				if c, err := namedLLMClient(cfg, p, m, agentLogger); err == nil {
					llmClient = c
					fmt.Fprintf(os.Stderr, "[orchestra] planner %s / %s\n", p, m)
				}
			}
		}
		_ = requestedMode

		// Custom agent override: look up agentMode in agents: config block.
		var systemPromptOverride string
		var customAgentTools []llm.ToolDef
		if agentMode != "" {
			if def := cfg.FindAgent(agentMode); def != nil {
				systemPromptOverride = def.SystemPrompt
				if def.Provider != "" && getTestLLMClient() == nil {
					if provCfg, ok := cfg.FindProvider(def.Provider); ok {
						if def.Model != "" {
							provCfg.Model = def.Model
						}
						newClient := llm.NewClient(provCfg)
						if oc, ok2 := newClient.(*llm.OpenAIClient); ok2 && agentLogger != nil {
							oc.SetLogger(agentLogger)
						}
						llmClient = newClient
					} else {
						retErr = fmt.Errorf("agent %q: provider %q not found in providers: section", agentMode, def.Provider)
						return retErr
					}
				} else if def.Model != "" && getTestLLMClient() == nil {
					overrideCfg := cfg.LLM
					overrideCfg.Model = def.Model
					newClient := llm.NewClient(overrideCfg)
					if oc, ok := newClient.(*llm.OpenAIClient); ok && agentLogger != nil {
						oc.SetLogger(agentLogger)
					}
					llmClient = newClient
				}
				if def.Tools != nil {
					var resolveErr error
					customAgentTools, resolveErr = tools.ResolveToolNamesWithPolicy(def.Tools, tools.Capabilities{
						Exec:    allowExecEffective,
						Web:     allowWebEffective,
						Browser: allowBrowserEffective,
					})
					if resolveErr != nil {
						retErr = resolveErr
						return retErr
					}
					// C7 in audit ledger: opt-in MCP for custom agents. When
					// the agent declares `mcp:*` or `*` in its tools list it
					// gets the live MCP tool definitions appended; otherwise
					// MCP stays out (agent.Options.ExtraTools is ignored when
					// CustomTools is set, so this is the only injection point).
					for _, name := range def.Tools {
						if name == "mcp:*" || name == "*" {
							customAgentTools = append(customAgentTools, mcpExtraTools...)
							break
						}
					}
				}
			}
		}

		imageParts, err := loadImageParts(applyImages)
		if err != nil {
			retErr = err
			return retErr
		}
		if len(imageParts) > 0 && !cfg.LLM.Multimodal {
			retErr = fmt.Errorf("--image: configured LLM is not marked multimodal in .orchestra.yml (set llm.multimodal: true after switching to a VL model)")
			return retErr
		}

		workerVerifyEnabled := cfg.Orchestra.ResolvedWorkerVerifyEnabled()
		cliQuestionAsker := buildQuestionAsker(agentMode, len(cfg.Orchestra.RequiredGates()) > 0)
		taskRunner := tasks.New(llmClient, validator, runner, tasks.ChildAgentConfig{
			MaxPromptBytes:         cfg.EffectiveMaxPromptBytes(),
			CompactThresholdPct:    cfg.Agent.CompactThresholdPct,
			ModelContextTokens:     int(cfg.EffectiveNumCtx()),
			CompletionMaxTokens:    cfg.LLM.MaxTokens,
			ToolDigestBytes:        cfg.Agent.ResolvedToolDigestBytes(),
			HistoryPruneKeepRecent: cfg.Agent.ResolvedHistoryPruneKeepRecent(),
			LLMStepTimeout:         time.Duration(cfg.LLM.TimeoutS) * time.Second,
			MaxStepsCap:            cfg.Agent.ResolvedChildMaxSteps(),
			UsageTracker:           usageTracker,
			ProviderLabel:          providerLabelFor(cfg, applyProvider),
			ModelLabel:             cfg.LLM.Model,
			MaxWorkerRetries:       cfg.Orchestra.ResolvedMaxWorkerRetries(),
			MaxWorkerVerifyRetries: cfg.Orchestra.ResolvedMaxWorkerVerifyRetries(),
			WorkerVerifyEnabled:    &workerVerifyEnabled,
			WorkerVerifyAffectedTests:     cfg.Orchestra.WorkerVerifyAffectedTests,
			WorkerVerifyFrontendTypecheck: cfg.Orchestra.WorkerVerifyFrontendTypecheck,
			QuestionAsker:                 cliQuestionAsker,
			MaxClarificationRounds:        cfg.Orchestra.ResolvedMaxClarificationRounds(),
			RelayViaLLM:                   cfg.Orchestra.ResolvedRelayViaLLM(),
			PhaseTimeouts: orchestrastate.PhaseTimeouts{
				DiscoveryS:       cfg.Orchestra.PhaseTimeouts.ResolvedDiscoveryS(),
				ContractS:        cfg.Orchestra.PhaseTimeouts.ResolvedContractS(),
				LeadBriefS:       cfg.Orchestra.PhaseTimeouts.ResolvedLeadBriefS(),
				BlockedEscalateS: cfg.Orchestra.PhaseTimeouts.ResolvedBlockedEscalateS(),
			},
			TierEscalation: tasks.TierEscalationSettings{
				Enabled:                  cfg.Orchestra.TierEscalation.ResolvedEnabled(),
				FailuresBeforeEscalation: cfg.Orchestra.TierEscalation.ResolvedFailuresBeforeEscalation(),
				MaxEscalatedRetries:      cfg.Orchestra.TierEscalation.ResolvedMaxEscalatedRetries(),
				EscalationTier:           cfg.Orchestra.TierEscalation.ResolvedEscalationTier(),
			},
			Caps: tools.Capabilities{
				Exec:    allowExecEffective,
				Web:     allowWebEffective,
				Browser: allowBrowserEffective,
			},
			ResolveClient: func(provider, model string) (llm.Client, string, string, error) {
				if getTestLLMClient() != nil {
					return llmClient, provider, model, nil
				}
				c, err := namedLLMClient(cfg, provider, model, agentLogger)
				if err != nil {
					return nil, "", "", err
				}
				pl, ml := provider, model
				if pl == "" {
					pl = providerLabelFor(cfg, applyProvider)
				}
				if ml == "" {
					ml = cfg.LLM.Model
				}
				return c, pl, ml, nil
			},
			ResolveTier: func(tier string) (provider, model string, ok bool) {
				return cfg.ResolveTierBinding(tier)
			},
			GuardSpawn: func(subagentType string) error {
				return orchestrastate.GuardSpawn(cfg.ProjectRoot, cfg.Orchestra.ResolvedPhaseEnforcement(), subagentType)
			},
			GuardContractRefs: func(refs []contract.Ref) error {
				return orchestrastate.GuardWorkOrderContract(cfg.ProjectRoot, cfg.Orchestra.ResolvedPhaseEnforcement(), refs)
			},
			RouteTaskType: func(taskType string) (tasks.TaskTypeRoute, bool) {
				rule, ok := cfg.Routing.Route(taskType)
				if !ok {
					return tasks.TaskTypeRoute{}, false
				}
				route := tasks.TaskTypeRoute{
					SubagentType: rule.SubagentType,
					Tier:         rule.Tier,
				}
				if role, found := cfg.Routing.ResolveRole(rule.RequiredTier); found {
					route.Provider = strings.TrimSpace(role.Provider)
					route.Model = strings.TrimSpace(role.Model)
				}
				return route, true
			},
		})
		var hooksRunner agent.HooksRunner
		if hr := hooks.New(cfg.Hooks, cfg.ProjectRoot); hr != nil {
			hooksRunner = hr
		}

		// Discover skills once; expose skill_invoke when any are present.
		// Skipped silently on error so a malformed skill file doesn't kill
		// regular apply flow — the error will resurface on `orchestra skills list`.
		discoveredSkills, _ := skills.DiscoverCached(cfg.ProjectRoot)
		discoveredRefs, _ := skills.DiscoverRefs(cfg.ProjectRoot)
		var skillRunner agent.SkillRunner
		var skillSpecsList []agent.SkillSpec
		if len(discoveredSkills) > 0 {
			skillRunner = newCLISkillRunner(cfg, discoveredSkills, discoveredRefs, llmClient, validator, runner, agentLogger, cfg.Agent.MaxSteps, allowExecEffective, allowWebEffective, allowBrowserEffective)
			skillSpecsList = skillSpecs(discoveredSkills)
		}

		agOpts := agent.Options{
			UsageTracker:         usageTracker,
			ProviderLabel:        providerLabelFor(cfg, applyProvider),
			ModelLabel:           cfg.LLM.Model,
			MaxSteps:             cfg.Agent.MaxSteps,
			MaxInvalidRetries:    cfg.Agent.MaxInvalidRetries,
			MaxDeniedToolRepeats: cfg.Agent.MaxDeniedRepeats,
			MaxToolErrorRepeats:  cfg.Agent.MaxToolErrors,
			MaxFinalFailures:     cfg.Agent.MaxFinalFailures,
			MaxPromptBytes:       cfg.EffectiveMaxPromptBytes(),
			CompactThresholdPct:  cfg.Agent.CompactThresholdPct,
			ModelContextTokens:   int(cfg.EffectiveNumCtx()),
			CompletionMaxTokens:  cfg.LLM.MaxTokens,
			LLMStepTimeout:       time.Duration(cfg.LLM.TimeoutS) * time.Second,
			Apply:                !dryRun,
			Backup:               backup,
			AllowExec:            allowExecEffective,
			AllowWeb:             allowWebEffective,
			AllowBrowser:         allowBrowserEffective,
			PermissionRules:      cfg.Permissions.Rules,
			Debug:                debugMode,
			ResponseFormat:       respFmt,
			PromptFamily:         promptpkg.ResolvePromptFamily(cfg.LLM.PromptFamily, cfg.LLM.Model),
			Mode:                 agent.Mode(agentMode),
			SystemPromptOverride: systemPromptOverride,
			CustomTools:          customAgentTools,
			ExtraTools:           mcpExtraTools,
			QuestionAsker:        cliQuestionAsker,
			HumanGates:           cfg.Orchestra.RequiredGates(),
			StateMaxBytes:        cfg.Orchestra.ResolvedStateMaxBytes(),
			OnEvent:              buildCLIRenderer(),
			AgentLogger:          agentLogger,
			SubtaskRunner:        taskRunner,
			Skills:               skillSpecsList,
			SkillRunner:          skillRunner,
			HooksRunner:          hooksRunner,
			UserImages:           imageParts,
			MultimodalLLM:        cfg.LLM.Multimodal,
		}
		agent.ApplyHistoryConfig(&agOpts, cfg)
		if getTestLLMClient() == nil {
			fp := strings.TrimSpace(cfg.LLM.Router.FastProvider)
			if fp == "" {
				if _, ok := cfg.FindProvider("fast"); ok {
					fp = "fast"
				}
			}
			if fp != "" {
				if c, err := namedLLMClient(cfg, fp, "", agentLogger); err == nil {
					agOpts.CompactionClient = c
					if pcfg, ok := cfg.FindProvider(fp); ok {
						agOpts.CompactionContextTokens = llm.ContextTokensFromConfig(pcfg)
					}
				}
			}
		}
		// Profile overlays defaults; named agents: (CustomTools / SystemPromptOverride /
		// provider) already applied above and take precedence for those fields.
		if err := agent.ApplyProfile(&agOpts, profileName, true); err != nil {
			retErr = err
			return retErr
		}
		agent.FillRetryLimits(&agOpts, agOpts.ProviderLabel)
		// llm.timeout_s always wins — profiles must not shrink the step budget.
		if t := time.Duration(cfg.LLM.TimeoutS) * time.Second; t > 0 {
			agOpts.LLMStepTimeout = t
		}
		// Restore custom-agent tool/prompt overrides if profile filtered tools.
		if systemPromptOverride != "" {
			agOpts.SystemPromptOverride = systemPromptOverride
		}
		if customAgentTools != nil {
			agOpts.CustomTools = customAgentTools
		}

		ag, err := agent.New(llmClient, validator, runner, agOpts)
		if err != nil {
			retErr = err
			return retErr
		}

		var hist []llm.Message
		hist, res, err := ag.Run(cmd.Context(), nil, query)
		if err != nil {
			retErr = err
			return retErr
		}
		_, res, err = agent.ContinueBuildAfterPlan(cmd.Context(), llmClient, validator, runner, agOpts, hist, res)
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

	if applyOutput == config.ApplyOutputPatch {
		resolvedPatch := corePatchPath
		if resolvedPatch == "" {
			var err error
			resolvedPatch, err = resolvePatchOutputPath(cfg, cwd, patchOutPath)
			if err != nil {
				retErr = err
				return retErr
			}
			var diffs []applier.FileDiff
			if applyResp != nil {
				diffs = applyResp.Diffs
			}
			if err := applier.WriteUnifiedPatch(resolvedPatch, diffs); err != nil {
				retErr = fmt.Errorf("write patch: %w", err)
				return retErr
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

func runApplyViaCore(cmd *cobra.Command, cfg *config.ProjectConfig, query string, allowExec bool, dryRun bool, backup bool, applyOutput, patchPath, profile string) (*core.AgentRunResult, error) {
	child, err := spawnCoreChild(cmd.Context(), cfg.ProjectRoot)
	if err != nil {
		return nil, err
	}
	defer child.Close()

	rpc := child.Client

	projectID, err := cache.ComputeProjectID(cfg.ProjectRoot)
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
		MaxPromptBytes:    cfg.EffectiveMaxPromptBytes(),
		AllowExec:         allowExec,
		Debug:             debugMode,
		Mode:              agentMode,
		ApplyOutput:       applyOutput,
		PatchPath:         patchPath,
		Profile:           profile,
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
		_ = fsutil.AtomicWriteFile(planPath, planJSON, 0600)
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
	_ = fsutil.AtomicWriteFile(diffPath, []byte(diffText.String()), 0600)

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
		_ = fsutil.AtomicWriteFile(resultPath, b, 0600)
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
	_ = fsutil.AtomicWriteFile(runPath, []byte(jsonl.String()), 0600)

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

// namedLLMClient builds a client from providers: map and/or model override.
func namedLLMClient(cfg *config.ProjectConfig, provider, model string, logger *llm.Logger) (llm.Client, error) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider != "" {
		provCfg, ok := cfg.FindProvider(provider)
		if !ok {
			return nil, fmt.Errorf("provider %q not found in providers", provider)
		}
		if model != "" {
			provCfg.Model = model
		}
		client := llm.NewClient(provCfg)
		if oc, ok := client.(*llm.OpenAIClient); ok && logger != nil {
			oc.SetLogger(logger)
		}
		return client, nil
	}
	if model != "" {
		override := cfg.LLM
		override.Model = model
		client := llm.NewClient(override)
		if oc, ok := client.(*llm.OpenAIClient); ok && logger != nil {
			oc.SetLogger(logger)
		}
		return client, nil
	}
	return nil, fmt.Errorf("provider or model required")
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
