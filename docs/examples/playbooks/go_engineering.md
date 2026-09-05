# Go engineering L0 defaults

**Narrowed by:** `.orchestra/playbooks/conventions.md` (L1, Docs Lead) picks the
rows that apply to this project; a dept playbook (e.g. `backend.md`, L2) may
narrow further but never contradicts an approved decision. L1/L2 may only
**narrow** this file — weakening a row here requires an `accepted_risk` entry
approved by the User in `decisions.md`.

## Error handling

| Rule | Why |
|---|---|
| Never `panic` outside `main()`/test setup/a documented invariant check | A library panic crashes the caller's whole process; return an error instead |
| Wrap with `fmt.Errorf("%s: %w", context, err)`, never `errors.New(err.Error())` | `%w` keeps the chain walkable with `errors.Is`/`errors.As` |
| Never discard an error with `_ = f()` unless the comment says why it's safe | A silently dropped error is the #1 source of "it worked on my machine" |
| `defer f.Close()` right after a successful `Open`, check the error only if the write path matters | Consistent placement makes a missing `Close` visible in review |
| Sentinel errors are `var Err... = errors.New(...)`, package-level, doc-commented | Callers need something stable to compare against, not a string |

## Testing

| Rule | Why |
|---|---|
| Table-driven tests for anything with >2 branches; `t.Run(name, ...)` per case | One failing row names itself instead of "TestFoo failed" |
| `t.TempDir()` for filesystem tests, never a shared fixture dir | Parallel test runs must not collide or leak state between runs |
| Assert behavior (`got != want`), never call-count on a mock unless the call itself is the behavior under test | A test that only proves the mock was called proves nothing about correctness |
| A bug fix ships with a regression test that fails on the pre-fix code | Otherwise the same bug returns silently next refactor |
| `go test ./... -race` on any package touching goroutines/channels/shared state before merge | Race conditions rarely reproduce in a normal `go test` run |

## Build & lint (exact commands — fill in what this project actually uses)

| Command | Purpose |
|---|---|
| `go build ./...` | Compiles every package; catches type errors across the whole module |
| `go vet ./...` | Catches suspicious constructs `go build` doesn't (printf mismatches, lock copies) |
| `go test ./...` | Full test suite |
| `gofmt -l .` (or `goimports -l .`) | Non-empty output means unformatted files — CI should fail on this |
| `golangci-lint run` (if configured) | Project's actual lint ruleset — record the config path here |

## Project layout

- `internal/` for anything not meant to be imported by other modules; a
  package that's already there almost always stays there rather than moving
  to a public path "to be reusable" without an actual second importer.
- One `go.work` (or single-module) boundary is the default — a new module is
  a build-topology decision, not something to add for a single package.
- Tests live next to the code they test (`foo.go` + `foo_test.go`), not in a
  parallel `tests/` tree, except integration/e2e suites that genuinely span
  packages.

## Common anti-patterns (what recurring lessons in this dept usually mean)

- **Ignoring a returned error to keep a function signature simple** — the
  fix is almost never "add a `_ =`", it's "decide what the caller does when
  this fails."
- **A goroutine that outlives the request/turn with no cancellation path** —
  every spawned goroutine needs a `context.Context` or a `done` channel a
  caller can signal.
- **Mutating a shared slice/map from multiple goroutines without a mutex or
  channel** — `go test -race` catches most of these; run it before trusting
  a passing suite.
- **A type assertion (`x.(T)`) without the two-value form** in code that
  isn't immediately following a type switch that already guarantees `T` —
  the panic surfaces far from the actual bug.
