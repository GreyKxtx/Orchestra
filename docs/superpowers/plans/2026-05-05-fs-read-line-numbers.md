# fs.read line-number prefix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `1: ` line-number prefixes to every `fs.read` response so the LLM can reference exact line numbers when constructing edits, reducing StaleContent retries.

**Architecture:** A pure presentation transform — `addLineNumbers(content string) string` is applied inside `FSRead()` after `readFileWithHash` returns the raw bytes. The `sha256`/`file_hash` fields are computed from the raw bytes, not the numbered content. No schema changes; only the value of the `content` field changes.

**Tech Stack:** Go stdlib only (`strings`, `fmt`).

---

## File map

| File | Change |
|---|---|
| `internal/tools/fs.go` | Add `addLineNumbers()` helper |
| `internal/tools/tools.go` | Call `addLineNumbers()` in `FSRead()` |
| `internal/tools/registry.go` | Update descriptions of `toolFSRead` and `toolFSEdit` |
| `internal/tools/fs_read_test.go` | New file: `TestAddLineNumbers`, `TestFSReadLineNumbers`, `TestFSReadHashUnchanged` |
| `internal/protocol/version.go` | Bump `ToolsVersion` 3 → 4 |
| `docs/PROTOCOL.md` | Update version table + add v4 history entry |

---

## Task 1: `addLineNumbers` helper — tests first

**Files:**
- Create: `internal/tools/fs_read_test.go`
- Modify: `internal/tools/fs.go`

- [ ] **Step 1: Create the test file with a failing test**

Create `internal/tools/fs_read_test.go`:

```go
package tools

import (
	"strings"
	"testing"
)

func TestAddLineNumbers(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "single line no newline",
			input: "hello",
			want:  "1: hello",
		},
		{
			name:  "single line with newline",
			input: "hello\n",
			want:  "1: hello\n",
		},
		{
			name:  "two lines no trailing newline",
			input: "a\nb",
			want:  "1: a\n2: b",
		},
		{
			name:  "two lines with trailing newline",
			input: "a\nb\n",
			want:  "1: a\n2: b\n",
		},
		{
			name:  "padding at 10 lines",
			input: strings.Repeat("x\n", 10),
			want: " 1: x\n 2: x\n 3: x\n 4: x\n 5: x\n" +
				" 6: x\n 7: x\n 8: x\n 9: x\n10: x\n",
		},
		{
			name: "padding at 100 lines",
			// build expected: "  1: x\n  2: x\n ... 100: x\n"
			input: strings.Repeat("x\n", 100),
			want: func() string {
				var b strings.Builder
				for i := 1; i <= 100; i++ {
					b.WriteString(fmt.Sprintf("%3d: x\n", i))
				}
				return b.String()
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := addLineNumbers(tc.input)
			if got != tc.want {
				t.Errorf("addLineNumbers(%q)\ngot:  %q\nwant: %q", tc.input, got, tc.want)
			}
		})
	}
}
```

Note: the file uses `fmt.Sprintf` in one test case — add `"fmt"` to the import block.

- [ ] **Step 2: Run the test to confirm it fails**

```
go test ./internal/tools/ -run TestAddLineNumbers -v
```

Expected: `FAIL — undefined: addLineNumbers`

- [ ] **Step 3: Implement `addLineNumbers` in `internal/tools/fs.go`**

Add the following function at the bottom of `internal/tools/fs.go` (after the existing `readFileWithHash` function):

```go
// addLineNumbers prefixes each line with its 1-based line number.
// Width is padded to the digit count of the last line number so columns align.
// A trailing newline in content is preserved; the empty "line" after it is not numbered.
func addLineNumbers(content string) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	hasTrailing := strings.HasSuffix(content, "\n")

	// Lines to number: exclude the empty string produced by a trailing \n.
	numerable := lines
	if hasTrailing {
		numerable = lines[:len(lines)-1]
	}
	if len(numerable) == 0 {
		return content
	}

	width := len(fmt.Sprintf("%d", len(numerable)))
	format := fmt.Sprintf("%%%dd: ", width)

	var b strings.Builder
	b.Grow(len(content) + len(numerable)*(width+2))
	for i, line := range numerable {
		fmt.Fprintf(&b, format, i+1)
		b.WriteString(line)
		b.WriteByte('\n')
	}

	result := b.String()
	if !hasTrailing {
		result = result[:len(result)-1] // strip the extra \n we added for the last line
	}
	return result
}
```

Add `"fmt"` to the imports in `fs.go` if not already present.

- [ ] **Step 4: Run the test to confirm it passes**

```
go test ./internal/tools/ -run TestAddLineNumbers -v
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```
git add internal/tools/fs.go internal/tools/fs_read_test.go
git commit -m "feat(tools): add addLineNumbers helper for fs.read"
```

---

## Task 2: Wire `addLineNumbers` into `FSRead` + hash invariant test

**Files:**
- Modify: `internal/tools/tools.go` (lines 238–251, the `FSRead` return)
- Modify: `internal/tools/fs_read_test.go` (add two more tests)

- [ ] **Step 1: Add two failing tests to `fs_read_test.go`**

Append to `internal/tools/fs_read_test.go`:

```go
func newReadRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestFSReadLineNumbers(t *testing.T) {
	r, root := newReadRunner(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSRead(context.Background(), FSReadRequest{Path: "f.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}
	want := "1: alpha\n2: beta\n"
	if resp.Content != want {
		t.Errorf("Content:\ngot:  %q\nwant: %q", resp.Content, want)
	}
}

func TestFSReadHashUnchanged(t *testing.T) {
	r, root := newReadRunner(t)
	raw := []byte("line one\nline two\n")
	if err := os.WriteFile(filepath.Join(root, "h.txt"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSRead(context.Background(), FSReadRequest{Path: "h.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}

	// Hash must match the raw file bytes, not the numbered content.
	wantHash, err := sha256File(filepath.Join(root, "h.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.SHA256 != wantHash {
		t.Errorf("SHA256 mismatch:\ngot:  %s\nwant: %s", resp.SHA256, wantHash)
	}
	if resp.FileHash != wantHash {
		t.Errorf("FileHash mismatch:\ngot:  %s\nwant: %s", resp.FileHash, wantHash)
	}
}
```

Add missing imports to `fs_read_test.go`:
```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run the new tests to confirm they fail**

```
go test ./internal/tools/ -run "TestFSReadLineNumbers|TestFSReadHashUnchanged" -v
```

Expected: `TestFSReadLineNumbers FAIL` — content has no line numbers yet. `TestFSReadHashUnchanged` should PASS already (hash not touched yet), but run both to establish baseline.

- [ ] **Step 3: Apply `addLineNumbers` in `FSRead()`**

In `internal/tools/tools.go`, find the return statement of `FSRead` (around line 243) and change:

```go
	return &FSReadResponse{
		Path:      relSlash,
		Content:   content,
		SHA256:    hash,
		FileHash:  hash,
		MTimeUnix: mtimeUnix,
		Size:      size,
		Truncated: truncated,
	}, nil
```

to:

```go
	return &FSReadResponse{
		Path:      relSlash,
		Content:   addLineNumbers(content),
		SHA256:    hash,
		FileHash:  hash,
		MTimeUnix: mtimeUnix,
		Size:      size,
		Truncated: truncated,
	}, nil
```

- [ ] **Step 4: Run all tools tests**

```
go test ./internal/tools/ -v
```

Expected: all PASS. If any existing test fails because it checks raw `Content`, update its expected value to include `N: ` prefixes.

- [ ] **Step 5: Commit**

```
git add internal/tools/tools.go internal/tools/fs_read_test.go
git commit -m "feat(tools/fs.read): always-on line-number prefix in content"
```

---

## Task 3: Update tool descriptions

**Files:**
- Modify: `internal/tools/registry.go`

- [ ] **Step 1: Update `toolFSRead` description**

In `internal/tools/registry.go`, find `toolFSRead()` (around line 77). Change the `Description` field from:

```go
Description: "Читает файл в workspace и возвращает content+sha256 (file_hash).",
```

to:

```go
Description: "Читает файл в workspace и возвращает content+sha256 (file_hash). Содержимое возвращается с префиксами номеров строк (`1: строка`) — только для ориентации. Префиксы не входят в файл: не включай их в поле `search` при редактировании.",
```

- [ ] **Step 2: Update `toolFSEdit` description**

In the same file, find `toolFSEdit()` (around line 139). Change the `Description` field from:

```go
Description: "Точечная замена в файле (search → replace). Строка поиска должна быть уникальна в файле. При неоднозначности — AmbiguousMatch; если не найдена — StaleContent. file_hash рекомендуется для защиты от гонок.",
```

to:

```go
Description: "Точечная замена в файле (search → replace). Строка поиска должна быть уникальна в файле. При неоднозначности — AmbiguousMatch; если не найдена — StaleContent. file_hash рекомендуется для защиты от гонок. Поле `search` должно содержать точный текст файла без префиксов номеров строк.",
```

- [ ] **Step 3: Run vet + tests**

```
go vet ./...
go test ./internal/tools/ -v
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```
git add internal/tools/registry.go
git commit -m "docs(tools): update fs.read and fs.edit descriptions for line-number prefix"
```

---

## Task 4: Bump ToolsVersion + update PROTOCOL.md

**Files:**
- Modify: `internal/protocol/version.go`
- Modify: `docs/PROTOCOL.md`

- [ ] **Step 1: Bump `ToolsVersion` in `version.go`**

In `internal/protocol/version.go`, change:

```go
ToolsVersion = 3
```

to:

```go
ToolsVersion = 4
```

- [ ] **Step 2: Update `docs/PROTOCOL.md`**

Find the versions table near the top:

```markdown
- **`protocol.ToolsVersion`**: `3`
```

Change to:

```markdown
- **`protocol.ToolsVersion`**: `4`
```

Then find `### История ToolsVersion` and prepend a new entry:

```markdown
- **v4** (2026-05-05): `fs.read` content field now prefixed with 1-based line
  numbers (`1: line`, `2: line`). The `sha256`/`file_hash` fields are still
  computed from the raw file bytes. Do NOT include line-number prefixes in
  `fs.edit` search strings.
```

- [ ] **Step 3: Run full test suite**

```
go vet ./...
go test ./...
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```
git add internal/protocol/version.go docs/PROTOCOL.md
git commit -m "feat(protocol): bump ToolsVersion 3→4 for fs.read line-number prefix"
```

---

## Task 5: Final verification

- [ ] **Step 1: Run tests with race detector**

```
go test -race ./...
```

Expected: all PASS, no race conditions.

- [ ] **Step 2: Verify `go vet` is clean**

```
go vet ./...
```

Expected: no output.

- [ ] **Step 3: Manual spot-check (optional but recommended)**

Build and run a quick read:

```
go build -o orchestra ./cmd/orchestra
echo "func foo() {}" > /tmp/sample.go
./orchestra apply --plan-only "read /tmp/sample.go"
```

Check that the LLM receives `1: func foo() {}` in the tool result.
