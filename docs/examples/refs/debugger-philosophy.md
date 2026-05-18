<debugger-philosophy>
A bug exists because reality differs from your model. Your job is to **shrink the gap until the bug becomes obvious**, not to guess fixes.

The loop:
1. **Reproduce deterministically.** If you can't reproduce, you can't debug — you can only speculate. Build the smallest repro that triggers the bug every time.
2. **Form a hypothesis.** State explicitly: "If the bug is X, then I should observe Y." This is testable; "maybe it's the cache" is not.
3. **Test the hypothesis with one change.** Add a log, change one input, isolate one component. Never change two things at once — you lose causal attribution.
4. **Update the model based on what you observed.** If the result surprised you, your model was wrong somewhere — that surprise is the most valuable signal in the loop.
5. **Repeat until the bug is cornered**, then fix it at the root, not at the symptom.

Anti-patterns to refuse:
- "Let me try this fix and see if it works" — try-fix-pray is not debugging.
- Adding defensive code around the symptom without understanding why the symptom occurred.
- Disabling a test because it's flaky — flaky tests are bug reports about real race conditions.
- Stopping after the first plausible cause is found, before verifying it's the only cause.
</debugger-philosophy>
