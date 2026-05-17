package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/orchestra/orchestra/internal/packs"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage file-based agent skills",
	Long: `Skills are reusable bundles (system prompt + tools + optional model/provider)
loaded from <project>/.orchestra/skills/*.md with YAML frontmatter.

Run 'orchestra skills list' to enumerate, 'orchestra skills show <name>' to
inspect, and 'orchestra apply --skill <name> "<task>"' to use one.`,
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skills discovered under .orchestra/skills/",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		return RunSkillsList(root, cmd.OutOrStdout())
	},
}

var skillsShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print a skill's metadata and body",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		return RunSkillsShow(root, args[0], cmd.OutOrStdout())
	},
}

var skillsInstallYes bool

var skillsInstallCmd = &cobra.Command{
	Use:   "install <git-url|http-archive|local-path>",
	Short: "Install a third-party skill pack to ~/.orchestra/packs/",
	Long: `Materialise a skill pack (git repo, HTTP(S) archive, or local
directory) under ~/.orchestra/packs/<id>/ and review each contained
skill interactively before it goes live.

SECURITY: a skill body becomes the system prompt of a child agent with
full tool access. Install prompts y/N for every skill, showing the full
body. Use --yes to accept everything non-interactively (dangerous;
prefer to vet by hand on first install).`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillsInstall,
}

var skillsUninstallCmd = &cobra.Command{
	Use:   "uninstall <pack-id>",
	Short: "Remove an installed skill pack",
	Long:  "Delete the named pack from ~/.orchestra/packs/. Use 'skills list' to find the pack-id.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillsUninstall,
}

func init() {
	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsShowCmd)
	skillsInstallCmd.Flags().BoolVar(&skillsInstallYes, "yes", false, "Accept every skill in the pack without prompting (dangerous)")
	skillsCmd.AddCommand(skillsInstallCmd)
	skillsCmd.AddCommand(skillsUninstallCmd)
	rootCmd.AddCommand(skillsCmd)
}

// RunSkillsList scans all skill sources (packs + user + project) and
// prints a table of discovered skills to w. A missing or empty source
// is not an error.
func RunSkillsList(projectRoot string, w io.Writer) error {
	all, err := skills.Discover(projectRoot)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintf(w, "No skills found under %s/%s, ~/%s, or ~/%s/*\n",
			projectRoot, skills.SkillsDir, skills.SkillsDir, skills.PacksDir)
		fmt.Fprintln(w, "Create a .md file with YAML frontmatter (name, description) to add one.")
		fmt.Fprintln(w, "Or install a third-party pack: orchestra skills install <git-url|archive|path>")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tORIGIN\tDESCRIPTION")
	for _, s := range all {
		origin := s.Origin
		if origin == "" {
			origin = "?"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, origin, oneLine(s.Description))
	}
	return tw.Flush()
}

// RunSkillsShow prints the full skill definition for name.
func RunSkillsShow(projectRoot, name string, w io.Writer) error {
	all, err := skills.Discover(projectRoot)
	if err != nil {
		return err
	}
	s := skills.Find(all, name)
	if s == nil {
		return fmt.Errorf("skill %q not found under %s/%s", name, projectRoot, skills.SkillsDir)
	}
	fmt.Fprintf(w, "Name:        %s\n", s.Name)
	fmt.Fprintf(w, "Description: %s\n", s.Description)
	fmt.Fprintf(w, "Source:      %s\n", s.Source)
	if s.Model != "" {
		fmt.Fprintf(w, "Model:       %s\n", s.Model)
	}
	if s.Provider != "" {
		fmt.Fprintf(w, "Provider:    %s\n", s.Provider)
	}
	if len(s.Tools) > 0 {
		fmt.Fprintf(w, "Tools:       %s\n", strings.Join(s.Tools, ", "))
	}
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w, s.Body)
	return nil
}

// packsDir returns the absolute root for installed packs
// (<userHome>/.orchestra/packs), or "" + error when home is unknown.
func packsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home: %w", err)
	}
	return filepath.Join(home, skills.PacksDir), nil
}

func runSkillsInstall(cmd *cobra.Command, args []string) error {
	src, err := packs.ParseSource(args[0])
	if err != nil {
		return err
	}
	root, err := packsDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(root, src.ID())
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Fetching %s (%s) → %s\n", src.Original, src.Kind, dest)
	if _, err := packs.Fetch(cmd.Context(), src, dest, packs.FetchOptions{}); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	return reviewPack(cmd.Context(), w, cmd.InOrStdin(), dest, src.ID(), skillsInstallYes)
}

// reviewPack walks the freshly-fetched pack dir, validates each .md as a
// skill, and prompts the user y/N for each. Rejected skills are deleted.
// If --yes is set, every valid skill is kept (with a warning).
func reviewPack(ctx context.Context, w io.Writer, in io.Reader, packDir, packID string, autoYes bool) error {
	var paths []string
	err := filepath.Walk(packDir, func(p string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && filepath.Ext(info.Name()) == ".md" {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		fmt.Fprintln(w, "Pack contained no .md skill files. Removing.")
		return os.RemoveAll(packDir)
	}
	if autoYes {
		fmt.Fprintf(w, "WARNING: --yes accepts every skill without review (%d found in %s).\n", len(paths), packID)
	}

	reader := bufio.NewReader(in)
	accepted := 0
	for _, p := range paths {
		s, err := skills.Load(p)
		if err != nil {
			fmt.Fprintf(w, "  skip (invalid): %s — %v\n", relUnder(packDir, p), err)
			_ = os.Remove(p)
			continue
		}
		if s.Name == "" {
			fmt.Fprintf(w, "  skip (no name): %s\n", relUnder(packDir, p))
			_ = os.Remove(p)
			continue
		}
		fmt.Fprintln(w, strings.Repeat("─", 60))
		fmt.Fprintf(w, "Skill:       %s\nDescription: %s\nFile:        %s\n",
			s.Name, s.Description, relUnder(packDir, p))
		if len(s.Tools) > 0 {
			fmt.Fprintf(w, "Tools:       %s\n", strings.Join(s.Tools, ", "))
		}
		if s.Model != "" {
			fmt.Fprintf(w, "Model:       %s\n", s.Model)
		}
		fmt.Fprintln(w, "--- body ---")
		fmt.Fprintln(w, strings.TrimRight(s.Body, "\n"))
		fmt.Fprintln(w, "------------")
		if autoYes || prompt(reader, w, "Install this skill? [y/N]: ") {
			accepted++
			continue
		}
		_ = os.Remove(p)
	}
	if accepted == 0 {
		fmt.Fprintln(w, "No skills accepted. Removing pack directory.")
		return os.RemoveAll(packDir)
	}
	fmt.Fprintf(w, "Installed %d skill(s) under pack %s.\n", accepted, packID)
	return nil
}

// prompt reads a line of input and returns true on y/Y/yes.
func prompt(r *bufio.Reader, w io.Writer, msg string) bool {
	fmt.Fprint(w, msg)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func relUnder(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil {
		return rel
	}
	return p
}

func runSkillsUninstall(cmd *cobra.Command, args []string) error {
	root, err := packsDir()
	if err != nil {
		return err
	}
	id := args[0]
	dest := filepath.Join(root, id)
	st, err := os.Stat(dest)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("pack %q not found under %s", id, root)
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed pack %s.\n", id)
	return nil
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
