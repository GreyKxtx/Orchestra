<pr-description-template>
A good PR description lets a reviewer accept or reject without opening the diff first.

**Title (≤ 70 chars):** `<type>(<scope>): <imperative summary>` — same style as commit subjects.

**Body structure:**

```markdown
## Summary
<1-3 bullets — the WHY in plain language. What user-visible problem/opportunity does this address?>

## Changes
<3-7 bullets — what changed, by area. Reference key files. Skip noise (formatting, renames).>

## Test plan
- [ ] <observable verification step a reviewer could run>
- [ ] <regression check for affected adjacent area>
- [ ] <edge case explicitly handled>

## Risk
<one paragraph — what could break? What is rollback? What is the blast radius?>

## Related
- Closes #<issue>  (or "no issue" if internal)
- Depends on #<pr>  (only when actually blocked)
```

**Required:**
- `Summary` — never skip. A PR without a summary forces the reviewer to reconstruct intent from the diff.
- `Risk` — even "no risk: pure docs change" is better than absence. Forces the author to think about blast radius.

**Skip if irrelevant:**
- `Test plan` — for pure docs / dead-code removal.
- `Related` — for one-off internal cleanup.

**Anti-patterns:**
- "Update X" / "fixes" / "WIP" as title.
- Body that paraphrases the diff line-by-line.
- "See commits for details" — reviewer shouldn't have to context-switch.
- Marketing copy ("dramatically improves UX") without evidence.
- Mixed concerns ("refactor + new feature + version bump") — split the PR.
</pr-description-template>
