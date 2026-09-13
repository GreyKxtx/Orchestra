package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/llm"
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
	evalApply   bool
	evalModel   string
	evalTimeout int
	evalOnly    []string
	evalRepeat  int
)

func init() {
	evalCmd.Flags().BoolVar(&evalApply, "apply", true, "Actually apply changes during eval (default true)")
	evalCmd.Flags().StringVar(&evalModel, "model", "", "Override model from config")
	evalCmd.Flags().IntVar(&evalTimeout, "timeout", 120, "Per-task timeout in seconds")
	evalCmd.Flags().StringSliceVar(&evalOnly, "task", nil, "Run only these tasks by name (repeatable, or comma-separated)")
	evalCmd.Flags().IntVar(&evalRepeat, "repeat", 1, "Run each task this many times; a task that wins some runs and loses others is reported FLAKY")
	rootCmd.AddCommand(evalCmd)
}

// evalStatus names the outcome of running one task `runs` times, of which
// `won` succeeded.
//
// Three outcomes, not two. A task that wins some runs and loses others has
// not passed and has not failed — it has told you the suite cannot answer the
// question yet. Folding that into either verdict is how a run reports a score
// it has not established: this very suite scored a task FAIL and then PASS on
// consecutive runs with nothing changed in between, and reported both with
// the same confidence.
func evalStatus(won, runs int) string {
	switch {
	case runs <= 0:
		return "FAIL"
	case won == runs:
		return "PASS"
	case won == 0:
		return "FAIL"
	default:
		return "FLAKY"
	}
}

// selectTasks narrows the set to the names asked for.
//
// A name that matches nothing is an error rather than an empty run: the whole
// point of --task is to iterate on one task, and a typo that silently runs
// zero of them reads exactly like a suite that passed.
func selectTasks(tasks []evalharness.Task, only []string) ([]evalharness.Task, error) {
	if len(only) == 0 {
		return tasks, nil
	}
	want := make(map[string]bool, len(only))
	for _, name := range only {
		if n := strings.TrimSpace(name); n != "" {
			want[n] = true
		}
	}
	var picked []evalharness.Task
	for _, task := range tasks {
		if want[task.Name] {
			picked = append(picked, task)
			delete(want, task.Name)
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for name := range want {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		available := make([]string, 0, len(tasks))
		for _, task := range tasks {
			available = append(available, task.Name)
		}
		return nil, fmt.Errorf("no such task: %s\navailable: %s",
			strings.Join(missing, ", "), strings.Join(available, ", "))
	}
	return picked, nil
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
	tasks, err = selectTasks(tasks, evalOnly)
	if err != nil {
		return err
	}
	if evalRepeat < 1 {
		return fmt.Errorf("--repeat must be at least 1, got %d", evalRepeat)
	}

	fmt.Fprintf(os.Stderr, "Running %d eval task(s) from %s", len(tasks), tasksDir)
	if evalRepeat > 1 {
		fmt.Fprintf(os.Stderr, ", %d run(s) each", evalRepeat)
	}
	fmt.Fprint(os.Stderr, "\n\n")

	// Build RunAgent using Core.
	runAgent := func(ctx context.Context, run evalharness.AgentRun) (evalharness.AgentOutcome, error) {
		workspaceRoot := run.WorkspaceRoot
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
				return evalharness.AgentOutcome{}, fmt.Errorf("load config: %w", cfgErr)
			}
			srcCfg.ProjectRoot = workspaceRoot
			if evalModel != "" {
				srcCfg.LLM.Model = evalModel
			}
			if e := config.Save(cfgPath, srcCfg); e != nil {
				return evalharness.AgentOutcome{}, fmt.Errorf("write eval config: %w", e)
			}
		}

		c, err := core.New(workspaceRoot, core.Options{LLMClient: getTestLLMClient()})
		if err != nil {
			return evalharness.AgentOutcome{}, fmt.Errorf("core: %w", err)
		}
		defer c.Close()

		// The answer is streamed, not returned: neither agent.Result nor
		// AgentRunResult carries the final prose. Every other consumer — the
		// editor panel, the web adapter — builds it by accumulating
		// message_delta, so the harness does exactly the same rather than
		// widening the core's result shape for the sake of a test.
		var answer strings.Builder
		onEvent := func(method string, params any) {
			if method != "agent/event" {
				return
			}
			m, ok := params.(map[string]any)
			if !ok {
				return
			}
			if kind, _ := m["type"].(string); kind != string(llm.StreamEventMessageDelta) {
				return
			}
			if chunk, _ := m["content"].(string); chunk != "" {
				answer.WriteString(chunk)
			}
		}

		res, err := c.AgentRun(ctx, core.AgentRunParams{
			Query:    run.Query,
			Apply:    run.Apply,
			MaxSteps: run.MaxSteps,
			Mode:     run.Mode,
			OnEvent:  onEvent,
		})
		if err != nil {
			return evalharness.AgentOutcome{}, err
		}
		return evalharness.AgentOutcome{Steps: res.Steps, Answer: answer.String(), PlanPath: res.PlanPath}, nil
	}

	runner := &evalharness.Runner{RunAgent: runAgent}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	header := "TASK\tSTATUS\tSTEPS\tRETRIES\tRESOLVE\tDURATION\tDETAILS"
	if evalRepeat > 1 {
		header = "TASK\tSTATUS\tRUNS\tSTEPS\tRETRIES\tRESOLVE\tDURATION\tDETAILS"
	}
	fmt.Fprintln(tw, header)

	clean, broken, flaky := 0, 0, 0
	var totalSteps, totalRetries, totalResolve, runs int
	for i, task := range tasks {
		var (
			won      int
			details  string
			steps    int
			retries  int
			resolve  int
			duration time.Duration
		)
		for r := 0; r < evalRepeat; r++ {
			// Progress, because the table is buffered until the end so its
			// columns line up — which on a long suite means an hour with
			// nothing on screen at all.
			//
			// Each of these is written as a COMPLETE line. The agent and the
			// LSP write to this same stream while a task runs, so a half-line
			// waiting for its verdict gets another process's output spliced
			// into the middle of it.
			label := fmt.Sprintf("[%d/%d] %s", i+1, len(tasks), task.Name)
			if evalRepeat > 1 {
				label += fmt.Sprintf(" (run %d/%d)", r+1, evalRepeat)
			}
			fmt.Fprintf(os.Stderr, "%s: started\n", label)

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(evalTimeout)*time.Second)
			result := runner.RunTask(ctx, task)
			cancel()

			ok := result.Passed && result.Error == nil
			if ok {
				won++
			} else if details == "" {
				// Keep the FIRST failure seen. On a flaky task that is the one
				// worth reading; a later clean run must not erase it.
				switch {
				case result.Error != nil:
					details = result.Error.Error()
				case len(result.Failures) > 0:
					details = strings.Join(result.Failures, "; ")
				}
			}
			steps += result.Steps
			retries += result.InvalidRetries
			resolve += result.ResolveFailed
			duration += result.Duration
			runs++

			verdict := "FAIL"
			if ok {
				verdict = "PASS"
			}
			fmt.Fprintf(os.Stderr, "%s: %s in %s\n", label, verdict, result.Duration.Round(time.Second))
		}

		status := evalStatus(won, evalRepeat)
		switch status {
		case "PASS":
			clean++
		case "FLAKY":
			flaky++
		default:
			broken++
		}

		totalSteps += steps
		totalRetries += retries
		totalResolve += resolve

		if evalRepeat > 1 {
			fmt.Fprintf(tw, "%s\t%s\t%d/%d\t%d\t%d\t%d\t%s\t%s\n",
				task.Name, status, won, evalRepeat,
				steps/evalRepeat, retries, resolve,
				(duration / time.Duration(evalRepeat)).Round(time.Millisecond), details)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			task.Name, status, steps, retries, resolve,
			duration.Round(time.Millisecond), details)
	}
	tw.Flush()

	n := len(tasks)
	avgSteps := float64(totalSteps) / float64(runs)
	avgRetries := float64(totalRetries) / float64(runs)
	avgResolve := float64(totalResolve) / float64(runs)
	fmt.Fprintf(os.Stderr, "\n%d passed, %d failed", clean, broken)
	if flaky > 0 {
		fmt.Fprintf(os.Stderr, ", %d flaky", flaky)
	}
	fmt.Fprintf(os.Stderr, " (%d task(s)", n)
	if evalRepeat > 1 {
		fmt.Fprintf(os.Stderr, " × %d runs", evalRepeat)
	}
	fmt.Fprintln(os.Stderr, ")")
	fmt.Fprintf(os.Stderr, "avg steps: %.1f  avg invalid retries: %.1f  avg resolve_failed: %.1f  (Phase 1 target retries: <3.0)\n",
		avgSteps, avgRetries, avgResolve)
	if broken+flaky > 0 {
		return fmt.Errorf("%d task(s) failed, %d flaky", broken, flaky)
	}
	return nil
}
