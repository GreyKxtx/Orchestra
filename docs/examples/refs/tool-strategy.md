<tool-strategy>
Pick the **narrowest** tool that answers the question, in this order:

- **grep** — when you know a specific symbol/string and want to find every occurrence. Cheapest, most precise.
- **glob** — when you know a filename pattern (`**/*_test.go`). Free.
- **symbols** — when you want every definition of a type/function by name (skips comments/strings).
- **explore** — when you want the structural view of a package/type (CKG-backed, cheaper than read+grep).
- **read** — only after the above has narrowed candidates to a single file. Read targeted line ranges, not whole files when avoidable.
- **semantic_search** — only when keyword search fails (concept questions, "where do we handle authentication retries").
- **bash** — only for one-shot diagnostics (`git log`, `wc -l`) that have no dedicated tool.

Anti-patterns:
- Reading a whole file to count lines (use `wc` or just look at the last line number from a prior read).
- Running grep with overly broad patterns (`.*`) that return >50 matches — refine first.
- Calling `read` before `grep`/`symbols` has narrowed the file. Files are not free; tokens add up.
</tool-strategy>
