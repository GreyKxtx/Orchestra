//go:build bench_llm
// +build bench_llm

// Manual benchmark that includes LLM calls (not used in CI).
package main

import (
	"fmt"
	"net/http"
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

	// Check daemon
	fmt.Println("[INFO] Checking daemon status...")
	daemonRunning := checkDaemon()
	if !daemonRunning {
		fmt.Println("[INFO] Daemon is not running. Starting daemon in background...")
		cmd := exec.Command(orchestraExe, "daemon", "--project-root", testDir)
		cmd.Dir = projectRoot
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			fmt.Printf("[ERROR] Failed to start daemon: %v\n", err)
			fmt.Println("[INFO] Please start daemon manually in a separate terminal:")
			fmt.Printf("  cd %s\n", projectRoot)
			fmt.Printf("  .\\orchestra.exe daemon --project-root \"%s\"\n", testDir)
			fmt.Println("\nPress Enter when daemon is running...")
			fmt.Scanln()
		} else {
			// Wait for daemon to start
			fmt.Print("[INFO] Waiting for daemon to start")
			for i := 0; i < 10; i++ {
				time.Sleep(1 * time.Second)
				fmt.Print(".")
				if checkDaemon() {
					fmt.Println(" OK")
					daemonRunning = true
					break
				}
			}
			if !daemonRunning {
				fmt.Println(" FAILED")
				fmt.Println("[ERROR] Daemon failed to start. Please start it manually.")
				os.Exit(1)
			}
		}
	}
	if daemonRunning {
		fmt.Println("[INFO] Daemon is running")
	}

	os.Chdir(testDir)

	// DAEMON MODE
	fmt.Println("=== DAEMON MODE ===")
	daemonResults := []float64{}
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
			daemonResults = append(daemonResults, float64(elapsed))
			fmt.Printf("%.2f ms\n", float64(elapsed))
		} else {
			fmt.Printf("%.2f ms (warmup)\n", float64(elapsed))
		}
	}

	// DIRECT MODE
	fmt.Println("\n=== DIRECT MODE (--no-daemon) ===")
	directResults := []float64{}
	for i := 1; i <= iterations; i++ {
		fmt.Printf("  Run %d/%d... ", i, iterations)
		start := time.Now()
		cmd := exec.Command(orchestraExe, "apply", "--plan-only", "--no-daemon", "--debug", query)
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
			directResults = append(directResults, float64(elapsed))
			fmt.Printf("%.2f ms\n", float64(elapsed))
		} else {
			fmt.Printf("%.2f ms (warmup)\n", float64(elapsed))
		}
	}

	// Calculate statistics
	daemonStats := calcStats(daemonResults)
	directStats := calcStats(directResults)

	// Print results
	fmt.Println("\n=== RESULTS ===")
	fmt.Println("\nDAEMON MODE:")
	fmt.Printf("  Median:  %.2f ms\n", daemonStats.median)
	fmt.Printf("  Average: %.2f ms\n", daemonStats.avg)
	fmt.Printf("  Min:     %.2f ms\n", daemonStats.min)
	fmt.Printf("  Max:     %.2f ms\n", daemonStats.max)
	fmt.Printf("  P90:     %.2f ms\n", daemonStats.p90)

	fmt.Println("\nDIRECT MODE:")
	fmt.Printf("  Median:  %.2f ms\n", directStats.median)
	fmt.Printf("  Average: %.2f ms\n", directStats.avg)
	fmt.Printf("  Min:     %.2f ms\n", directStats.min)
	fmt.Printf("  Max:     %.2f ms\n", directStats.max)
	fmt.Printf("  P90:     %.2f ms\n", directStats.p90)

	speedupMedian := directStats.median / daemonStats.median
	speedupAvg := directStats.avg / daemonStats.avg
	speedupP90 := directStats.p90 / daemonStats.p90

	fmt.Println("\nSPEEDUP (Direct/Daemon):")
	fmt.Printf("  Median:  %.2fx\n", speedupMedian)
	fmt.Printf("  Average: %.2fx\n", speedupAvg)
	fmt.Printf("  P90:     %.2fx\n", speedupP90)

	// Generate report
	report := generateReport(query, daemonResults, directResults, daemonStats, directStats, speedupMedian, speedupAvg, speedupP90)
	reportPath := filepath.Join(projectRoot, "docs", "PERFORMANCE_REPORT.md")
	if err := os.WriteFile(reportPath, []byte(report), 0644); err != nil {
		fmt.Printf("\n[ERROR] Failed to write report: %v\n", err)
	} else {
		fmt.Printf("\nReport saved to: %s\n", reportPath)
	}
}

func checkDaemon() bool {
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://127.0.0.1:8080/api/v1/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
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

func generateReport(query string, daemonResults, directResults []float64, daemonStats, directStats stats, speedupMedian, speedupAvg, speedupP90 float64) string {
	daemonStr := strings.Join(strings.Fields(fmt.Sprint(daemonResults)), ", ")
	directStr := strings.Join(strings.Fields(fmt.Sprint(directResults)), ", ")

	analysis := ""
	if speedupMedian > 1 {
		analysis = fmt.Sprintf("✅ **Daemon mode is faster** by %.2fx (median). The daemon's cache and pre-indexed state significantly reduce context building time.", speedupMedian)
	} else {
		analysis = fmt.Sprintf("⚠️ **Direct mode is faster** by %.2fx (median). This may indicate that the project is too small to benefit from daemon caching, or the daemon overhead exceeds the benefits.", 1/speedupMedian)
	}

	return fmt.Sprintf(`# Orchestra Performance Report

Generated: %s

## Test Configuration

- Project: `+"`testdata/small`"+`
- Query: `+"`%s`"+`
- Iterations: 10 (after 1 warmup)
- Test modes: Daemon vs Direct (--no-daemon)

## Results

### Daemon Mode

| Metric | Value (ms) |
|--------|------------|
| Median | %.2f |
| Average | %.2f |
| Min | %.2f |
| Max | %.2f |
| P90 | %.2f |

### Direct Mode

| Metric | Value (ms) |
|--------|------------|
| Median | %.2f |
| Average | %.2f |
| Min | %.2f |
| Max | %.2f |
| P90 | %.2f |

### Speedup (Direct/Daemon)

| Metric | Speedup |
|--------|---------|
| Median | %.2fx |
| Average | %.2fx |
| P90 | %.2fx |

## Analysis

%s

## Raw Data

### Daemon Mode (ms)
%s

### Direct Mode (ms)
%s
`, time.Now().Format("2006-01-02 15:04:05"), query,
		daemonStats.median, daemonStats.avg, daemonStats.min, daemonStats.max, daemonStats.p90,
		directStats.median, directStats.avg, directStats.min, directStats.max, directStats.p90,
		speedupMedian, speedupAvg, speedupP90,
		analysis,
		daemonStr, directStr)
}
