package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/stageinvoke"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/workflow"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/spf13/cobra"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Manage and run multi-stage skill workflows",
	Long:  "Workflows orchestrate skills as a DAG with completion-marker routing.\nDefined as YAML files in .orchestra/workflows/ (project) or ~/.orchestra/workflows/ (user).",
}

var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List discovered workflows",
	RunE:  runWorkflowList,
}

var workflowShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print a workflow's structure",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflowShow,
}

var workflowRunCmd = &cobra.Command{
	Use:   "run <name> [arguments...]",
	Short: "Execute a workflow",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runWorkflowRun,
}

var (
	workflowApplyFlag        bool
	workflowAllowExecFlag    bool
	workflowAllowWebFlag     bool
	workflowAllowBrowserFlag bool
)

func init() {
	workflowRunCmd.Flags().BoolVar(&workflowApplyFlag, "apply", false, "Allow workflow stages to write files (default: dry-run; ops are staged in-memory)")
	workflowRunCmd.Flags().BoolVar(&workflowAllowExecFlag, "allow-exec", false, "Allow bash/git.commit/git.push in workflow stages (DANGEROUS)")
	workflowRunCmd.Flags().BoolVar(&workflowAllowWebFlag, "allow-web", false, "Allow webfetch/websearch in workflow stages")
	workflowRunCmd.Flags().BoolVar(&workflowAllowBrowserFlag, "allow-browser", false, "Allow browser.* tools in workflow stages")
	workflowCmd.AddCommand(workflowListCmd, workflowShowCmd, workflowRunCmd)
	rootCmd.AddCommand(workflowCmd)
}

func loadWorkflows(cwd string) (*config.ProjectConfig, []*workflow.Workflow, error) {
	cfg, err := config.Load(filepath.Join(cwd, ".orchestra.yml"))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w (run 'orchestra init' first)", err)
	}
	ws, err := workflow.Discover(cfg.ProjectRoot)
	if err != nil {
		return cfg, nil, err
	}
	return cfg, ws, nil
}

func runWorkflowList(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	_, ws, err := loadWorkflows(cwd)
	if err != nil {
		return err
	}
	if len(ws) == 0 {
		fmt.Println("No workflows found. Create one in .orchestra/workflows/<name>.yaml")
		return nil
	}
	fmt.Printf("%-20s  %s\n", "NAME", "DESCRIPTION")
	for _, w := range ws {
		desc := w.Description
		if desc == "" {
			desc = fmt.Sprintf("%d stage(s)", len(w.Stages))
		}
		fmt.Printf("%-20s  %s\n", w.Name, desc)
	}
	return nil
}

func runWorkflowShow(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	_, ws, err := loadWorkflows(cwd)
	if err != nil {
		return err
	}
	w := workflow.Find(ws, args[0])
	if w == nil {
		return fmt.Errorf("workflow %q not found", args[0])
	}
	fmt.Printf("Workflow: %s\n", w.Name)
	if w.Description != "" {
		fmt.Printf("Description: %s\n", w.Description)
	}
	fmt.Printf("Source: %s\n\n", w.Source)
	order, err := workflow.TopoSort(w)
	if err != nil {
		return err
	}
	fmt.Println("Stages (topological order):")
	for i, id := range order {
		var stage *workflow.Stage
		for j := range w.Stages {
			if w.Stages[j].ID == id {
				stage = &w.Stages[j]
				break
			}
		}
		extras := []string{}
		if stage.Parallel > 1 {
			extras = append(extras, fmt.Sprintf("parallel=%d", stage.Parallel))
		}
		if stage.LoopUntilMarker != "" {
			extras = append(extras, fmt.Sprintf("loop_until=%q", stage.LoopUntilMarker))
		}
		if stage.MaxAttempts > 0 {
			extras = append(extras, fmt.Sprintf("max_attempts=%d", stage.MaxAttempts))
		}
		extraStr := ""
		if len(extras) > 0 {
			extraStr = " (" + strings.Join(extras, ", ") + ")"
		}
		depStr := ""
		if len(stage.DependsOn) > 0 {
			depStr = " ← " + strings.Join(stage.DependsOn, ", ")
		}
		fmt.Printf("  %d. %s → skill=%s%s%s\n", i+1, stage.ID, stage.Skill, extraStr, depStr)
	}
	return nil
}

func runWorkflowRun(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	cfg, ws, err := loadWorkflows(cwd)
	if err != nil {
		return err
	}
	w := workflow.Find(ws, args[0])
	if w == nil {
		return fmt.Errorf("workflow %q not found", args[0])
	}
	arguments := strings.TrimSpace(strings.Join(args[1:], " "))
	if arguments == "" {
		return fmt.Errorf("workflow %q: missing arguments (the user task)", w.Name)
	}

	discoveredSkills, err := skills.DiscoverCached(cfg.ProjectRoot)
	if err != nil {
		return fmt.Errorf("workflow: discover skills: %w", err)
	}
	discoveredRefs, err := skills.DiscoverRefs(cfg.ProjectRoot)
	if err != nil {
		return fmt.Errorf("workflow: discover refs: %w", err)
	}

	// Validate every stage's skill exists before doing any LLM work.
	for _, stage := range w.Stages {
		if skills.Find(discoveredSkills, stage.Skill) == nil {
			return fmt.Errorf("workflow %q: stage %q references unknown skill %q (run `orchestra skills list` to see available skills)", w.Name, stage.ID, stage.Skill)
		}
	}
	// loop_until_marker must reference a skill with at least one completion marker.
	if err := workflow.ValidateAgainstSkills(w, func(name string) []string {
		if s := skills.Find(discoveredSkills, name); s != nil {
			return s.CompletionMarkers
		}
		return nil
	}); err != nil {
		return err
	}

	dryRun := !workflowApplyFlag
	allowExecEffective := workflowAllowExecFlag
	if cfg.Exec.Confirm != nil && !*cfg.Exec.Confirm {
		allowExecEffective = true
	}
	allowWebEffective := workflowAllowWebFlag
	allowBrowserEffective := workflowAllowBrowserFlag
	usageTracker := newUsageTracker("workflow:"+w.Name, cfg)

	llmClient := llm.NewClient(cfg.LLM)
	if oc, ok := llmClient.(*llm.OpenAIClient); ok {
		oc.SetLogger(llm.NewLogger(cfg.ProjectRoot))
	}
	llmClient = llm.MaybeWrapRouter(llmClient, cfg.LLMRegistry(), cfg.LLM.Router)

	validator, err := schema.NewValidator()
	if err != nil {
		return err
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
		Embed:              cfg.ResolvedEmbed(),
	})
	if err != nil {
		return err
	}
	defer runner.Close()

	var agentLogger *llm.Logger
	if oc, ok := llmClient.(*llm.OpenAIClient); ok {
		agentLogger = oc.GetLogger()
	}
	var hooksRunner agent.HooksRunner
	if hr := hooks.New(cfg.Hooks, cfg.ProjectRoot); hr != nil {
		hooksRunner = hr
	}

	workflowCompactionClient, workflowCompactionCtxTokens := compactionClientFor(cfg, agentLogger)
	inv := stageinvoke.New(stageinvoke.Config{
		Cfg:           cfg,
		Skills:        discoveredSkills,
		Refs:          discoveredRefs,
		Client:        llmClient,
		Validator:     validator,
		Runner:        runner,
		AllowExec:     allowExecEffective,
		AllowWeb:      allowWebEffective,
		AllowBrowser:  allowBrowserEffective,
		AgentLogger:   agentLogger,
		HooksRunner:   hooksRunner,
		UsageTracker:  usageTracker,
		ProviderLabel: providerLabelFor(cfg, ""),
		ModelLabel:    cfg.LLM.Model,

		CompactionClient:        workflowCompactionClient,
		CompactionContextTokens: workflowCompactionCtxTokens,
	})

	markersFor := func(skillName string) []string {
		s := skills.Find(discoveredSkills, skillName)
		if s == nil {
			return nil
		}
		return s.CompletionMarkers
	}

	startedAt := time.Now()
	opts := workflow.RunOptions{
		Arguments: arguments,
		OnStageStart: func(stageID string, attempt int) {
			fmt.Fprintf(os.Stderr, "[workflow:%s] → stage %s (attempt %d)\n", w.Name, stageID, attempt)
		},
		OnStageDone: func(stageID string, attempt int, output, marker, nextAction string) {
			markerStr := marker
			if markerStr == "" {
				markerStr = "(no marker)"
			}
			fmt.Fprintf(os.Stderr, "[workflow:%s] ← stage %s done: marker=%s, action=%s, %dB out\n",
				w.Name, stageID, markerStr, nextAction, len(output))
		},
	}

	res, runErr := workflow.Run(cmd.Context(), w, inv, markersFor, opts)
	finalizeUsage(usageTracker, cfg)
	elapsed := time.Since(startedAt)

	fmt.Fprintf(os.Stderr, "\n[workflow:%s] finished in %s (%d stage invocation(s))\n",
		w.Name, elapsed.Round(time.Millisecond), len(res.StagesExecuted))

	if runErr != nil {
		if res.FailureReason != "" {
			fmt.Fprintf(os.Stderr, "[workflow:%s] FAILED: %s\n", w.Name, res.FailureReason)
		}
		return runErr
	}

	// Print the final stage's output to stdout so the user sees the actual result.
	if res.FinalStage != "" {
		fmt.Println(res.Outputs[res.FinalStage])
	}
	return nil
}
