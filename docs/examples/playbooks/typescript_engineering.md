# TypeScript engineering L0 defaults

**Narrowed by:** `.orchestra/playbooks/conventions.md` (L1, Docs Lead) picks the
rows that apply to this project; a dept playbook (e.g. `frontend.md`, L2) may
narrow further but never contradicts an approved decision. L1/L2 may only
**narrow** this file — weakening a row here requires an `accepted_risk` entry
approved by the User in `decisions.md`.

## Type safety

| Rule | Why |
|---|---|
| `strict: true` in `tsconfig.json` (or record why not, as an accepted_risk) | `any` leaks past every other rule below once strict mode is off |
| `any` requires a comment saying why the type can't be narrowed | An unexplained `any` is a type hole nobody will revisit |
| Prefer `unknown` + a narrowing check over `any` at a trust boundary (API response, `JSON.parse`, webview message) | Forces the caller to validate before use instead of trusting shape |
| No non-null assertion (`!`) on a value that can genuinely be null/undefined at runtime | It converts a compile-time catch into a runtime crash |
| Discriminated unions (a `type`/`kind` field) for anything with >2 variants, not a pile of optional fields | The compiler can then exhaustiveness-check `switch` statements |

## Error handling

| Rule | Why |
|---|---|
| `catch (err: unknown)`, narrow before reading `.message` | `catch (err: any)` silently defeats strict mode at the one place errors actually surface |
| A rejected promise is either awaited-and-caught or explicitly `.catch()`'d — never a bare fire-and-forget | An unhandled rejection is a bug that only appears in the console, if that |
| User-facing error text says what happened and what to do; no raw stack traces in UI | Matches the project's own copy conventions, not just this playbook |

## Testing

| Rule | Why |
|---|---|
| Test behavior through the public API/rendered output, not internal state | Internal-state assertions break on refactors that don't change behavior |
| Mock only true I/O boundaries (network, filesystem, timers) — not your own modules | Mocking your own code hides the bug the test was supposed to catch |
| A bug fix ships with a regression test that fails on the pre-fix code | Otherwise the same bug returns silently next refactor |

## Build & lint (exact commands — fill in what this project actually uses)

| Command | Purpose |
|---|---|
| `tsc --noEmit` | Type-checks without emitting — the fastest whole-project correctness signal |
| `npm test` / `npm run test` | Full test suite (record the actual runner: vitest, jest, node --test) |
| `eslint .` | Project's lint ruleset — record the config path here |
| `npm run build` | Confirms the bundler/compiler step that CI/deploy actually runs |

## Project layout

- Co-locate a module's types with the module, not in one giant `types.ts`
  that every file imports from and any change touches.
- Barrel files (`index.ts` re-exporting a directory) are a convenience, not
  a place to add logic — a barrel with side effects breaks tree-shaking and
  hides which file actually defines something.
- Environment-specific code (Node-only, browser-only, webview-only) stays
  behind an explicit boundary/adapter, not scattered `typeof window` checks.

## Common anti-patterns (what recurring lessons in this dept usually mean)

- **Widening a type with `as` to make an error go away** — the fix is
  almost never the cast, it's that the value's actual shape disagrees with
  what the code assumes; re-check the source of the value.
- **`useEffect`/equivalent with a missing or wrong dependency array** (React
  or similar reactive frameworks) — causes either stale closures or an
  infinite render loop; both are recurring-lesson material.
- **Reaching into another module's internals via a deep relative import**
  (`../../../foo/internal/bar`) instead of its public export — signals the
  module boundary itself is wrong, not just the import path.
- **Swallowing a promise rejection with `.catch(() => {})`** — this is
  worse than not catching at all, because it looks handled in review.
