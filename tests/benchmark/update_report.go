//go:build ignore
// +build ignore

// Utility to update docs/PERFORMANCE_REPORT.md from go test -bench output.
// Usage: go run update_report.go < bench_output.txt
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type benchResult struct {
	name        string
	nsPerOp     int64
	bytesPerOp  int64
	allocsPerOp int64
}

func main() {
	repoRoot := findRepoRoot()
	if repoRoot == "" {
		fmt.Fprintf(os.Stderr, "ERROR: cannot find repo root (looking for go.mod)\n")
		os.Exit(1)
	}

	reportPath := filepath.Join(repoRoot, "docs", "PERFORMANCE_REPORT.md")
	results := parseBenchOutput(os.Stdin)

	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "WARNING: no benchmark results found in input\n")
		os.Exit(1)
	}

	updateReport(reportPath, results)
	fmt.Printf("Updated: %s\n", reportPath)
}

func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func parseBenchOutput(r *os.File) map[string][]benchResult {
	results := make(map[string][]benchResult)
	scanner := bufio.NewScanner(r)

	// Pattern: BenchmarkContext_Direct_Small-24    57   18324847 ns/op  4466734 B/op    3529 allocs/op
	re := regexp.MustCompile(`^BenchmarkContext_(Direct|DaemonInProc|DaemonHTTP)_(Small|Medium|Large)-\d+\s+\d+\s+(\d+)\s+ns/op\s+(\d+)\s+B/op\s+(\d+)\s+allocs/op`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		matches := re.FindStringSubmatch(line)
		if len(matches) != 6 {
			continue
		}

		mode := matches[1]
		size := matches[2]
		nsPerOp, _ := strconv.ParseInt(matches[3], 10, 64)
		bytesPerOp, _ := strconv.ParseInt(matches[4], 10, 64)
		allocsPerOp, _ := strconv.ParseInt(matches[5], 10, 64)

		key := fmt.Sprintf("%s_%s", mode, size)
		results[key] = append(results[key], benchResult{
			name:        fmt.Sprintf("%s_%s", mode, size),
			nsPerOp:     nsPerOp,
			bytesPerOp:  bytesPerOp,
			allocsPerOp: allocsPerOp,
		})
	}

	// Calculate medians for each benchmark
	for key, runs := range results {
		if len(runs) > 0 {
			sort.Slice(runs, func(i, j int) bool {
				return runs[i].nsPerOp < runs[j].nsPerOp
			})
			medianIdx := len(runs) / 2
			results[key] = []benchResult{runs[medianIdx]}
		}
	}

	return results
}

func updateReport(reportPath string, results map[string][]benchResult) {
	content, err := os.ReadFile(reportPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot read %s: %v\n", reportPath, err)
		os.Exit(1)
	}

	contentStr := string(content)

	// Update Small table
	contentStr = updateTable(contentStr, "Small", results, "Small")

	// Update Medium table
	contentStr = updateTable(contentStr, "Medium", results, "Medium")

	// Update Large table (if present)
	contentStr = updateTable(contentStr, "Large", results, "Large")

	// Update raw output section (append new results)
	contentStr = updateRawOutput(contentStr, results)

	// Update generated date
	now := time.Now().Format("2006-01-02")
	contentStr = regexp.MustCompile(`Generated: \d{4}-\d{2}-\d{2}`).ReplaceAllString(contentStr, fmt.Sprintf("Generated: %s", now))

	if err := os.WriteFile(reportPath, []byte(contentStr), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot write %s: %v\n", reportPath, err)
		os.Exit(1)
	}
}

func updateTable(content, size string, results map[string][]benchResult, sizeKey string) string {
	// Find the table for this size
	tableStart := strings.Index(content, fmt.Sprintf("### %s (", size))
	if tableStart == -1 {
		return content // Table not found, skip
	}

	tableEnd := strings.Index(content[tableStart:], "\n## ")
	if tableEnd == -1 {
		tableEnd = len(content) - tableStart
	}
	tableEnd += tableStart

	tableContent := content[tableStart:tableEnd]

	// Extract existing table structure
	lines := strings.Split(tableContent, "\n")
	var newLines []string
	var inTable bool
	var headerFound bool

	for _, line := range lines {
		if strings.Contains(line, "| Mode |") {
			headerFound = true
			newLines = append(newLines, line)
			newLines = append(newLines, "|------|------:|------:|------------------:|-----:|----------:|")
			inTable = true
			continue
		}

		if inTable && strings.HasPrefix(strings.TrimSpace(line), "|") {
			// Skip old data rows
			continue
		}

		if inTable && !strings.HasPrefix(strings.TrimSpace(line), "|") {
			inTable = false
		}

		if !inTable || !headerFound {
			newLines = append(newLines, line)
		}
	}

	// Add new data rows
	if headerFound {
		directKey := fmt.Sprintf("Direct_%s", sizeKey)
		inProcKey := fmt.Sprintf("DaemonInProc_%s", sizeKey)
		httpKey := fmt.Sprintf("DaemonHTTP_%s", sizeKey)

		if direct, ok := results[directKey]; ok && len(direct) > 0 {
			d := direct[0]
			ms := float64(d.nsPerOp) / 1e6
			newLines = append(newLines, fmt.Sprintf("| Direct | %s | %.2f | 1.0x | %s | %s |",
				formatInt(d.nsPerOp), ms, formatInt(d.bytesPerOp), formatInt(d.allocsPerOp)))
		}

		if inProc, ok := results[inProcKey]; ok && len(inProc) > 0 {
			i := inProc[0]
			ms := float64(i.nsPerOp) / 1e6
			direct, hasDirect := results[directKey]
			speedup := "—"
			if hasDirect && len(direct) > 0 && direct[0].nsPerOp > 0 {
				speedup = fmt.Sprintf("**%.1f×**", float64(direct[0].nsPerOp)/float64(i.nsPerOp))
			}
			newLines = append(newLines, fmt.Sprintf("| Daemon InProc | %s | %.2f | %s | %s | %s |",
				formatInt(i.nsPerOp), ms, speedup, formatInt(i.bytesPerOp), formatInt(i.allocsPerOp)))
		}

		if http, ok := results[httpKey]; ok && len(http) > 0 {
			h := http[0]
			ms := float64(h.nsPerOp) / 1e6
			direct, hasDirect := results[directKey]
			speedup := "—"
			if hasDirect && len(direct) > 0 && direct[0].nsPerOp > 0 {
				speedup = fmt.Sprintf("**%.1f×**", float64(direct[0].nsPerOp)/float64(h.nsPerOp))
			}
			newLines = append(newLines, fmt.Sprintf("| Daemon HTTP | %s | %.2f | %s | %s | %s |",
				formatInt(h.nsPerOp), ms, speedup, formatInt(h.bytesPerOp), formatInt(h.allocsPerOp)))
		}
	}

	newTableContent := strings.Join(newLines, "\n")
	return content[:tableStart] + newTableContent + content[tableEnd:]
}

func updateRawOutput(content string, results map[string][]benchResult) string {
	// Find "## Raw output" section
	rawStart := strings.Index(content, "## Raw output")
	if rawStart == -1 {
		return content
	}

	rawEndInSection := strings.Index(content[rawStart:], "\n## ")
	if rawEndInSection == -1 {
		rawEndInSection = len(content) - rawStart
	}
	rawEnd := rawStart + rawEndInSection

	// Find ```text marker
	codeStartIdx := strings.Index(content[rawStart:], "```text")
	if codeStartIdx == -1 {
		// No code block found, just append update note
		now := time.Now().Format("2006-01-02 15:04:05")
		newRaw := content[rawStart:rawEnd] + "\n\n(Last updated: " + now + ")\n"
		return content[:rawStart] + newRaw + content[rawEnd:]
	}
	codeStart := rawStart + codeStartIdx

	// Keep header, replace content
	header := content[rawStart : codeStart+7]

	// Find closing ```
	codeEndIdx := strings.Index(content[codeStart+7:], "\n```")
	if codeEndIdx == -1 {
		// Malformed, skip update
		return content
	}
	footer := content[codeStart+7+codeEndIdx : rawEnd]

	// Generate new raw output (simplified - just note that it's updated)
	now := time.Now().Format("2006-01-02 15:04:05")
	newRaw := header + "\n" +
		fmt.Sprintf("(Updated: %s - run 'go test -run=^$ -bench=BenchmarkContext -benchmem -count=5 ./tests/benchmark' to see full output)\n", now) +
		footer

	return content[:rawStart] + newRaw + content[rawEnd:]
}

func formatInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	if s != "" {
		parts = append([]string{s}, parts...)
	}
	return strings.Join(parts, ",")
}
