# Context-only Go benchmarks (no LLM).
# Usage: .\run_benchmark.ps1

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path $PSScriptRoot -Parent

Write-Host "=== Orchestra Context Benchmarks (Go) ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "[INFO] Measuring context build only (no LLM)" -ForegroundColor Yellow
Write-Host ""

Set-Location $projectRoot

Write-Host "[INFO] Command:" -ForegroundColor Yellow
Write-Host "  go test -run=^$ -bench=BenchmarkContext -benchmem -count=5 ./tests/benchmark" -ForegroundColor Gray
Write-Host ""

$output = & go test -run=^$ -bench=BenchmarkContext -benchmem -count=5 ./tests/benchmark 2>&1 | Out-String
Write-Host $output

$outPath = Join-Path $projectRoot "bench_output.txt"
$output | Out-File -FilePath $outPath -Encoding UTF8

Write-Host ""
Write-Host "[INFO] Updating docs/PERFORMANCE_REPORT.md..." -ForegroundColor Yellow
$updateResult = Get-Content $outPath | go run ./tests/benchmark/update_report.go 2>&1
if ($LASTEXITCODE -eq 0) {
    Write-Host $updateResult
    Write-Host "[INFO] Report updated successfully!" -ForegroundColor Green
} else {
    Write-Host "[WARNING] Failed to update report: $updateResult" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "=== Done ===" -ForegroundColor Green
Write-Host "See docs/PERFORMANCE_REPORT.md for interpretation." -ForegroundColor Cyan
