package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/store"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/spf13/cobra"
)

var demoApply bool

var demoCmd = &cobra.Command{
	Use:   "demo [name]",
	Short: "Run a demo scenario",
	Long:  "Creates a temporary project and demonstrates Orchestra capabilities",
	Args:  cobra.ExactArgs(1),
	RunE:  runDemo,
}

func init() {
	rootCmd.AddCommand(demoCmd)
	demoCmd.Flags().BoolVar(&demoApply, "apply", false, "Actually apply changes (default is dry-run)")
}

func runDemo(cmd *cobra.Command, args []string) error {
	demoName := args[0]
	if demoName != "tiny-go" {
		return fmt.Errorf("unknown demo: %s (supported: tiny-go)", demoName)
	}

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "orchestra-demo-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	fmt.Printf("Demo project created at: %s\n", tmpDir)

	// Create .orchestra directory
	orchestraDir := filepath.Join(tmpDir, ".orchestra")
	if err := os.MkdirAll(orchestraDir, 0755); err != nil {
		return fmt.Errorf("failed to create .orchestra dir: %w", err)
	}

	// Create .orchestra.yml
	cfg := config.DefaultConfig(tmpDir)
	cfg.LLM.APIBase = "http://localhost:8000/v1"
	cfg.LLM.Model = "demo-model"
	cfg.ContextLimit = 50
	cfg.Limits.ContextKB = 50
	configPath := filepath.Join(tmpDir, ".orchestra.yml")
	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Create initial project files
	goMod := `module demo

go 1.22
`
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goMod), 0644); err != nil {
		return fmt.Errorf("failed to create go.mod: %w", err)
	}

	mainBeforeGo := `package main

import "fmt"

func main() {
	fmt.Println("Hello, Orchestra!")
}
`
	mainAfterGo := `package main

import (
	"fmt"

	"demo/internal/pkg/math"
)

func main() {
	fmt.Println("Hello, Orchestra!")
	fmt.Printf("2 * 3 = %d\n", math.Multiply(2, 3))
	fmt.Printf("5 - 2 = %d\n", Subtract(5, 2))
}
`
	mainPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(mainBeforeGo), 0644); err != nil {
		return fmt.Errorf("failed to create main.go: %w", err)
	}

	utilsBeforeGo := `package main

// Add adds two integers
func Add(a, b int) int {
	return a + b
}
`
	utilsAfterGo := `package main

// Add adds two integers
func Add(a, b int) int {
	return a + b
}

// Subtract subtracts b from a
func Subtract(a, b int) int {
	return a - b
}
`
	utilsPath := filepath.Join(tmpDir, "utils.go")
	if err := os.WriteFile(utilsPath, []byte(utilsBeforeGo), 0644); err != nil {
		return fmt.Errorf("failed to create utils.go: %w", err)
	}

	mainBefore, err := os.ReadFile(mainPath)
	if err != nil {
		return fmt.Errorf("failed to read main.go: %w", err)
	}
	utilsBefore, err := os.ReadFile(utilsPath)
	if err != nil {
		return fmt.Errorf("failed to read utils.go: %w", err)
	}
	mainHash := store.ComputeSHA256(mainBefore)
	utilsHash := store.ComputeSHA256(utilsBefore)

	anyOps := []ops.AnyOp{
		{
			Op: ops.OpFileMkdirAll,
			MkdirAll: &ops.MkdirAllOp{
				Op:   ops.OpFileMkdirAll,
				Path: "internal/pkg/math",
				Mode: 493, // 0755
			},
		},
		{
			Op: ops.OpFileWriteAtomic,
			WriteAtomic: &ops.WriteAtomicOp{
				Op:      ops.OpFileWriteAtomic,
				Path:    "internal/pkg/math/math.go",
				Content: "package math\n\n// Multiply multiplies two integers\nfunc Multiply(a, b int) int {\n\treturn a * b\n}\n\n// Divide divides a by b (integer division)\nfunc Divide(a, b int) int {\n\tif b == 0 {\n\t\treturn 0\n\t}\n\treturn a / b\n}\n",
				Conditions: ops.WriteAtomicConditions{
					MustNotExist: true,
				},
			},
		},
		{
			Op: ops.OpFileWriteAtomic,
			WriteAtomic: &ops.WriteAtomicOp{
				Op:      ops.OpFileWriteAtomic,
				Path:    "utils.go",
				Content: utilsAfterGo,
				Conditions: ops.WriteAtomicConditions{
					FileHash: utilsHash,
				},
			},
		},
		{
			Op: ops.OpFileWriteAtomic,
			WriteAtomic: &ops.WriteAtomicOp{
				Op:      ops.OpFileWriteAtomic,
				Path:    "main.go",
				Content: mainAfterGo,
				Conditions: ops.WriteAtomicConditions{
					FileHash: mainHash,
				},
			},
		},
	}

	// Apply the plan
	dryRun := !demoApply
	startedAt := time.Now()
	mode := "demo"
	steps := len(anyOps)
	plan := planArtifact{
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
		Query:           "demo: tiny-go",
		GeneratedAtUnix: startedAt.Unix(),
		Ops:             anyOps,
	}
	var applyResp *tools.FSApplyOpsResponse
	var retErr error

	defer func() {
		_ = writeApplyArtifacts(tmpDir, plan, applyResp, dryRun, startedAt, time.Now(), mode, steps, retErr)
	}()

	runner, err := tools.NewRunner(tmpDir, tools.RunnerOptions{
		ExcludeDirs:     cfg.ExcludeDirs,
		ExecTimeout:     time.Duration(cfg.Exec.TimeoutS) * time.Second,
		ExecOutputLimit: cfg.Exec.OutputLimitKB * 1024,
	})
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}
	defer runner.Close()

	resp, err := runner.FSApplyOps(cmd.Context(), tools.FSApplyOpsRequest{
		Ops:    anyOps,
		DryRun: dryRun,
		Backup: !dryRun,
	})
	if err != nil {
		retErr = err
		return fmt.Errorf("failed to apply ops: %w", err)
	}
	applyResp = resp

	// Print results
	if len(resp.ChangedFiles) == 0 {
		fmt.Println("Changed files: (none)")
	} else {
		fmt.Printf("Changed files: %s\n", strings.Join(resp.ChangedFiles, ", "))
	}
	fmt.Printf("Dry-run: %v\n", dryRun)

	// Print diff
	if len(resp.Diffs) > 0 {
		fmt.Println("\n=== Diff ===")
		for _, d := range resp.Diffs {
			fmt.Printf("\n===== %s =====\n", d.Path)
			fmt.Println("--- before")
			fmt.Print(d.Before)
			if !strings.HasSuffix(d.Before, "\n") {
				fmt.Println()
			}
			fmt.Println("--- after")
			fmt.Print(d.After)
			if !strings.HasSuffix(d.After, "\n") {
				fmt.Println()
			}
		}
	}

	fmt.Printf("\nPlan saved to: %s\n", filepath.Join(orchestraDir, "plan.json"))
	fmt.Printf("Diff saved to: %s\n", filepath.Join(orchestraDir, "diff.txt"))
	fmt.Printf("Last result saved to: %s\n", filepath.Join(orchestraDir, "last_result.json"))
	fmt.Printf("Run log saved to: %s\n", filepath.Join(orchestraDir, "last_run.jsonl"))

	return nil
}
