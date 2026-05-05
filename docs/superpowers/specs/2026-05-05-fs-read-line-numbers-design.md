# Design: fs.read line-number prefix (sub-project C)

**Date:** 2026-05-05  
**Status:** approved  
**ToolsVersion bump:** 3 → 4

## Context

`docs/commands-and-modes.md §3.4` identifies line-number prefixes in read output
as a high-value, low-cost borrowing from OpenCode. OpenCode returns `1: foo\n2: bar\n`
from its read tool; Orchestra currently returns raw text. The prefix helps the LLM
reference exact lines when constructing the next `fs.edit`, reducing StaleContent
retries.

## Decision

**Option A — always-on in `content` field.** Every `fs.read` response has line
numbers baked into `content`. No opt-in parameter needed; LLM gets the benefit
by default.

Rejected alternatives:
- **B (opt-in `line_numbers` parameter):** benefit only materialises when LLM
  remembers to pass the flag — not reliable with qwen3.6-27b.
- **C (separate `numbered_content` field):** doubles token usage on every read.

## What changes

### `internal/tools/tools.go` — `FSRead()`

After `readFileWithHash` returns `content`, apply `addLineNumbers(content)` before
storing it in `FSReadResponse.Content`. Hash and all other fields are computed from
the raw bytes — numbering is presentation-only.

#### Format rules

- Prefix every line with `N: ` (1-based).
- Pad the number to the width of the last line number so columns align:
  - ≤ 9 lines → `"1: "` (no padding)
  - 10–99 lines → `" 1: "` / `"10: "`
  - 100–999 lines → `"  1: "` / `"100: "`
- Trailing newline: if the original content ends with `\n`, the last empty
  "line" is **not** numbered (same behaviour as editors).
- Truncation: if `truncated: true`, only the lines that fit are numbered;
  the `truncated` flag already signals incompleteness.

#### Helper function

```go
// addLineNumbers prefixes each line with its 1-based line number.
// Width is padded to the number of digits in the last line number.
func addLineNumbers(content string) string
```

Lives in `internal/tools/fs.go` alongside `readFileWithHash`.

### `internal/tools/registry.go` — tool descriptions

**`toolFSRead` description:**
> "Читает файл в workspace и возвращает content+sha256 (file_hash). Содержимое
> возвращается с префиксами номеров строк (`1: строка`) — только для ориентации.
> Префиксы не входят в файл: не включай их в поле `search` при редактировании."

**`toolFSEdit` description** — append one sentence:
> "Поле `search` должно содержать точный текст файла без префиксов номеров строк."

### `internal/protocol/version.go`

`ToolsVersion` 3 → 4.

### `docs/PROTOCOL.md`

Update ToolsVersion entry and note the semantic change to `fs.read` content.

## What does NOT change

| Item | Reason |
|---|---|
| `FSReadRequest` / `FSReadResponse` structs | Same fields; only value of `content` changes |
| `sha256` / `file_hash` | Computed from raw bytes before numbering |
| Resolver (`internal/resolver`) | Reads files via `projectfs.ReadFile` directly |
| Applier, ops | No dependency on FSReadResponse |
| `fs.edit` logic | No change; description update is sufficient |

## Risk: LLM includes `N: ` in search string

**Likelihood:** low — description in both `fs.read` and `fs.edit` explicitly
prohibits it.  
**Impact if it happens:** `StaleContent` error → agent retries, compact hint
added to history. The forgiving resolver (LineTrimmed, already shipped) does not
help here since the prefix is not whitespace — but the error message is clear
enough for the LLM to self-correct within the retry budget.

## Testing

### Updates to existing tests

Existing `FSRead` tests in `internal/tools/` assert `Content` equals raw text.
Update expected values to numbered format.

### New test: `TestAddLineNumbers`

Table-driven in `internal/tools/fs_read_test.go` (new file, consistent with `fs_glob_test.go` / `fs_edit_test.go`):

| Case | Input | Expected |
|---|---|---|
| single line | `"hello"` | `"1: hello"` |
| two lines | `"a\nb"` | `"1: a\n2: b"` |
| trailing newline | `"a\nb\n"` | `"1: a\n2: b\n"` |
| 10+ lines | 10-line string | numbers padded to 2 digits |
| 100+ lines | 100-line string | numbers padded to 3 digits |
| empty | `""` | `""` |

### New test: `TestFSReadHashUnchanged`

Call `FSRead` on a real temp file; assert that `SHA256` / `FileHash` match
`sha256File(path)` of the original bytes (not the numbered content).
