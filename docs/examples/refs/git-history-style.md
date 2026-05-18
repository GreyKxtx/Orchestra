<git-history-style>
A clean history is a debugging tool. Every commit should be a self-contained, working step that bisect can land on.

**Per-commit rules:**
- Compiles in isolation. Tests pass in isolation. Bisect-friendly.
- One logical change — see `@refs/atomic-commit-discipline`.
- Subject in imperative mood, ≤ 72 chars. Body explains *why*.
- No "WIP", "fixup", "checkpoint" in main history. Use `git commit --fixup` during dev, then autosquash before merging.

**Branch hygiene (before merge):**
1. Rebase onto current main — surface conflicts now, not at merge time.
2. Squash fixups (`git rebase -i --autosquash`).
3. Re-order so logically related commits are adjacent.
4. Verify each commit still passes its own tests (`git rebase --exec "go test ./..."`).
5. Force-push to your branch ONLY (never to shared branches).

**What NOT to squash:**
- Distinct logical changes — keep them as separate commits even if small. "feat(x): add Y" + "test(x): cover Z" are two commits, not one mushy "feat+test" blob.
- Mechanical refactors before a feature — the refactor is reviewable evidence the feature is small.

**Merge vs rebase:**
- Feature branch → main: **rebase** (linear history, no merge bubbles).
- Long-lived shared branches → main: **merge** (preserves the parallel-work signal).
- Avoid `--no-ff` merges for PRs unless team convention requires the merge commit marker.

**Anti-patterns:**
- 30-commit PRs with messages like "fix", "more fix", "actually fix".
- One mega-commit titled "feat: implement everything".
- Reverts done as fresh commits without `Revert "..."` prefix (lose the link to original).
- `git rebase -i` on commits other people have based work on.
</git-history-style>
