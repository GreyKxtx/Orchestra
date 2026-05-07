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
| C | ✅ | ✅ | ✅ (struct/union/enum) | ✅ | ✅ (#include) | ✅ | ✅ |
| C++ | ✅ | ✅ | ✅ (class/struct/enum) | ✅ | ✅ (#include) | ✅ | ✅ |
| C# | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Kotlin | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Scala | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Ruby | ✅ | ✅ | ✅ (class/module) | ✅ | ❌ (no import syntax) | ✅ | ✅ |
| PHP | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Elixir | ✅ | ✅ | ✅ (defmodule) | ✅ (def/defp) | ❌ | ✅ | ✅ |
| Swift | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

**Notes:**
- Go has a dedicated parser (`parseGoFile`) with full type/interface/struct/method support
- All other supported languages use the generic tree-sitter pipeline (`parseGenericFile`)
- C/C++ FQN uses file-relative path as prefix (e.g. `src/main.c::func`)
- C# / Kotlin / Scala / PHP FQN uses dotted namespace (e.g. `com.example.ClassName.method`)
- Ruby / Elixir FQN uses file-relative path or module name
- Complexity scoring (`countComplexity`) is available for all supported languages

---

## OTel Instrumentation — `orchestra init --instrument`

| Language | Detect | Install | telemetry file | Entry patch | Phase |
|----------|--------|---------|---------------|-------------|-------|
| Go | ✅ (`go.mod`) | ✅ `go get` | ✅ `internal/telemetry/otel.go` | ✅ injects `InitTracer` + import | 1 ✅ |
| Python | ✅ (`requirements.txt`, `pyproject.toml`, `setup.py`) | ✅ `pip install` | ✅ `telemetry.py` | ✅ injects `init_tracer` call | 1 ✅ |
| TypeScript | ✅ (`tsconfig.json`) | ✅ `npm install` | ✅ `telemetry.ts` | ❌ (loaded via `--require`, no code patch) | 1 ✅ |
| JavaScript | ✅ (`package.json`) | ✅ `npm install` | ✅ `telemetry.js` | ❌ (loaded via `--require`, no code patch) | 1 ✅ |
| Java | ✅ (`pom.xml`, `build.gradle`) | ✅ (instructions in file) | ✅ `src/main/java/telemetry/OtelConfig.java` | ❌ (manual) | 2 ✅ |
| Kotlin | ✅ (`build.gradle.kts`) | ✅ (instructions in file) | ✅ `src/main/kotlin/telemetry/OtelConfig.kt` | ❌ (manual) | 2 ✅ |
| Scala | ✅ (`build.sbt`) | ✅ (instructions in file) | ✅ `src/main/scala/telemetry/OtelConfig.scala` | ❌ (manual) | 2 ✅ |
| C# / .NET | ✅ (`*.csproj`, `*.sln`) | ✅ `dotnet add package` | ✅ `Telemetry/OtelConfig.cs` | ❌ (manual) | 2 ✅ |
| Rust | ✅ (`Cargo.toml`) | ✅ `cargo add` | ✅ `src/telemetry.rs` | ❌ (manual) | 3 ✅ |
| Ruby | ✅ (`Gemfile`) | ✅ `bundle add` | ✅ `lib/telemetry.rb` | ❌ (manual) | 3 ✅ |
| PHP | ✅ (`composer.json`) | ✅ `composer require` | ✅ `src/Telemetry/OtelConfig.php` | ❌ (manual) | 3 ✅ |
| Elixir | ✅ (`mix.exs`) | ✅ (instructions in file) | ✅ `lib/telemetry/otel.ex` | ❌ (manual) | 3 ✅ |
| Erlang | ❌ | ❌ | ❌ | ❌ | 4 ❌ |
| Swift | ❌ | ❌ | ❌ | ❌ | 4 ❌ |
| Dart | ❌ | ❌ | ❌ | ❌ | 4 ❌ |

**Notes:**
- Phase 1 langs (`Go`, `Python`, `TypeScript`, `JavaScript`) fully implemented including entry-point patching
- Phase 2 (`Java`, `Kotlin`, `Scala`, `C#`) and Phase 3 (`Rust`, `Ruby`, `PHP`, `Elixir`) write the telemetry file and run package install; entry-point patching is manual (architecture varies too much per framework)
- Architecture is data-driven (`LangConfig` struct) — adding a language = one struct, no logic changes
- C# detection uses glob patterns (`*.csproj`) via updated `Detect` function
- TS/JS don't patch the entry point (loaded via Node.js `--require`), so `MainPatch` is empty

---

## Summary

| | CKG | OTel Instrument |
|---|-----|----------------|
| Go | ✅ full | ✅ done |
| TypeScript / JavaScript | ✅ full | ✅ done |
| Python | ✅ full | ✅ done |
| Rust | ✅ full (CKG) | ✅ done |
| Java | ✅ full (CKG) | ✅ done |
| C / C++ | ✅ full (CKG) | ❌ not done |
| C# | ✅ full (CKG) | ✅ done |
| Kotlin | ✅ full (CKG) | ✅ done |
| Scala | ✅ full (CKG) | ✅ done |
| Ruby | ✅ full (CKG) | ✅ done |
| PHP | ✅ full (CKG) | ✅ done |
| Elixir | ✅ full (CKG) | ✅ done |
| Swift / Dart / Erlang | ❌ not done | ❌ not done |
