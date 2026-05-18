// Package workflow orchestrates multi-stage agent pipelines defined as YAML
// files in .orchestra/workflows/. Each stage invokes a named skill, captures
// its final output, and feeds it forward into downstream stages via
// {stage.output} substitution. Loops on completion markers (e.g. plan
// verifier re-routing back to planner on `## ISSUES FOUND`) are first-class.
//
// Workflows generalise internal/pipeline (which hard-codes Investigator →
// Coder → Critic) to arbitrary user-defined graphs without writing Go code.
package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Workflow is one parsed yaml definition.
type Workflow struct {
	Name        string  `yaml:"name"`
	Description string  `yaml:"description,omitempty"`
	Stages      []Stage `yaml:"stages"`

	// Source is the absolute file path the workflow was loaded from.
	Source string `yaml:"-"`
}

// Stage is one node in the workflow DAG.
type Stage struct {
	// ID is the unique stage identifier referenced by other stages via
	// depends_on and inputs substitution. Must be unique within a workflow.
	ID string `yaml:"id"`

	// Skill is the skill name to invoke (must resolve via skills.Find).
	Skill string `yaml:"skill"`

	// DependsOn lists stage IDs that must complete before this one starts.
	// Used for topological ordering and the {ID.output} substitution scope.
	DependsOn []string `yaml:"depends_on,omitempty"`

	// Inputs are templated strings appended to the user query handed to the
	// skill. Supports two substitutions:
	//   - $ARGUMENTS  → the original CLI query
	//   - {ID.output} → previous stage's final output text
	Inputs []string `yaml:"inputs,omitempty"`

	// Parallel, when > 1, runs this stage N times concurrently with the same
	// inputs. Outputs are joined with separator lines for downstream stages.
	Parallel int `yaml:"parallel,omitempty"`

	// LoopUntilMarker, when non-empty, re-runs the stage (or the stage
	// specified by OnMarker mapping) until the marker appears in the output
	// or MaxAttempts is exhausted.
	LoopUntilMarker string `yaml:"loop_until_marker,omitempty"`

	// OnMarker maps a marker → action when LoopUntilMarker is set but the
	// stage emitted a different marker. Actions:
	//   "redo:<stage_id>"  → re-run that earlier stage with updated inputs
	//   "fail"             → abort the whole workflow with that marker as reason
	// When unset and a non-loop_until marker is seen, defaults to "fail".
	OnMarker map[string]string `yaml:"on_marker,omitempty"`

	// MaxAttempts caps loop iterations. Default 3 when LoopUntilMarker set.
	MaxAttempts int `yaml:"max_attempts,omitempty"`
}

// Load reads a yaml workflow from disk and validates its shape. The returned
// workflow's Source is set to the absolute path.
func Load(path string) (*Workflow, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("workflow: abs %s: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("workflow: read %s: %w", abs, err)
	}
	var w Workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("workflow: parse %s: %w", abs, err)
	}
	w.Source = abs
	if w.Name == "" {
		// Fall back to filename without extension so workflows that omit
		// the field still get a stable id for logging.
		w.Name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	}
	if err := Validate(&w); err != nil {
		return nil, err
	}
	return &w, nil
}

// Validate checks structural invariants: unique stage ids, valid depends_on
// references, no cycles, on_marker actions reference existing stages.
// Returns an error describing the first problem found.
func Validate(w *Workflow) error {
	if w.Name == "" {
		return fmt.Errorf("workflow: name is empty")
	}
	if len(w.Stages) == 0 {
		return fmt.Errorf("workflow %q: no stages", w.Name)
	}
	seen := make(map[string]bool, len(w.Stages))
	for i, s := range w.Stages {
		if s.ID == "" {
			return fmt.Errorf("workflow %q: stage[%d] has empty id", w.Name, i)
		}
		if seen[s.ID] {
			return fmt.Errorf("workflow %q: duplicate stage id %q", w.Name, s.ID)
		}
		seen[s.ID] = true
		if s.Skill == "" {
			return fmt.Errorf("workflow %q: stage %q has empty skill", w.Name, s.ID)
		}
		if s.Parallel < 0 {
			return fmt.Errorf("workflow %q: stage %q: parallel must be >= 0", w.Name, s.ID)
		}
		if s.MaxAttempts < 0 {
			return fmt.Errorf("workflow %q: stage %q: max_attempts must be >= 0", w.Name, s.ID)
		}
	}
	// depends_on references must exist.
	for _, s := range w.Stages {
		for _, dep := range s.DependsOn {
			if !seen[dep] {
				return fmt.Errorf("workflow %q: stage %q depends_on unknown stage %q", w.Name, s.ID, dep)
			}
			if dep == s.ID {
				return fmt.Errorf("workflow %q: stage %q depends_on itself", w.Name, s.ID)
			}
		}
		// on_marker "redo:<id>" must reference an existing stage.
		for marker, action := range s.OnMarker {
			if strings.HasPrefix(action, "redo:") {
				target := strings.TrimPrefix(action, "redo:")
				if !seen[target] {
					return fmt.Errorf("workflow %q: stage %q on_marker[%q]=redo:%s references unknown stage", w.Name, s.ID, marker, target)
				}
			} else if action != "fail" {
				return fmt.Errorf("workflow %q: stage %q on_marker[%q]: unknown action %q (expected redo:<id> or fail)", w.Name, s.ID, marker, action)
			}
		}
	}
	// Cycle check via topological sort.
	if _, err := TopoSort(w); err != nil {
		return err
	}
	return nil
}

// TopoSort returns stage IDs in dependency order. Errors out on cycles.
func TopoSort(w *Workflow) ([]string, error) {
	indeg := make(map[string]int, len(w.Stages))
	successors := make(map[string][]string, len(w.Stages))
	byID := make(map[string]*Stage, len(w.Stages))
	for i := range w.Stages {
		s := &w.Stages[i]
		byID[s.ID] = s
		indeg[s.ID] = len(s.DependsOn)
		for _, dep := range s.DependsOn {
			successors[dep] = append(successors[dep], s.ID)
		}
	}
	// Kahn's algorithm. Process ids in deterministic order so that
	// multi-root workflows produce stable output.
	var ready []string
	for id, d := range indeg {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	out := make([]string, 0, len(w.Stages))
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		out = append(out, next)
		// Append newly-ready successors in sorted order.
		var newly []string
		for _, succ := range successors[next] {
			indeg[succ]--
			if indeg[succ] == 0 {
				newly = append(newly, succ)
			}
		}
		sort.Strings(newly)
		ready = append(ready, newly...)
	}
	if len(out) != len(w.Stages) {
		return nil, fmt.Errorf("workflow %q: cycle detected in stage dependencies", w.Name)
	}
	return out, nil
}

// Discover walks the standard workflow locations (project then user) and
// returns the merged list. Project workflows override user workflows with
// the same name. Returns an empty slice when no directories exist.
func Discover(projectRoot string) ([]*Workflow, error) {
	dirs := []string{filepath.Join(projectRoot, ".orchestra", "workflows")}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".orchestra", "workflows"))
	}

	byName := make(map[string]*Workflow)
	order := []string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			w, err := Load(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if _, exists := byName[w.Name]; !exists {
				order = append(order, w.Name)
			}
			// First write wins (project before user) — matches skills precedence.
			if _, exists := byName[w.Name]; !exists {
				byName[w.Name] = w
			}
		}
	}
	out := make([]*Workflow, 0, len(byName))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out, nil
}

// Find returns the workflow with the given name from the list, or nil.
func Find(workflows []*Workflow, name string) *Workflow {
	for _, w := range workflows {
		if w.Name == name {
			return w
		}
	}
	return nil
}
