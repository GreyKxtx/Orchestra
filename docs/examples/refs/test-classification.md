<test-classification>
A failing test is not automatically a bug. Misdiagnosing a flake as a bug wastes hours; misdiagnosing a bug as a flake ships broken code. Classify deliberately.

**Buckets (decide for each failure):**

1. **Real failure** — deterministic, reproducible locally with the same inputs. Always the same assertion fails the same way. Fix in code.

2. **Regression** — a real failure that was green on previous commit. Bisect to identify the introducing change. High priority.

3. **Flaky** — fails sometimes, passes sometimes, no code change in between. Common causes:
   - Race conditions (concurrent writes to shared state, missing synchronisation).
   - Timing assumptions (`time.Sleep(100ms)` and CI is slow today).
   - Order dependency (test A leaves state that test B depends on).
   - External dependency (network, DNS, third-party API).
   - Resource exhaustion (port conflict, FD limit).
   **Never silently skip / xfail a flaky test.** File the race, then either fix or quarantine with a TODO+date+owner.

4. **Environmental** — passes locally, fails in CI (or vice versa). Diff the environments: Go version, OS, env vars, locale, available binaries, file paths (Windows `\` vs Unix `/`). Fix the test to be portable.

5. **Outdated** — test correctly fails because the spec changed. Update the test (and confirm with the spec owner that the spec change was intentional).

6. **Bad test** — assertion was wrong / over-specific (asserts on private impl, on map iteration order, on log line counts). Fix the assertion, not the code.

**Output format (per failing test):**
```
- <pkg>.TestX — <real|regression|flaky|env|outdated|bad-test> — <one-line evidence>
  Repro: <command|condition>
  Fix direction: <code | test | infra | needs-decision>
```

**Discipline:**
- Don't classify based on what's convenient. A flake under deadline pressure that you ship today is a bug that pages someone next quarter.
- "I re-ran it and it passed" alone is NOT proof of flakiness. Run it 100 times under load before declaring flake.
- If the same test is in two buckets across runs, it's flaky — the worst kind.
</test-classification>
