package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

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

func init() {
	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsShowCmd)
	rootCmd.AddCommand(skillsCmd)
}

// RunSkillsList scans <projectRoot>/.orchestra/skills/ and prints a
// table of discovered skills to w. A missing or empty directory prints
// a hint and returns nil.
func RunSkillsList(projectRoot string, w io.Writer) error {
	all, err := skills.Discover(projectRoot)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintf(w, "No skills found under %s/%s\n", projectRoot, skills.SkillsDir)
		fmt.Fprintln(w, "Create a .md file with YAML frontmatter (name, description) to add one.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDESCRIPTION")
	for _, s := range all {
		fmt.Fprintf(tw, "%s\t%s\n", s.Name, oneLine(s.Description))
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

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
