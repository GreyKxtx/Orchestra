# Python engineering L0 defaults

**Narrowed by:** `.orchestra/playbooks/conventions.md` (L1, Docs Lead) picks the
rows that apply to this project; a dept playbook (e.g. `backend.md`, L2) may
narrow further but never contradicts an approved decision. L1/L2 may only
**narrow** this file — weakening a row here requires an `accepted_risk` entry
approved by the User in `decisions.md`.

## Type hints & structure

| Rule | Why |
|---|---|
| New/touched functions get type hints on parameters and return value | Lets a type checker catch mismatches before runtime, and doubles as inline docs |
| A checker (`mypy` or `pyright`) runs in CI, not just the editor | An unused hint that nobody checks is decoration, not a guarantee |
| Dataclasses/`pydantic` models for structured data crossing a boundary (API, config, DB row) — not bare dicts | A typo'd dict key fails at the point of use, not at construction |
| No mutable default arguments (`def f(x=[])`) | The list is shared across every call that doesn't pass one — a classic silent bug |

## Error handling

| Rule | Why |
|---|---|
| `except Exception:` (or bare `except:`) only at a top-level boundary that logs and re-raises or exits — never to silently continue | A caught-and-ignored exception hides the actual failure from everyone downstream |
| Raise a specific exception type (custom or stdlib), not a bare `Exception("...")` | Callers need something to `except` on other than parsing the message string |
| Use `raise NewError(...) from err` when re-wrapping, not a bare `raise NewError(...)` | Keeps the original traceback instead of pointing at the wrong line |
| Context managers (`with open(...) as f:`) for anything that must be closed/released | A `finally: f.close()` is easy to forget; `with` makes it structural |

## Testing

| Rule | Why |
|---|---|
| `pytest` (or the project's actual runner) with one test file per module, `test_<name>.py` | Predictable discovery, and a failing file names its module |
| Fixtures for setup, not copy-pasted setup code across test functions | A fixture change propagates; copy-paste drifts silently |
| A bug fix ships with a regression test that fails on the pre-fix code | Otherwise the same bug returns silently next refactor |
| Mock only true I/O boundaries (network, filesystem, subprocess, clock) — not your own modules | Mocking your own code hides the bug the test was supposed to catch |

## Build & lint (exact commands — fill in what this project actually uses)

| Command | Purpose |
|---|---|
| `python -m pytest` | Full test suite |
| `mypy .` / `pyright` | Type-checking (record which one this project uses) |
| `ruff check .` (or `flake8`) | Lint — record the config path here |
| `ruff format .` (or `black .`) | Formatting — CI should fail on a diff, not just warn |

## Project layout

- `src/<package>/` layout (not a bare top-level package) so the installed
  package can't accidentally be shadowed by the working directory during
  tests.
- One virtual environment / lockfile mechanism per project (`poetry`,
  `uv`, `pip-tools`) — record which one; mixing `requirements.txt` edits
  with an unrelated lockfile is how dependency drift starts.
- Scripts meant to be run directly (`if __name__ == "__main__":`) stay out
  of importable library modules, or guard the side-effecting part behind
  that check.

## Common anti-patterns (what recurring lessons in this dept usually mean)

- **Catching `Exception` to keep a script running past an error it doesn't
  actually understand** — the fix is almost never the broad except, it's
  handling (or deliberately propagating) the specific failure.
- **A mutable default argument surviving into production** (see table
  above) — this is one of the highest-recurrence lessons in Python code
  specifically because it passes a casual review.
- **Circular imports resolved by moving an import inside a function** — the
  import cycle usually means two modules should be one, or a shared
  interface needs to move to a third module.
- **`assert` used for input validation on a path that ships with
  `python -O`** — asserts are stripped under optimization; validation that
  must always run needs an explicit `raise`.
