# OTel Instrument Package Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add unit tests for the `internal/instrument/` package — currently has 0 test files despite full implementation.

**Architecture:** Two test files, same package (`package instrument`) so private helpers are accessible. `detect_test.go` covers `Detect()`. `instrument_test.go` covers `Instrument()`, `containsMarker()`, `detectModule()`, `findEntryPoint()`, `patchEntryPoint()`, and `injectGoImport()`. No network, no real package-manager invocations — all tests use `t.TempDir()` and a `noInstallGoLang` fixture with `InstallCmd: ""`.

**Tech Stack:** Go stdlib `testing`, `os`, `path/filepath`, `strings`. No third-party deps.

---

## File Structure

- Create: `internal/instrument/detect_test.go` — tests for `Detect()` + shared helpers `must`, `langNames`, `containsStr`
- Create: `internal/instrument/instrument_test.go` — tests for `Instrument()` and all private helpers + local helpers `writeFile`, `readFile`

---

### Task 1: detect_test.go — Detect() tests

**Files:**
- Create: `internal/instrument/detect_test.go`

- [ ] **Step 1: Write detect_test.go**

```go
package instrument

import (
	"os"
	"path/filepath"
	"testing"
)

// --- shared test helpers (used by both test files) ---

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func langNames(lcs []LangConfig) []string {
	names := make([]string, len(lcs))
	for i, lc := range lcs {
		names[i] = lc.Name
	}
	return names
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// --- Detect() tests ---

func TestDetect_Empty(t *testing.T) {
	dir := t.TempDir()
	got := Detect(dir, AllLangs)
	if len(got) != 0 {
		t.Fatalf("expected no detection in empty dir, got %v", langNames(got))
	}
}

func TestDetect_Go(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.21\n"), 0644))
	got := Detect(dir, AllLangs)
	if len(got) != 1 || got[0].Name != "go" {
		t.Fatalf("expected [go], got %v", langNames(got))
	}
}

func TestDetect_Python(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644))
	got := Detect(dir, AllLangs)
	if len(got) != 1 || got[0].Name != "python" {
		t.Fatalf("expected [python], got %v", langNames(got))
	}
}

func TestDetect_TypeScriptBeatsJS(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{}"), 0644))
	must(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644))
	got := Detect(dir, AllLangs)
	names := langNames(got)
	for _, n := range names {
		if n == "javascript" {
			t.Errorf("javascript should be suppressed when typescript detected; got %v", names)
		}
	}
	if !containsStr(names, "typescript") {
		t.Errorf("expected typescript in result, got %v", names)
	}
}

func TestDetect_GoAndPython(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.21\n"), 0644))
	must(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644))
	got := Detect(dir, AllLangs)
	names := langNames(got)
	if !containsStr(names, "go") || !containsStr(names, "python") {
		t.Fatalf("expected both go and python detected, got %v", names)
	}
}
```

- [ ] **Step 2: Run tests**

```powershell
go test ./internal/instrument/... -v -run TestDetect
```

Expected: 5 tests PASS. No failures.

- [ ] **Step 3: Commit**

```powershell
git add internal/instrument/detect_test.go
git commit -m "test(instrument): add Detect() unit tests"
```

---

### Task 2: instrument_test.go — Instrument() and helper tests

**Files:**
- Create: `internal/instrument/instrument_test.go`

- [ ] **Step 1: Write instrument_test.go**

```go
package instrument

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noInstallGoLang is goLang with InstallCmd cleared so tests never run 'go get'.
var noInstallGoLang = func() LangConfig {
	lc := goLang
	lc.InstallCmd = ""
	lc.InstallArgs = nil
	return lc
}()

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	must(t, os.MkdirAll(filepath.Dir(path), 0755))
	must(t, os.WriteFile(path, []byte(content), 0644))
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("readFile %s: %v", name, err)
	}
	return string(data)
}

// --- Instrument() dry-run ---

func TestInstrument_DryRun_NoFilesWritten(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/myapp\n\ngo 1.21\n")

	results, err := Instrument(dir, []LangConfig{noInstallGoLang}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Skipped {
		t.Error("should not be skipped")
	}
	if r.TelemetryFile != "internal/telemetry/otel.go" {
		t.Errorf("unexpected TelemetryFile: %q", r.TelemetryFile)
	}
	// dry-run: telemetry file must NOT exist on disk
	telPath := filepath.Join(dir, "internal", "telemetry", "otel.go")
	if _, err := os.Stat(telPath); err == nil {
		t.Error("dry-run must not write telemetry file to disk")
	}
}

// --- Instrument() idempotency ---

func TestInstrument_AlreadyInstrumented_Skipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/myapp\n\ngo 1.21\n")
	// File containing the marker string.
	writeFile(t, dir, "main.go", "package main\n\nimport \"go.opentelemetry.io/otel\"\n\nfunc main() { _ = otel.GetTracerProvider() }\n")

	results, err := Instrument(dir, []LangConfig{noInstallGoLang}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Skipped {
		t.Error("expected Skipped=true when marker already present")
	}
	if !strings.Contains(r.SkipReason, "already instrumented") {
		t.Errorf("unexpected SkipReason: %q", r.SkipReason)
	}
}

// --- Instrument() write mode ---

func TestInstrument_Go_WritesTelemetryFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/myapp\n\ngo 1.21\n")

	lc := noInstallGoLang
	lc.MainPatch = MainPatch{} // skip entry-point patching for this test
	results, err := Instrument(dir, []LangConfig{lc}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Skipped {
		t.Fatalf("unexpected results: %+v", results)
	}

	content := readFile(t, dir, "internal/telemetry/otel.go")
	if !strings.Contains(content, "InitTracer") {
		t.Errorf("expected InitTracer in telemetry file, got:\n%s", content)
	}
	if !strings.Contains(content, "localhost:4318") {
		t.Errorf("expected OTLP endpoint in telemetry file, got:\n%s", content)
	}
}

func TestInstrument_Go_PatchesMainGo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/myapp\n\ngo 1.21\n")
	writeFile(t, dir, "main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")

	results, err := Instrument(dir, []LangConfig{noInstallGoLang}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Patched {
		t.Error("expected Patched=true")
	}
	if r.PatchedFile != "main.go" {
		t.Errorf("expected PatchedFile=main.go, got %q", r.PatchedFile)
	}

	content := readFile(t, dir, "main.go")
	if !strings.Contains(content, "InitTracer") {
		t.Errorf("expected InitTracer call in patched main.go, got:\n%s", content)
	}
	if !strings.Contains(content, "internal/telemetry") {
		t.Errorf("expected import injection in patched main.go, got:\n%s", content)
	}
}

// --- containsMarker ---

func TestContainsMarker_Found(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n// go.opentelemetry.io/otel\nfunc main() {}\n")
	found, err := containsMarker(dir, []string{".go"}, "go.opentelemetry.io/otel")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("expected marker to be found")
	}
}

func TestContainsMarker_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	found, err := containsMarker(dir, []string{".go"}, "go.opentelemetry.io/otel")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected marker not found in plain file")
	}
}

func TestContainsMarker_WrongExtension(t *testing.T) {
	dir := t.TempDir()
	// marker is in a .txt file, but we only scan .go
	writeFile(t, dir, "notes.txt", "go.opentelemetry.io/otel\n")
	found, err := containsMarker(dir, []string{".go"}, "go.opentelemetry.io/otel")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected marker not found when only scanning .go files")
	}
}

// --- detectModule ---

func TestDetectModule_ParsesGoMod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module github.com/example/repo\n\ngo 1.21\n")
	got := detectModule(dir, goLang)
	if got != "github.com/example/repo" {
		t.Errorf("expected github.com/example/repo, got %q", got)
	}
}

func TestDetectModule_FallsBackToDirName(t *testing.T) {
	dir := t.TempDir()
	// no go.mod present
	got := detectModule(dir, goLang)
	if got != filepath.Base(dir) {
		t.Errorf("expected %q (dir base), got %q", filepath.Base(dir), got)
	}
}

// --- findEntryPoint ---

func TestFindEntryPoint_Direct(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	got, err := findEntryPoint(dir, []string{"main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "main.go") {
		t.Errorf("expected main.go path, got %q", got)
	}
}

func TestFindEntryPoint_Wildcard(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cmd/myapp/main.go", "package main\n")
	got, err := findEntryPoint(dir, []string{"cmd/*/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "cmd/myapp/main.go") {
		t.Errorf("expected cmd/myapp/main.go, got %q", got)
	}
}

func TestFindEntryPoint_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := findEntryPoint(dir, []string{"main.go", "cmd/*/main.go"})
	if err == nil {
		t.Error("expected error when no entry point found")
	}
}

// --- patchEntryPoint ---

func TestPatchEntryPoint_InsertsCallAndBackup(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	original := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	must(t, os.WriteFile(mainPath, []byte(original), 0644))

	err := patchEntryPoint(mainPath, "func main() {", "\n\tdefer telemetry.InitTracer(\"svc\")()", "")
	if err != nil {
		t.Fatal(err)
	}

	content := readFile(t, dir, "main.go")
	if !strings.Contains(content, "telemetry.InitTracer") {
		t.Errorf("expected init call in patched file, got:\n%s", content)
	}
	// Backup must exist.
	if _, err := os.Stat(mainPath + ".orchestra.bak"); err != nil {
		t.Errorf("expected backup file at %s.orchestra.bak, got: %v", mainPath, err)
	}
}

func TestPatchEntryPoint_MarkerNotFound_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	must(t, os.WriteFile(mainPath, []byte("package main\nfunc main() {}\n"), 0644))

	err := patchEntryPoint(mainPath, "NONEXISTENT_MARKER", "\n\tpatch()", "")
	if err == nil {
		t.Error("expected error when marker not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// --- injectGoImport ---

func TestInjectGoImport_ExistingBlock(t *testing.T) {
	src := "package main\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n"
	got := injectGoImport(src, "example.com/myapp/internal/telemetry")
	if !strings.Contains(got, `"example.com/myapp/internal/telemetry"`) {
		t.Errorf("expected import injected into block, got:\n%s", got)
	}
	if strings.Count(got, "import (") != 1 {
		t.Errorf("expected exactly one import block, got:\n%s", got)
	}
}

func TestInjectGoImport_NoImportBlock(t *testing.T) {
	src := "package main\n\nfunc main() {}\n"
	got := injectGoImport(src, "example.com/myapp/internal/telemetry")
	if !strings.Contains(got, "example.com/myapp/internal/telemetry") {
		t.Errorf("expected import added when no block exists, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests**

```powershell
go test ./internal/instrument/... -v
```

Expected: all tests PASS. Output should include lines like:
```
--- PASS: TestInstrument_DryRun_NoFilesWritten
--- PASS: TestInstrument_AlreadyInstrumented_Skipped
--- PASS: TestInstrument_Go_WritesTelemetryFile
--- PASS: TestInstrument_Go_PatchesMainGo
--- PASS: TestContainsMarker_Found
... (20 tests total)
ok  	github.com/orchestra/orchestra/internal/instrument
```

- [ ] **Step 3: Run full test suite to confirm no regressions**

```powershell
go test ./... 2>&1 | Select-String -Pattern "FAIL|ok" | head -30
```

Expected: all packages `ok`, no `FAIL` lines.

- [ ] **Step 4: Commit**

```powershell
git add internal/instrument/instrument_test.go
git commit -m "test(instrument): add Instrument() and helper unit tests"
```

---

## Self-Review Checklist

**Spec coverage:**
- `Detect()` — ✅ Task 1: 5 tests (empty, go, python, ts>js, go+python)
- `Instrument()` dry-run — ✅ Task 2: TestInstrument_DryRun_NoFilesWritten
- `Instrument()` idempotency — ✅ Task 2: TestInstrument_AlreadyInstrumented_Skipped
- `Instrument()` write mode — ✅ Task 2: TestInstrument_Go_WritesTelemetryFile + TestInstrument_Go_PatchesMainGo
- `containsMarker()` — ✅ Task 2: 3 tests (found, not found, wrong extension)
- `detectModule()` — ✅ Task 2: 2 tests (parses go.mod, falls back to dir name)
- `findEntryPoint()` — ✅ Task 2: 3 tests (direct, wildcard, not found)
- `patchEntryPoint()` — ✅ Task 2: 2 tests (inserts + backup, marker not found)
- `injectGoImport()` — ✅ Task 2: 2 tests (existing block, no block)

**Placeholder scan:** No TBD/TODO/placeholder language found.

**Type consistency:** All functions called match signatures in `internal/instrument/instrument.go` and `detect.go`.
