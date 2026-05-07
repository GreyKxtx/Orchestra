package instrument

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Result describes what was done for one language.
type Result struct {
	Lang          string
	Skipped       bool   // already instrumented
	SkipReason    string
	TelemetryFile string // relative path written
	Patched       bool   // entry point patched
	PatchedFile   string // relative path of patched file
	InstallOutput string // stdout+stderr of package install
}

// Instrument instruments the project at dir for each detected language.
// dryRun=true: prints what would be done without writing/installing.
func Instrument(dir string, langs []LangConfig, dryRun bool) ([]Result, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	var results []Result
	for _, lc := range langs {
		r, err := instrumentOne(absDir, lc, dryRun)
		if err != nil {
			return results, fmt.Errorf("instrument %s: %w", lc.Name, err)
		}
		results = append(results, r)
	}
	return results, nil
}

func instrumentOne(dir string, lc LangConfig, dryRun bool) (Result, error) {
	r := Result{Lang: lc.Name}

	// Idempotency: if the marker already appears anywhere in the project, skip.
	if lc.AlreadyInstrumentedMarker != "" {
		found, err := containsMarker(dir, lc.Extensions, lc.AlreadyInstrumentedMarker)
		if err != nil {
			return r, err
		}
		if found {
			r.Skipped = true
			r.SkipReason = "already instrumented (" + lc.AlreadyInstrumentedMarker + " found)"
			return r, nil
		}
	}

	// Resolve template placeholders.
	module := detectModule(dir, lc)
	service := filepath.Base(dir)

	telContent := lc.TelemetryTemplate
	telContent = strings.ReplaceAll(telContent, "{{MODULE}}", module)
	telContent = strings.ReplaceAll(telContent, "{{SERVICE}}", service)

	// Write telemetry file.
	telPath := filepath.Join(dir, lc.TelemetryFile)
	r.TelemetryFile = lc.TelemetryFile
	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(telPath), 0o755); err != nil {
			return r, fmt.Errorf("mkdir %s: %w", filepath.Dir(telPath), err)
		}
		if err := atomicWrite(telPath, []byte(telContent)); err != nil {
			return r, fmt.Errorf("write telemetry file: %w", err)
		}
	}

	// Run package install.
	if lc.InstallCmd != "" && !dryRun {
		out, err := runInstall(dir, lc.InstallCmd, lc.InstallArgs)
		r.InstallOutput = out
		if err != nil {
			return r, fmt.Errorf("install packages: %w", err)
		}
	}

	// Patch entry point (if configured).
	if lc.MainPatch.InsertAfter != "" {
		entry, err := findEntryPoint(dir, lc.MainPatch.EntryGlobs)
		if err != nil {
			// no entry point found — not fatal, just skip patching
		} else {
			initCall := strings.ReplaceAll(lc.MainPatch.InitCall, "{{SERVICE}}", service)
			importLine := strings.ReplaceAll(lc.MainPatch.ImportLine, "{{MODULE}}", module)

			if !dryRun {
				if err := patchEntryPoint(entry, lc.MainPatch.InsertAfter, initCall, importLine); err != nil {
					return r, fmt.Errorf("patch entry point %s: %w", entry, err)
				}
			}
			rel, _ := filepath.Rel(dir, entry)
			r.Patched = true
			r.PatchedFile = rel
		}
	}

	return r, nil
}

// containsMarker walks files with the given extensions looking for marker string.
func containsMarker(dir string, extensions []string, marker string) (bool, error) {
	extSet := make(map[string]bool, len(extensions))
	for _, e := range extensions {
		extSet[e] = true
	}

	found := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !extSet[filepath.Ext(path)] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // best-effort
		}
		if strings.Contains(string(data), marker) {
			found = true
		}
		return nil
	})
	return found, err
}

// detectModule extracts module path for Go (from go.mod); other langs return dir base.
func detectModule(dir string, lc LangConfig) string {
	if lc.Name == "go" {
		modFile := filepath.Join(dir, "go.mod")
		f, err := os.Open(modFile)
		if err != nil {
			return filepath.Base(dir)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "module ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "module "))
			}
		}
	}
	return filepath.Base(dir)
}

// findEntryPoint finds the first file matching any of the globs.
// Patterns support one level of wildcard via filepath.Glob; for patterns like
// "cmd/*/main.go" we also walk the tree and check suffix matching.
func findEntryPoint(dir string, globs []string) (string, error) {
	for _, pattern := range globs {
		// Try direct filepath.Glob first (works for simple patterns).
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		if len(matches) > 0 {
			return matches[0], nil
		}
		// Walk and match by suffix for patterns containing *.
		if strings.Contains(pattern, "*") {
			result, err := walkGlob(dir, pattern)
			if err == nil {
				return result, nil
			}
		}
	}
	return "", fmt.Errorf("no entry point found")
}

// walkGlob walks dir and returns the first file whose relative path matches pattern.
func walkGlob(dir, pattern string) (string, error) {
	var found string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ok, _ := filepath.Match(pattern, rel)
		if ok {
			found = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("no match for %s", pattern)
	}
	return found, nil
}

// patchEntryPoint inserts initCall after insertAfter in the file, and injects importLine.
func patchEntryPoint(path, insertAfter, initCall, importLine string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)

	// Backup original.
	if err := os.WriteFile(path+".orchestra.bak", data, 0o644); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}

	// Insert init call after the marker line.
	idx := strings.Index(content, insertAfter)
	if idx == -1 {
		return fmt.Errorf("marker %q not found in %s", insertAfter, path)
	}
	insertAt := idx + len(insertAfter)
	content = content[:insertAt] + initCall + content[insertAt:]

	// Inject import line (Go-specific).
	if importLine != "" {
		content = injectGoImport(content, importLine)
	}

	return atomicWrite(path, []byte(content))
}

// injectGoImport adds importLine into the first import block found.
func injectGoImport(content, importLine string) string {
	// Find existing import block "import (\n...\n)"
	start := strings.Index(content, "import (")
	if start == -1 {
		// Single-line import or no import: insert before "func main"
		mainIdx := strings.Index(content, "\nfunc ")
		if mainIdx == -1 {
			return content
		}
		imp := fmt.Sprintf("\nimport %q\n", importLine)
		return content[:mainIdx] + imp + content[mainIdx:]
	}
	// Insert before the closing ")"
	closing := strings.Index(content[start:], "\n)")
	if closing == -1 {
		return content
	}
	insertAt := start + closing
	return content[:insertAt] + "\n\t\"" + importLine + "\"" + content[insertAt:]
}

func runInstall(dir, cmd string, args []string) (string, error) {
	c := exec.Command(cmd, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	return string(out), err
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".orchestra.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
