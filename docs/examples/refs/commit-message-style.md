<commit-message-style>
Conventional-commit subject + a body that explains **why**.

**Subject (≤ 72 chars):**
`<type>(<scope>): <imperative summary>`

- `type`: `feat` (new capability), `fix` (bug fix), `refactor` (behaviour-preserving restructure), `docs`, `test`, `chore`, `perf`.
- `scope`: package or area — `cli`, `agent`, `workflow`, `skills`, `llm`, `tools`, `core`, `protocol`, etc.
- Imperative mood: "add X", "fix Y", not "added"/"fixed".
- One subject = one logical change. If you can't summarise it in one line, the commit is too big.

**Body (wrap at 72 cols):**
Explains the **why**, the constraint that forced this shape, and any non-obvious consequence. The diff already shows the **what** — don't repeat it.

Include if applicable:
- The user-visible behaviour change.
- The invariant that motivated the change (link to ref / rule / issue).
- Anything a future reader would otherwise have to reconstruct from `git blame`.

**What NOT to include:**
- "Updates" / "Some changes" / "Misc" — these defeat search.
- Restatement of the diff line-by-line.
- TODOs in the message — put them in code or in an issue.
- "Co-Authored-By" lines unless the user asked for them.

**Examples (good):**
```
feat(workflow): YAML-driven multi-stage skill orchestration
fix(agent): retry on StaleContent only if file_hash actually changed
refactor(resolver): collapse three-pass matcher into one allocation
```

**Examples (bad):**
```
update stuff
fix bug
feat: add workflow runner with topological sort, parallel execution,
  marker detection, redo routing, max_attempts limit, $ARGUMENTS subst…
  ↑ too much in one commit; split.
```
</commit-message-style>
