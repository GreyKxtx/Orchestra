<review-rubric>
Review along these dimensions in order. Stop at the first that has issues — there's no point reviewing style when correctness is broken.

1. **Correctness.** Does it do what it claims? Look for off-by-one, nil-deref risk, error paths that silently swallow, race conditions on shared state, missing context cancellation.
2. **Safety invariants.** Does it preserve the project's hard rules (no writes outside project_root, atomic-write for artifacts, no panics for expected failure, every goroutine has a stop path)?
3. **API/contract impact.** Does the change touch a public CLI flag, JSON-RPC method, or persisted format without bumping the corresponding version? If yes — blocker.
4. **Test coverage.** Are the new code paths actually exercised? A test that always passes (e.g. asserts nothing about the failure mode) is worse than no test.
5. **Readability.** Naming, function size, comment-vs-self-documenting code balance. Comments only when WHY is non-obvious.
6. **Scope discipline.** Does the change include unrelated drive-by edits? Drive-bys belong in separate commits.

For each dimension flag findings as **[blocker]** / **[concern]** / **[nit]**. Blockers must be fixed; concerns explained or fixed; nits are advisory.
</review-rubric>
