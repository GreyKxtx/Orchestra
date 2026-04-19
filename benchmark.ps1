# Context-only benchmark script for Orchestra
# Usage: .\benchmark.ps1

$ErrorActionPreference = "Stop"
$projectRoot = $PSScriptRoot
$benchCount = 5

Write-Host "=== Orchestra Context Benchmarks (Go) ===" -ForegroundColor Cyan
Write-Host "Project root: $projectRoot" -ForegroundColor Gray
Write-Host "Large benches are skipped by default." -ForegroundColor Gray
Write-Host "Enable with: `$env:ORCHESTRA_BENCH_LARGE=1" -ForegroundColor Gray
Write-Host ""

Set-Location $projectRoot

$cmd = "go test -run=^$ -bench=BenchmarkContext -benchmem -count=$benchCount ./tests/benchmark"
Write-Host "[INFO] Running:" -ForegroundColor Yellow
Write-Host "  $cmd" -ForegroundColor Gray
Write-Host ""

$output = & go test -run=^$ -bench=BenchmarkContext -benchmem -count=$benchCount ./tests/benchmark 2>&1 | Out-String
Write-Host $output

$outPath = Join-Path $projectRoot "bench_output.txt"
$output | Out-File -FilePath $outPath -Encoding UTF8

Write-Host "" 
Write-Host "[INFO] Raw output saved to: $outPath" -ForegroundColor Green

# Update PERFORMANCE_REPORT.md
Write-Host "[INFO] Updating docs/PERFORMANCE_REPORT.md..." -ForegroundColor Yellow
$reportUpdateCmd = "Get-Content $outPath | go run ./tests/benchmark/update_report.go"
$updateResult = & powershell -NoProfile -Command $reportUpdateCmd 2>&1
if ($LASTEXITCODE -eq 0) {
    Write-Host $updateResult
    Write-Host "[INFO] Report updated successfully!" -ForegroundColor Green
} else {
    Write-Host "[WARNING] Failed to update report: $updateResult" -ForegroundColor Yellow
}

Write-Host "[INFO] See docs/PERFORMANCE_REPORT.md for interpretation." -ForegroundColor Cyan
