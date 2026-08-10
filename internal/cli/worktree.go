package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/orchestra/orchestra/internal/git"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage orchestra git worktrees under .orchestra/worktrees/",
	Long:  "Creates isolated linked worktrees for parallel agent runs. Registry: .orchestra/worktrees.json",
}

var (
	worktreeBranch  string
	worktreeRef     string
	worktreeForce   bool
)

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List git worktrees (marks orchestra-managed entries)",
	Args:  cobra.NoArgs,
	RunE:  runWorktreeList,
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a managed worktree at .orchestra/worktrees/<name>",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorktreeAdd,
}

var worktreeRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a managed worktree",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorktreeRemove,
}

var worktreePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Prune stale git worktrees and registry entries",
	Args:  cobra.NoArgs,
	RunE:  runWorktreePrune,
}

var worktreePathCmd = &cobra.Command{
	Use:   "path <name>",
	Short: "Print absolute path of a managed worktree",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorktreePath,
}

func init() {
	worktreeAddCmd.Flags().StringVar(&worktreeBranch, "branch", "", "Branch name (default orchestra/<name>)")
	worktreeAddCmd.Flags().StringVar(&worktreeRef, "ref", "HEAD", "Base ref for new branch")
	worktreeAddCmd.Flags().BoolVar(&worktreeForce, "force", false, "Pass --force to git worktree add")
	worktreeRemoveCmd.Flags().BoolVar(&worktreeForce, "force", false, "Force remove dirty worktree")
	worktreeCmd.AddCommand(worktreeListCmd, worktreeAddCmd, worktreeRemoveCmd, worktreePruneCmd, worktreePathCmd)
	rootCmd.AddCommand(worktreeCmd)
}

func runWorktreeList(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	if !git.IsRepo(cfg.ProjectRoot) {
		return fmt.Errorf("not a git repository: %s", cfg.ProjectRoot)
	}
	entries, err := git.ListWorktrees(cfg.ProjectRoot)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tMAIN\tMANAGED\tBRANCH\tPATH")
	for _, e := range entries {
		name := e.Name
		if name == "" {
			name = "-"
		}
		branch := e.Branch
		if branch == "" && e.Detached {
			branch = "(detached)"
		}
		fmt.Fprintf(tw, "%s\t%v\t%v\t%s\t%s\n", name, e.Main, e.Managed, branch, e.Path)
	}
	return tw.Flush()
}

func runWorktreeAdd(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	entry, err := git.AddWorktree(cfg.ProjectRoot, args[0], worktreeBranch, worktreeRef, worktreeForce)
	if err != nil {
		return err
	}
	fmt.Printf("Created worktree %q at %s (branch %s)\n", entry.Name, entry.Path, strings.TrimPrefix(entry.Branch, "refs/heads/"))
	return nil
}

func runWorktreeRemove(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	if err := git.RemoveWorktree(cfg.ProjectRoot, args[0], worktreeForce); err != nil {
		return err
	}
	fmt.Printf("Removed worktree %q\n", args[0])
	return nil
}

func runWorktreePrune(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	n, err := git.PruneWorktrees(cfg.ProjectRoot)
	if err != nil {
		return err
	}
	fmt.Printf("Pruned git worktrees; removed %d stale registry entries\n", n)
	return nil
}

func runWorktreePath(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	path, err := git.ResolveManagedWorktree(cfg.ProjectRoot, args[0])
	if err != nil {
		return err
	}
	fmt.Println(filepath.Clean(path))
	return nil
}
