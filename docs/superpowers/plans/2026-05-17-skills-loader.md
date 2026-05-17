# Skills Loader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add file-based, discoverable "skills" — reusable bundles of (system prompt + allowed tools + optional model/provider) loaded from `.orchestra/skills/*.md`, invokable via `orchestra apply --skill <name>` and listable via `orchestra skills list|show`.

**Architecture:** A `skills` package parses Markdown files with YAML frontmatter (`name`, `description`, `tools`, `model`, `provider`) into a `Skill` struct. Loader scans `<project_root>/.orchestra/skills/*.md`. The existing `AgentOptions` machinery in `internal/cli/apply.go` is the integration point: `--skill <name>` resolves a `Skill`, validates its tools against `validAgentToolNames`, and merges into `AgentOptions` (SystemPrompt, ToolFilter, Model, Provider) — reusing the path already proven for custom agents (`agents:` in `.orchestra.yml`). Skills are a *superset* of inline custom agents: same merge semantics, just loaded from disk and shareable across projects.

**Tech Stack:** Go stdlib (`os`, `path/filepath`, `strings`, `bufio`), `gopkg.in/yaml.v3` (already a dep), existing `internal/config` validators, existing `cmd/orchestra` CLI scaffolding (`spf13/cobra` if used — verify in Task 0; else stdlib `flag`).

---

## File Map

| File | Change |
|---|---|
| `internal/skills/skill.go` | **Create**: `Skill` struct, frontmatter constants |
| `internal/skills/loader.go` | **Create**: `Load(path)`, `Discover(projectRoot)` |
| `internal/skills/loader_test.go` | **Create**: unit tests for parser + discovery |
| `internal/cli/skills.go` | **Create**: `orchestra skills list|show` commands |
| `internal/cli/skills_test.go` | **Create**: CLI smoke tests |
| `internal/cli/apply.go` | **Modify**: add `--skill` flag, resolve skill → merge into `AgentOptions` |
| `internal/cli/apply_test.go` | **Modify**: add `--skill` flag test |
| `cmd/orchestra/main.go` | **Modify**: wire `skills` subcommand |
| `docs/PROTOCOL.md` | **Modify**: note skills are CLI-only (don't bump versions) |
| `docs/CHANGELOG.md` | **Modify**: add entry |
| `internal/prompt/files/build.txt` or equivalent | **No change** (skill prompt overrides) |

---

## Task 0: Orient — confirm CLI framework + custom-agent merge path

**Files:** read-only

- [ ] **Step 1: Confirm CLI framework**

Run: `rg -n "cobra|spf13" cmd/orchestra/ internal/cli/ | head`
Expected: either matches (Cobra) or no matches (stdlib `flag`). Note which.

- [ ] **Step 2: Read the exact apply.go branch that handles `--agent <name>` from `.orchestra.yml`**

Run: `rg -n "FindAgent|resolveCustomAgent|AgentDefinition" internal/cli/apply.go`
Note the line numbers of: (a) where `--agent` flag is parsed, (b) where `cfg.FindAgent(name)` is called, (c) where its `SystemPrompt/Tools/Model/Provider` are merged into `AgentOptions`.

- [ ] **Step 3: Read `AgentOptions` struct definition**

Run: `rg -n "type AgentOptions" internal/agent/`
Read the struct so later tasks reference real field names.

- [ ] **Step 4: Read `validAgentToolNames` (already in `internal/config/config.go`)**

Confirm the exported function or symbol used to validate tool names. If unexported, decide in Task 3 whether to export it or duplicate the small map.

No commit — this is orientation only.

---

## Task 1: `Skill` type + frontmatter parser

**Files:**
- Create: `internal/skills/skill.go`
- Create: `internal/skills/loader.go` (parser part only)
- Create: `internal/skills/loader_test.go`

- [ ] **Step 1: Write failing test for happy-path parse**

`internal/skills/loader_test.go`:

```go
package skills

import (
	"strings"
	"testing"
)

func TestParse_HappyPath(t *testing.T) {
	src := `---
name: refactor-go
description: Refactor Go code with conservative edits.
tools: [read, edit, write, grep, symbols]
model: qwen3.6-27b
---
You are a careful Go refactoring assistant. Use small, focused edits.
$ARGUMENTS
`
	s, err := Parse("refactor-go.md", strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Name != "refactor-go" {
		t.Errorf("name: got %q want refactor-go", s.Name)
	}
	if s.Description == "" {
		t.Error("description empty")
	}
	if len(s.Tools) != 5 || s.Tools[0] != "read" {
		t.Errorf("tools: got %v", s.Tools)
	}
	if s.Model != "qwen3.6-27b" {
		t.Errorf("model: got %q", s.Model)
	}
	if !strings.Contains(s.Body, "$ARGUMENTS") {
		t.Errorf("body missing args marker: %q", s.Body)
	}
}
```

- [ ] **Step 2: Run test — expect compile failure (no `Parse`, no `Skill`)**

Run: `go test ./internal/skills/ -run TestParse_HappyPath`
Expected: build error, package or symbol not found.

- [ ] **Step 3: Implement `Skill` struct and `Parse`**

`internal/skills/skill.go`:

```go
// Package skills loads reusable agent skill bundles from
// .orchestra/skills/*.md. A skill is a Markdown file with a YAML
// frontmatter header (name, description, tools, model, provider)
// followed by a Markdown body used as the agent system prompt.
package skills

// Skill is a parsed skill definition. Source is the absolute file path
// the skill was loaded from (empty when constructed in tests).
type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools,omitempty"`
	Model       string   `yaml:"model,omitempty"`
	Provider    string   `yaml:"provider,omitempty"`

	Body   string `yaml:"-"`
	Source string `yaml:"-"`
}
```

`internal/skills/loader.go`:

```go
package skills

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

const frontmatterDelim = "---"

// Parse reads a single skill from r. The source argument is recorded on
// the returned Skill (for diagnostics) and is not otherwise validated.
func Parse(source string, r io.Reader) (*Skill, error) {
	br := bufio.NewReader(r)
	first, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read frontmatter open: %w", err)
	}
	if strings.TrimRight(first, "\r\n") != frontmatterDelim {
		return nil, fmt.Errorf("skill %s: missing %q frontmatter open on line 1", source, frontmatterDelim)
	}

	var fmBuf strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read frontmatter: %w", err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == frontmatterDelim {
			break
		}
		fmBuf.WriteString(line)
		if err == io.EOF {
			return nil, fmt.Errorf("skill %s: unterminated frontmatter", source)
		}
	}

	var s Skill
	if err := yaml.Unmarshal([]byte(fmBuf.String()), &s); err != nil {
		return nil, fmt.Errorf("skill %s: parse frontmatter: %w", source, err)
	}

	bodyBytes, err := io.ReadAll(br)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	s.Body = strings.TrimLeft(string(bodyBytes), "\r\n")
	s.Source = source
	return &s, nil
}
```

- [ ] **Step 4: Run the test — expect PASS**

Run: `go test ./internal/skills/ -run TestParse_HappyPath -v`
Expected: PASS.

- [ ] **Step 5: Add failing tests for validation errors**

Append to `loader_test.go`:

```go
func TestParse_MissingFrontmatter(t *testing.T) {
	_, err := Parse("x.md", strings.NewReader("just body\n"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "frontmatter open") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_UnterminatedFrontmatter(t *testing.T) {
	_, err := Parse("x.md", strings.NewReader("---\nname: x\n"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse("x.md", strings.NewReader("---\nname: [bad\n---\nbody"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

- [ ] **Step 6: Run all skills tests — expect PASS**

Run: `go test ./internal/skills/ -v`
Expected: all 4 tests PASS. If unterminated case prematurely returns EOF with the wrong error message, fix the EOF handling in `Parse` (the order matters: append the line first, then check EOF).

- [ ] **Step 7: Commit**

```bash
git add internal/skills/skill.go internal/skills/loader.go internal/skills/loader_test.go
git commit -m "feat(skills): add Skill struct and frontmatter parser"
```

---

## Task 2: Discovery + validation

**Files:**
- Modify: `internal/skills/loader.go`
- Modify: `internal/skills/loader_test.go`

- [ ] **Step 1: Write failing discovery test**

Append to `loader_test.go`:

```go
import (
	"os"
	"path/filepath"
)

func TestDiscover_ScansSkillsDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("a.md", "---\nname: a\ndescription: A\n---\nbody A\n")
	writeSkill("b.md", "---\nname: b\ndescription: B\n---\nbody B\n")
	writeSkill("not-a-skill.txt", "ignored")

	skills, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2: %+v", len(skills), skills)
	}
	if skills[0].Name != "a" || skills[1].Name != "b" {
		t.Errorf("sorted by name failed: %q %q", skills[0].Name, skills[1].Name)
	}
}

func TestDiscover_MissingDirReturnsEmpty(t *testing.T) {
	root := t.TempDir() // no .orchestra/skills inside
	skills, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected empty, got %d", len(skills))
	}
}

func TestDiscover_DuplicateNameIsError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: dup\ndescription: x\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: dup\ndescription: y\n---\n"), 0o644)
	_, err := Discover(root)
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscover_MissingNameIsError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "x.md"), []byte("---\ndescription: x\n---\n"), 0o644)
	_, err := Discover(root)
	if err == nil {
		t.Fatal("expected missing-name error")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL (no `Discover`)**

Run: `go test ./internal/skills/ -run TestDiscover -v`
Expected: build/symbol error.

- [ ] **Step 3: Implement `Discover` and `Load`**

Append to `internal/skills/loader.go`:

```go
import (
	"os"
	"path/filepath"
	"sort"
)

// SkillsDir is the per-project directory scanned by Discover, relative
// to the project root.
const SkillsDir = ".orchestra/skills"

// Load reads a single skill from path.
func Load(path string) (*Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(path, f)
}

// Discover scans <projectRoot>/.orchestra/skills/*.md and returns all
// parsed skills sorted by Name. A missing directory is not an error
// (returns nil, nil). Files with extensions other than .md are ignored.
// Returns an error if any skill has a missing Name, has invalid YAML,
// or if two skills share the same Name.
func Discover(projectRoot string) ([]*Skill, error) {
	dir := filepath.Join(projectRoot, SkillsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills dir %s: %w", dir, err)
	}

	var skills []*Skill
	seen := make(map[string]string) // name → source path
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		s, err := Load(p)
		if err != nil {
			return nil, err
		}
		if s.Name == "" {
			return nil, fmt.Errorf("skill %s: name is required", p)
		}
		if prev, ok := seen[s.Name]; ok {
			return nil, fmt.Errorf("duplicate skill name %q in %s and %s", s.Name, prev, p)
		}
		seen[s.Name] = p
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// Find returns the skill with the given name from skills, or nil.
func Find(skills []*Skill, name string) *Skill {
	for _, s := range skills {
		if s.Name == name {
			return s
		}
	}
	return nil
}
```

Note: merge the new `import` block with the existing one in `loader.go` (single grouped `import (...)`).

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/skills/ -v`
Expected: all tests PASS.

- [ ] **Step 5: Add tool-name validation**

The plan validates tool names at the *consumer* layer (apply.go in Task 4), not in the loader — same pattern as `AgentDefinition`. Skip in this task. (This bullet exists so the reviewer doesn't ask "where's the tool validation?")

- [ ] **Step 6: Commit**

```bash
git add internal/skills/loader.go internal/skills/loader_test.go
git commit -m "feat(skills): add Discover and Load with duplicate/name validation"
```

---

## Task 3: `orchestra skills list` and `orchestra skills show` CLI commands

**Files:**
- Create: `internal/cli/skills.go`
- Create: `internal/cli/skills_test.go`
- Modify: `cmd/orchestra/main.go` (wire subcommand)

- [ ] **Step 1: Confirm CLI dispatch shape**

Run: `rg -n "func .*Cmd|RegisterCommand|os.Args\[1\]" cmd/orchestra/main.go internal/cli/ | head -30`
Find how existing subcommands (e.g. `apply`, `mcp`, `init`) are registered. Mirror that exact pattern in this task — DO NOT introduce a new dispatch mechanism. If the project uses Cobra, create a `*cobra.Command`; if stdlib `flag`, create a `func RunSkills(args []string) error` and switch on `args[0]`.

- [ ] **Step 2: Write failing test for `skills list` output**

`internal/cli/skills_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsList_PrintsDiscoveredSkills(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "refactor.md"), []byte(
		"---\nname: refactor\ndescription: Refactor code.\n---\nbody\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "review.md"), []byte(
		"---\nname: review\ndescription: Code review pass.\n---\nbody\n"), 0o644)

	var out bytes.Buffer
	if err := RunSkillsList(root, &out); err != nil {
		t.Fatalf("RunSkillsList: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "refactor") || !strings.Contains(s, "Refactor code.") {
		t.Errorf("missing refactor row in output:\n%s", s)
	}
	if !strings.Contains(s, "review") || !strings.Contains(s, "Code review pass.") {
		t.Errorf("missing review row in output:\n%s", s)
	}
}

func TestSkillsList_EmptyDirIsNotError(t *testing.T) {
	var out bytes.Buffer
	if err := RunSkillsList(t.TempDir(), &out); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestSkillsShow_PrintsBody(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "x.md"), []byte(
		"---\nname: x\ndescription: D\ntools: [read, edit]\n---\nHello body.\n"), 0o644)

	var out bytes.Buffer
	if err := RunSkillsShow(root, "x", &out); err != nil {
		t.Fatalf("RunSkillsShow: %v", err)
	}
	s := out.String()
	for _, want := range []string{"x", "D", "read", "edit", "Hello body."} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestSkillsShow_UnknownIsError(t *testing.T) {
	err := RunSkillsShow(t.TempDir(), "nope", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected unknown-skill error, got %v", err)
	}
}
```

- [ ] **Step 3: Run tests — expect FAIL**

Run: `go test ./internal/cli/ -run TestSkills -v`
Expected: build error (no `RunSkillsList` / `RunSkillsShow`).

- [ ] **Step 4: Implement `RunSkillsList` and `RunSkillsShow`**

`internal/cli/skills.go`:

```go
package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/orchestra/orchestra/internal/skills"
)

// RunSkillsList scans <projectRoot>/.orchestra/skills/ and prints a
// table of discovered skills to w. A missing directory prints a hint
// and returns nil.
func RunSkillsList(projectRoot string, w io.Writer) error {
	all, err := skills.Discover(projectRoot)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintf(w, "No skills found under %s/%s\n", projectRoot, skills.SkillsDir)
		fmt.Fprintf(w, "Create a .md file with YAML frontmatter (name, description) to add one.\n")
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
```

- [ ] **Step 5: Run tests — expect PASS**

Run: `go test ./internal/cli/ -run TestSkills -v`
Expected: PASS.

- [ ] **Step 6: Wire the subcommand into `cmd/orchestra/main.go`**

Read first: `head -60 cmd/orchestra/main.go` (or wherever subcommands are dispatched — found in Step 1).

Add the `skills` subcommand mirroring the `mcp` subcommand exactly. Skeleton (adapt to actual framework):

```go
// Inside the dispatch switch (or alongside other Cobra cmds):
case "skills":
    if len(os.Args) < 3 {
        fmt.Fprintln(os.Stderr, "usage: orchestra skills <list|show> [args]")
        os.Exit(2)
    }
    root, _ := os.Getwd()
    switch os.Args[2] {
    case "list":
        if err := cli.RunSkillsList(root, os.Stdout); err != nil {
            fmt.Fprintln(os.Stderr, err)
            os.Exit(1)
        }
    case "show":
        if len(os.Args) < 4 {
            fmt.Fprintln(os.Stderr, "usage: orchestra skills show <name>")
            os.Exit(2)
        }
        if err := cli.RunSkillsShow(root, os.Args[3], os.Stdout); err != nil {
            fmt.Fprintln(os.Stderr, err)
            os.Exit(1)
        }
    default:
        fmt.Fprintf(os.Stderr, "unknown skills subcommand: %s\n", os.Args[2])
        os.Exit(2)
    }
```

If the project uses Cobra, instead create a `var skillsCmd = &cobra.Command{Use: "skills", ...}` with two children (`list`, `show`) and add via `rootCmd.AddCommand(skillsCmd)`.

- [ ] **Step 7: Build and smoke-check**

Run: `go build -o orchestra ./cmd/orchestra`
Then: `./orchestra skills list` in the repo root.
Expected: "No skills found under …/.orchestra/skills" (since none exist yet).

- [ ] **Step 8: Commit**

```bash
git add internal/cli/skills.go internal/cli/skills_test.go cmd/orchestra/main.go
git commit -m "feat(cli): add 'orchestra skills list|show' commands"
```

---

## Task 4: `orchestra apply --skill <name>` integration

**Files:**
- Modify: `internal/cli/apply.go`
- Modify: `internal/cli/apply_test.go`
- Read-only reference: `internal/config/config.go` (for `validAgentToolNames`)

This task layers `--skill` on top of the *exact same* merge machinery that already handles `--agent` (custom inline agents). Re-read that section before editing.

- [ ] **Step 1: Read the existing `--agent` resolution branch in `apply.go`**

Run: `rg -n "agentName|FindAgent|resolveCustomAgent|SystemPrompt|AgentOptions" internal/cli/apply.go`
Identify: flag declaration, the function that resolves a custom agent name to options, and the merge call site. Copy the exact function signature you'll mirror for `resolveSkill`.

- [ ] **Step 2: Export `validAgentToolNames` lookup**

The skill loader runs at CLI time, not inside `internal/config`, so we need a public way to validate tool names without an import cycle.

Pick whichever exists / is least disruptive:
- If `internal/config` already exports a `ValidAgentTool(name string) bool` helper, use it.
- Otherwise, add it in `internal/config/config.go`:

```go
// ValidAgentTool reports whether name is a valid short tool name
// usable in AgentDefinition.Tools or in a skill's tools: list.
func ValidAgentTool(name string) bool {
    return validAgentToolNames[name]
}
```

And add a tiny test in `internal/config/config_test.go`:

```go
func TestValidAgentTool(t *testing.T) {
    if !ValidAgentTool("read") {
        t.Error("read should be valid")
    }
    if ValidAgentTool("nonexistent") {
        t.Error("nonexistent should be invalid")
    }
}
```

Run: `go test ./internal/config/ -run TestValidAgentTool -v`
Expected: PASS.

- [ ] **Step 3: Write failing apply-flag test**

In `internal/cli/apply_test.go`, add (adapt to the existing testing style — if there's an `applyTestHarness`, reuse it; otherwise call the public entry as below):

```go
func TestApply_SkillFlag_LoadsAndMerges(t *testing.T) {
    root := t.TempDir()
    dir := filepath.Join(root, ".orchestra", "skills")
    if err := os.MkdirAll(dir, 0o755); err != nil {
        t.Fatal(err)
    }
    os.WriteFile(filepath.Join(dir, "minimal.md"), []byte(
        "---\nname: minimal\ndescription: D\ntools: [read]\n---\nYou are minimal.\n"), 0o644)

    opts, err := resolveSkillOpts(root, "minimal")
    if err != nil {
        t.Fatalf("resolveSkillOpts: %v", err)
    }
    if opts.SystemPrompt == "" || !strings.Contains(opts.SystemPrompt, "minimal") {
        t.Errorf("SystemPrompt: %q", opts.SystemPrompt)
    }
    if len(opts.Tools) != 1 || opts.Tools[0] != "read" {
        t.Errorf("Tools: %v", opts.Tools)
    }
}

func TestApply_SkillFlag_UnknownSkill(t *testing.T) {
    _, err := resolveSkillOpts(t.TempDir(), "nope")
    if err == nil {
        t.Fatal("expected error")
    }
}

func TestApply_SkillFlag_InvalidTool(t *testing.T) {
    root := t.TempDir()
    dir := filepath.Join(root, ".orchestra", "skills")
    os.MkdirAll(dir, 0o755)
    os.WriteFile(filepath.Join(dir, "bad.md"), []byte(
        "---\nname: bad\ndescription: D\ntools: [definitely-not-a-real-tool]\n---\n"), 0o644)
    _, err := resolveSkillOpts(root, "bad")
    if err == nil || !strings.Contains(err.Error(), "definitely-not-a-real-tool") {
        t.Fatalf("expected invalid-tool error, got %v", err)
    }
}
```

The struct returned by `resolveSkillOpts` should mirror whatever the existing `resolveCustomAgentOpts` (or equivalent) returns. If the existing one returns `*agent.AgentOptions` directly, do the same here.

- [ ] **Step 4: Run tests — expect FAIL**

Run: `go test ./internal/cli/ -run TestApply_SkillFlag -v`
Expected: build error.

- [ ] **Step 5: Implement `resolveSkillOpts` in `apply.go`**

Add (near `resolveCustomAgentOpts` — keep them adjacent so future readers see the parallel):

```go
import "github.com/orchestra/orchestra/internal/skills"

// resolveSkillOpts loads the named skill from <projectRoot>/.orchestra/skills/
// and converts it into AgentOptions. Tool names are validated against
// config.ValidAgentTool. Returns an error if the skill is missing, has a
// bad tool, or fails to parse.
func resolveSkillOpts(projectRoot, name string) (*agent.AgentOptions, error) {
    all, err := skills.Discover(projectRoot)
    if err != nil {
        return nil, err
    }
    s := skills.Find(all, name)
    if s == nil {
        return nil, fmt.Errorf("skill %q not found under %s/%s", name, projectRoot, skills.SkillsDir)
    }
    for _, t := range s.Tools {
        if !config.ValidAgentTool(t) {
            return nil, fmt.Errorf("skill %q: invalid tool name %q", name, t)
        }
    }
    return &agent.AgentOptions{
        SystemPrompt: s.Body,
        Tools:        s.Tools,
        Model:        s.Model,
        Provider:     s.Provider,
    }, nil
}
```

Adapt the returned struct's field names to match the existing `AgentOptions` exactly (verified in Task 0 Step 3). If the existing custom-agent merger does merging in-place into an already-built `AgentOptions` rather than returning one, follow that pattern instead.

- [ ] **Step 6: Wire the `--skill` flag into the apply command**

Find the flag block in `apply.go` (search for `--agent` flag definition). Add right next to it:

```go
var flagSkill string
// ... in flag setup:
fs.StringVar(&flagSkill, "skill", "", "Run with the named skill from .orchestra/skills/")
```

In the place where `--agent` is currently resolved into `AgentOptions`, add a parallel branch. Skill and agent are mutually exclusive — fail fast if both are set:

```go
if flagSkill != "" && flagAgent != "" {
    return fmt.Errorf("--skill and --agent are mutually exclusive")
}
if flagSkill != "" {
    skillOpts, err := resolveSkillOpts(projectRoot, flagSkill)
    if err != nil {
        return err
    }
    // Merge skillOpts into the in-flight AgentOptions exactly the way
    // the --agent path does (non-empty Skill fields override defaults).
    // Reuse the existing merge helper if one exists.
    mergeAgentOpts(&agentOpts, skillOpts)
}
```

If no `mergeAgentOpts` helper exists, inline the merge — copy the structure used for `--agent`. Match field-by-field; do not introduce new behavior.

- [ ] **Step 7: Run unit tests**

Run: `go test ./internal/cli/ -run TestApply_SkillFlag -v`
Expected: all three new tests PASS.

- [ ] **Step 8: Full package smoke**

Run: `go vet ./... && go test ./internal/skills/ ./internal/cli/ ./internal/config/`
Expected: vet clean, tests pass.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/apply.go internal/cli/apply_test.go internal/config/config.go internal/config/config_test.go
git commit -m "feat(cli): add --skill flag to apply; load from .orchestra/skills/"
```

---

## Task 5: Docs + changelog + memory update

**Files:**
- Modify: `docs/CHANGELOG.md`
- Modify: `docs/PROTOCOL.md` (one-line note)
- Create: `docs/skills.md` (short user-facing doc)

- [ ] **Step 1: Append CHANGELOG entry**

At the top of `docs/CHANGELOG.md`, add:

```markdown
## 2026-05-17 — Skills loader

- `internal/skills/` — file-based skills loaded from `<project>/.orchestra/skills/*.md` with YAML frontmatter (`name`, `description`, `tools`, `model`, `provider`) and Markdown body as system prompt.
- `orchestra skills list` — list discovered skills.
- `orchestra skills show <name>` — print full skill (metadata + body).
- `orchestra apply --skill <name> "<task>"` — run apply with the skill's system prompt + tool filter + model/provider overrides. Mutually exclusive with `--agent`.
- Tool names in `tools:` are validated against the same allow-list used by inline `agents:` (`config.ValidAgentTool`).
- No protocol/tools version bump: skills are a CLI-only loader on top of existing `AgentOptions`; LLM tool surface is unchanged.
```

- [ ] **Step 2: Add a one-line note to `docs/PROTOCOL.md`**

Find the section describing custom agents (search for "agents:" or "AgentDefinition"). Add after it:

```markdown
> **Skills:** the CLI also accepts `--skill <name>`, which loads a file-based agent definition from `.orchestra/skills/<name>.md`. Skills do not change the JSON-RPC surface — they're a CLI-side loader that resolves to the same `AgentOptions` used by inline `agents:`.
```

- [ ] **Step 3: Create `docs/skills.md`**

```markdown
# Skills

A *skill* is a reusable bundle of (system prompt + allowed tools + optional model/provider) stored as a single Markdown file with YAML frontmatter. Skills are the file-based, shareable form of the inline `agents:` block in `.orchestra.yml`.

## Location

`<project_root>/.orchestra/skills/<name>.md`

A skill file:

```markdown
---
name: refactor-go
description: Refactor Go code with conservative edits.
tools: [read, edit, write, grep, symbols]
model: qwen3.6-27b
provider: lmstudio   # optional; references providers: in .orchestra.yml
---
You are a careful Go refactoring assistant.
Make small, focused edits. Run tests after each change.
```

## Commands

| Command | Effect |
|---|---|
| `orchestra skills list` | List skills found under `.orchestra/skills/`. |
| `orchestra skills show <name>` | Print metadata + system prompt body. |
| `orchestra apply --skill <name> "<task>"` | Run apply with this skill's prompt + tool filter + model/provider overrides. |

## Rules

- `name` and `description` are required.
- `tools` is optional; when omitted the skill inherits the full build toolset. When set, every name must be in the same allow-list as inline `agents:`.
- `model` is optional; overrides the model on the selected provider.
- `provider` is optional; must reference a key in the top-level `providers:` map in `.orchestra.yml`.
- `--skill` and `--agent` are mutually exclusive.
```

- [ ] **Step 4: Commit**

```bash
git add docs/CHANGELOG.md docs/PROTOCOL.md docs/skills.md
git commit -m "docs(skills): add changelog, protocol note, and skills.md"
```

---

## Task 6: End-to-end smoke

**Files:** none (manual / scripted check)

- [ ] **Step 1: Create a real test skill in the repo**

```bash
mkdir -p .orchestra/skills
cat > .orchestra/skills/echo.md <<'EOF'
---
name: echo
description: Echo skill for smoke testing.
tools: [read, ls]
---
You are a read-only echo agent. Use only `read` and `ls`.
EOF
```

- [ ] **Step 2: List skills**

Run: `./orchestra skills list`
Expected: a table containing `echo  Echo skill for smoke testing.`

- [ ] **Step 3: Show the skill**

Run: `./orchestra skills show echo`
Expected: metadata block + body printed; `Tools: read, ls`.

- [ ] **Step 4: Apply with the skill (dry-run, no LLM)**

Run: `./orchestra apply --skill echo --plan-only "list files"`
Expected: command exits without error; the run uses the skill prompt (verify via `.orchestra/last_run.jsonl` if it includes the system prompt). If the underlying flow needs an LLM and you don't have one running, accept that the LLM call may fail — but the *flag wiring* (skill resolution, tool filter) must succeed before any LLM call.

- [ ] **Step 5: Clean up the smoke file**

```bash
rm .orchestra/skills/echo.md
rmdir .orchestra/skills 2>/dev/null || true
```

(Do NOT commit the smoke skill — it's for manual verification only.)

- [ ] **Step 6: Final repo-wide checks**

Run:
```bash
go vet ./...
go test ./...
go test -race ./internal/skills/ ./internal/cli/
```
Expected: all green. Pre-existing `internal/ckg.TestOTLPServer_…` and `internal/tasks.TestSpawn_MaxStepsClampedTo12` races are known and may flake — note them if seen, do not "fix" them in this plan.

- [ ] **Step 7: Update MEMORY index**

Append one line to `C:\Users\andre\.claude\projects\D--CursorProjects-Orchestra\memory\MEMORY.md` (Skills loader p.6 closed). The memory file `project_status.md` should also be updated to flip "Skill logic" from "запланирован" to "✅ готов" with the date. This is the only change to MEMORY in this plan.

---

## Self-Review Notes

- **Spec coverage:** "Close item 6 — Skill logic." Tasks 1–4 deliver loader + CLI + apply integration. Tasks 5–6 deliver docs and verification. ✅
- **Out of scope (documented as follow-up, NOT in this plan):** LLM-invokable `skill_invoke` tool (model picks a skill mid-run); user-global `~/.orchestra/skills/`; `$ARGUMENTS` substitution in the skill body; plugin-style skill packs from external repos. These can be added once the file-based MVP is in place.
- **Type consistency check:** `AgentOptions` field names (`SystemPrompt`, `Tools`, `Model`, `Provider`) are *assumed* in Task 4 Step 5 — Task 0 Step 3 mandates verifying them before writing code. If the real struct uses different names, fix at write-time; don't fork from this plan.
- **No version bump:** ToolsVersion / ProtocolVersion stay where they are. The JSON-RPC surface and LLM tool list are unchanged.
