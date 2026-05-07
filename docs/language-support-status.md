# Language Support Status

Two independent subsystems: **CKG** (code graph / parser) and **OTel Instrumentation** (`orchestra init --instrument`).

---

## CKG — Code Knowledge Graph

| Language | tree-sitter | FQN | containers | defs | imports | calls | complexity |
|----------|-------------|-----|------------|------|---------|-------|------------|
| Go | ✅ | ✅ | ✅ (via parseGoFile) | ✅ | ✅ | ✅ | ✅ |
| TypeScript / TSX | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| JavaScript / JSX | ✅ | ✅ (shared with TS) | ✅ | ✅ | ✅ | ✅ | ❌ |
| Python | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| Rust | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| Java | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| C | ❌ (no sitter) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| C++ | ❌ (no sitter) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Kotlin | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Scala | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| C# | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Ruby | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| PHP | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Swift | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

**Notes:**
- Go has a dedicated parser (`parseGoFile`) with full type/interface/struct/method support
- TS/JS/Python/Rust/Java use the generic tree-sitter pipeline (`parseGenericFile`)
- C and C++ appear in `LanguageFromExt` but have no tree-sitter binding compiled in — files are skipped silently
- Complexity scoring (`countComplexity`) is Go-only so far

---

## OTel Instrumentation — `orchestra init --instrument`

| Language | Detect | Install | telemetry file | Entry patch | Phase |
|----------|--------|---------|---------------|-------------|-------|
| Go | ✅ (`go.mod`) | ✅ `go get` | ✅ `internal/telemetry/otel.go` | ✅ injects `InitTracer` + import | 1 ✅ |
| Python | ✅ (`requirements.txt`, `pyproject.toml`, `setup.py`) | ✅ `pip install` | ✅ `telemetry.py` | ✅ injects `init_tracer` call | 1 ✅ |
| TypeScript | ✅ (`tsconfig.json`) | ✅ `npm install` | ✅ `telemetry.ts` | ❌ (loaded via `--require`, no code patch) | 1 ✅ |
| JavaScript | ✅ (`package.json`) | ✅ `npm install` | ✅ `telemetry.js` | ❌ (loaded via `--require`, no code patch) | 1 ✅ |
| Java | ❌ | ❌ | ❌ | ❌ | 2 ❌ |
| Kotlin | ❌ | ❌ | ❌ | ❌ | 2 ❌ |
| Scala | ❌ | ❌ | ❌ | ❌ | 2 ❌ |
| C# / .NET | ❌ | ❌ | ❌ | ❌ | 2 ❌ |
| Rust | ❌ | ❌ | ❌ | ❌ | 3 ❌ |
| Ruby | ❌ | ❌ | ❌ | ❌ | 3 ❌ |
| PHP | ❌ | ❌ | ❌ | ❌ | 3 ❌ |
| Elixir/Erlang | ❌ | ❌ | ❌ | ❌ | 3 ❌ |
| Swift | ❌ | ❌ | ❌ | ❌ | 4 ❌ |
| Dart | ❌ | ❌ | ❌ | ❌ | 4 ❌ |

**Notes:**
- Phase 1 langs (`Go`, `Python`, `TypeScript`, `JavaScript`) are fully implemented in `internal/instrument/lang.go`
- Architecture is data-driven (`LangConfig` struct) — adding a language = one struct, no logic changes
- TS/JS don't patch the entry point (loaded via Node.js `--require`), so `MainPatch` is empty
- Phase 2+ langs are on the roadmap but not started

---

## Summary

| | CKG | OTel Instrument |
|---|-----|----------------|
| Go | ✅ full | ✅ done |
| TypeScript / JavaScript | ✅ full | ✅ done |
| Python | ✅ full | ✅ done |
| Rust | ✅ full (CKG) | ❌ not done |
| Java | ✅ full (CKG) | ❌ not done |
| C / C++ | ❌ not done | ❌ not done |
| Everything else | ❌ not done | ❌ not done |
