//go:build bench_llm
// +build bench_llm

// Manual benchmark that includes LLM calls (not used in CI).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	projectRoot := "D:\\CursorProjects\\Orchestra"
	testDir := filepath.Join(projectRoot, "testdata", "small")
	query := "add logging to main.go"
	iterations := 11

	fmt.Println("=== Orchestra Performance Benchmark ===")
	fmt.Printf("Project: %s\n", testDir)
	fmt.Printf("Query: %s\n", query)
	fmt.Printf("Iterations: %d (1 warmup + 10 measurements)\n\n", iterations)

	orchestraExe := filepath.Join(projectRoot, "orchestra.exe")
	if _, err := os.Stat(orchestraExe); os.IsNotExist(err) {
		fmt.Printf("ERROR: %s not found. Run 'go build -o orchestra.exe ./cmd/orchestra' first\n", orchestraExe)
		os.Exit(1)
	}

	os.Chdir(testDir)

	fmt.Println("=== APPLY (--plan-only) ===")
	results := []float64{}
	for i := 1; i <= iterations; i++ {
		fmt.Printf("  Run %d/%d... ", i, iterations)
		start := time.Now()
		cmd := exec.Command(orchestraExe, "apply", "--plan-only", "--debug", query)
		cmd.Dir = testDir
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			fmt.Printf("ERROR: %v\n", err)
			continue
		}
		elapsed := time.Since(start).Milliseconds()
		if i > 1 {
			results = append(results, float64(elapsed))
			fmt.Printf("%.2f ms\n", float64(elapsed))
		} else {
			fmt.Printf("%.2f ms (warmup)\n", float64(elapsed))
		}
	}

	st := calcStats(results)

	fmt.Println("\n=== RESULTS ===")
	fmt.Printf("  Median:  %.2f ms\n", st.median)
	fmt.Printf("  Average: %.2f ms\n", st.avg)
	fmt.Printf("  Min:     %.2f ms\n", st.min)
	fmt.Printf("  Max:     %.2f ms\n", st.max)
	fmt.Printf("  P90:     %.2f ms\n", st.p90)

	report := generateReport(query, results, st)
	reportPath := filepath.Join(projectRoot, "docs", "PERFORMANCE_REPORT.md")
	if err := os.WriteFile(reportPath, []byte(report), 0644); err != nil {
		fmt.Printf("\n[ERROR] Failed to write report: %v\n", err)
	} else {
		fmt.Printf("\nReport saved to: %s\n", reportPath)
	}
}

type stats struct {
	median, avg, min, max, p90 float64
}

func calcStats(results []float64) stats {
	if len(results) == 0 {
		return stats{}
	}
	sorted := make([]float64, len(results))
	copy(sorted, results)
	sort.Float64s(sorted)

	s := stats{
		median: sorted[len(sorted)/2],
		min:    sorted[0],
		max:    sorted[len(sorted)-1],
		p90:    sorted[int(float64(len(sorted))*0.9)],
	}

	sum := 0.0
	for _, v := range results {
		sum += v
	}
	s.avg = sum / float64(len(results))

	return s
}

func generateReport(query string, results []float64, st stats) string {
	raw := strings.Join(strings.Fields(fmt.Sprint(results)), ", ")

	return fmt.Sprintf(`# Orchestra Performance Report

Generated: %s

## Test Configuration

- Project: `+"`testdata/small`"+`
- Query: `+"`%s`"+`
- Iterations: 10 (after 1 warmup)
- Mode: apply --plan-only (vNext; legacy HTTP daemon removed)

## Results

| Metric | Value (ms) |
|--------|------------|
| Median | %.2f |
| Average | %.2f |
| Min | %.2f |
| Max | %.2f |
| P90 | %.2f |

## Raw Data (ms)

%s
`, time.Now().Format("2006-01-02 15:04:05"), query,
		st.median, st.avg, st.min, st.max, st.p90,
		raw)
}
