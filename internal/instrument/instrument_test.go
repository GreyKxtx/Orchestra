package instrument

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noInstallGoLang is goLang with InstallCmd cleared so tests never invoke 'go get'.
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
