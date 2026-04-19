package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	evalharness "github.com/orchestra/orchestra/tests/eval"
	"github.com/spf13/cobra"
)

var evalCmd = &cobra.Command{
	Use:   "eval [tasks-dir]",
	Short: "Run eval tasks against the configured LLM",
	Long:  "Loads YAML task definitions from tasks-dir (default: tests/eval/tasks), runs each against the agent, and reports pass/fail.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runEval,
}

var (
	evalApply    bool
	evalModel    string
	evalTimeout  int
)

func init() {
	evalCmd.Flags().BoolVar(&evalApply, "apply", true, "Actually apply changes during eval (default true)")
	evalCmd.Flags().StringVar(&evalModel, "model", "", "Override model from config")
	evalCmd.Flags().IntVar(&evalTimeout, "timeout", 120, "Per-task timeout in seconds")
	rootCmd.AddCommand(evalCmd)
}

func runEval(cmd *cobra.Command, args []string) error {
	tasksDir := "tests/eval/tasks"
	if len(args) > 0 {
		tasksDir = args[0]
	}
	tasksDir, _ = filepath.Abs(tasksDir)

	tasks, err := evalharness.LoadTasks(tasksDir)
	if err != nil {
		return fmt.Errorf("load tasks: %w", err)
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no tasks found in %s", tasksDir)
	}

	fmt.Fprintf(os.Stderr, "Running %d eval task(s) from %s\n\n", len(tasks), tasksDir)

	// Build RunAgent using Core.
	runAgent := func(ctx context.Context, workspaceRoot, query string, maxSteps int, apply bool) (int, error) {
		cfgPath := filepath.Join(workspaceRoot, ".orchestra.yml")

		// Write a minimal config if none exists (eval uses project dir for config)
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			// Load config from CWD to get LLM settings, then write to eval workspace
			cwdCfgPath := ".orchestra.yml"
			if cwd, e := os.Getwd(); e == nil {
				cwdCfgPath = filepath.Join(cwd, ".orchestra.yml")
			}
			srcCfg, cfgErr := config.Load(cwdCfgPath)
			if cfgErr != nil {
				return 0, fmt.Errorf("load config: %w", cfgErr)
			}
			srcCfg.ProjectRoot = workspaceRoot
			if evalModel != "" {
				srcCfg.LLM.Model = evalModel
			}
			if e := config.Save(cfgPath, srcCfg); e != nil {
				return 0, fmt.Errorf("write eval config: %w", e)
			}
		}

		c, err := core.New(workspaceRoot, core.Options{LLMClient: testLLMClient})
		if err != nil {
			return 0, fmt.Errorf("core: %w", err)
		}
		defer c.Close()

		res, err := c.AgentRun(ctx, core.AgentRunParams{
			Query:    query,
			Apply:    apply,
			MaxSteps: maxSteps,
		})
		if err != nil {
			return 0, err
		}
		return res.Steps, nil
	}

	runner := &evalharness.Runner{RunAgent: runAgent}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TASK\tSTATUS\tSTEPS\tDURATION\tDETAILS")

	passed, failed := 0, 0
	for _, task := range tasks {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(evalTimeout)*time.Second)
		result := runner.RunTask(ctx, task)
		cancel()

		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		if result.Error != nil {
			status = "ERROR"
		}

		details := ""
		if result.Error != nil {
			details = result.Error.Error()
		} else if len(result.Failures) > 0 {
			details = strings.Join(result.Failures, "; ")
		}

		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
			task.Name, status, result.Steps,
			result.Duration.Round(time.Millisecond),
			details)

		if result.Passed && result.Error == nil {
			passed++
		} else {
			failed++
		}
	}
	tw.Flush()

	fmt.Fprintf(os.Stderr, "\n%d passed, %d failed (total: %d)\n", passed, failed, len(tasks))
	if failed > 0 {
		return fmt.Errorf("%d task(s) failed", failed)
	}
	return nil
}
