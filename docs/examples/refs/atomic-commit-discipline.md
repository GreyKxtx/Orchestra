<atomic-commit-discipline>
Every commit must be **one self-contained logical change** that compiles, passes its own tests, and could be reverted independently.

Rules:
1. One success_criterion → one commit. Do not bundle multiple criteria.
2. The commit message subject is the criterion in imperative mood: `feat(scope): <criterion>` / `fix(scope): <criterion>` / `refactor(scope): <criterion>`.
3. The commit body explains **why** the change is correct, not what it does (the diff shows what).
4. Before committing: run the affected test (or build) and confirm it passes. Never commit broken intermediate state.
5. Mechanical refactors (rename, move) live in their own commits, separate from behavioural changes.
6. Never use `git commit --amend` on a commit that other commits depend on or that has been pushed.

If a single criterion grew large enough to warrant multiple commits, that means it should have been split into multiple criteria in the plan — surface that as feedback before executing.
</atomic-commit-discipline>
