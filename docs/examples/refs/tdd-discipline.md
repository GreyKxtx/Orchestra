<tdd-discipline>
Test-Driven Development — write the failing test first, watch it fail, then implement the minimum to make it pass.

**The cycle (RED → GREEN → REFACTOR):**

1. **RED.** Write a single failing test that captures one behaviour. Run it. Confirm it fails with the expected error message — not a different one. If it fails for the wrong reason (compile error, missing import), you haven't tested what you think.

2. **GREEN.** Write the minimum implementation to make this one test pass. Resist the urge to add "while I'm here" code. Run the test. Confirm it passes — and that previously-green tests still pass.

3. **REFACTOR.** Now that you have a safety net, restructure for clarity. Re-run the full suite. If it stays green, the refactor was behaviour-preserving.

**Rules:**
- One behaviour per test. Tests with multiple `assert`s on unrelated properties signal you should split.
- Test name describes the behaviour: `TestX_WhenY_DoesZ`. Not `TestX1`.
- Assert against observable behaviour, not implementation details. If you assert on a private field, refactor breaks the test.
- A test that always passes (no failure path) is worse than no test. Always run it in the RED state first to confirm it can fail.
- Coverage is a side effect of TDD, not the goal. Don't game it.

**Anti-patterns:**
- Writing implementation first and tests after ("test-after development") — produces tests that confirm what the code does, not what it should do.
- Mocking the unit under test instead of its collaborators.
- Test bodies that duplicate the implementation logic (the test will agree with the impl even when both are wrong).
- Skipping/commenting flaky tests instead of fixing the underlying race.
</tdd-discipline>
