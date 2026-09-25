package importrules

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A mode with no code of its own is its roles.Spec and its prompt file, and
// nothing else. scout and product are two such modes: their names appear in
// no other non-test Go file, so their tool lists, write scopes, the task
// tool's subagent_type enum, the <available_agents> cards, the default agency
// flows, the phase gate and config's reserved names all come from the Spec.
// Adding a mode used to mean editing 6–7 files that had to agree.
func TestASpecOnlyModeLivesInTwoFiles(t *testing.T) {
	root := repoRoot(t)
	for _, mode := range []string{"scout", "product"} {
		literal := `"` + mode + `"`
		var found []string
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", "vendor", "testdata", "dist", "out":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), literal) {
				rel, _ := filepath.Rel(root, path)
				found = append(found, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[0] != "internal/roles/roles.go" {
			t.Errorf("mode %s is named in %v; it should need only internal/roles/roles.go and its prompt file", mode, found)
		}
		if _, err := os.Stat(filepath.Join(root, "internal", "prompt", "files", mode+".txt")); err != nil {
			t.Errorf("mode %s has no prompt file: %v", mode, err)
		}
	}
}

// Agents are assembled in one place. agent.Options were built by hand in
// eleven — the CLI, the core's launch, three skill and stage launchers, the
// task runner and its verifier, three pipeline stages — and each read the
// config its own way until they disagreed (ARCH-1). internal/app is the
// composition root; nothing else writes an agent.Options literal.
func TestAgentOptionsAreBuiltOnlyInApp(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata", "dist", "out":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/app/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "agent.Options{") {
			t.Errorf("%s builds an agent.Options literal; use app.TurnOptions or app.ChildOptions", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The composition root sits below everything that launches an agent: core,
// the CLI, the task runner, skills, workflow stages and the pipeline all call
// it, so it may import none of them.
func TestAppStaysBelowItsCallers(t *testing.T) {
	root := repoRoot(t)
	for _, dep := range listDeps(t, root, "./internal/app") {
		for _, caller := range []string{"core", "cli", "tasks", "skillrun", "stageinvoke", "pipeline", "workflow"} {
			if dep == modulePath+"/internal/"+caller {
				t.Errorf("internal/app links %s, one of its callers", dep)
			}
		}
	}
}

// Callers ask the client stack for a capability — llm.LoggerOf,
// llm.ContextTokensOf, llm.DiscoverLimits — never for the concrete
// OpenAI-compatible client (ARCH-8). Reaching for the concrete type made each
// of those a no-op on every other provider: with provider anthropic the
// request log stayed empty on every surface.
func TestNoCallerReachesForTheConcreteLLMClient(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata", "dist", "out":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "llm/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, bad := range []string{"llm.AsOpenAIClient(", "*llm.OpenAIClient)", "*llm.AnthropicClient)"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s uses %s; ask the stack (llm.LoggerOf, llm.ContextTokensOf, llm.DiscoverLimits)", rel, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
