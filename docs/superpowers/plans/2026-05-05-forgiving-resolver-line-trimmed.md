# Forgiving Resolver — LineTrimmed Strategy

> **For agentic workers:** Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **User preference (overrides skill default):** implement all functionality first, run a single test-audit pass at the end. No TDD per task. Source: `memory/user_prefs.md`.

**Goal:** When `internal/resolver/external_patches.go::resolveSearchReplace` cannot find an exact match for an LLM-supplied `search` block, retry once with **trailing-whitespace** of every line stripped on both sides. If that yields a unique match, build the op against the *original* file content (preserving its real bytes) so the applier's strict re-check still passes.

This is sub-project **B** from the core-parity roadmap (`memory/core_parity_roadmap.md`). Closes the most common cause of LLM patch failures — trailing spaces and CRLF/LF mismatches the model couldn't see — without weakening any safety invariant: `Conditions.FileHash` still hard-fails real concurrent edits, and `Expected` in the resulting op is verbatim from the file.

**Non-goals:** Indentation-flexible matching (leading whitespace deltas, tab↔space normalization, indent re-balancing of `replace`). That's substantially harder (mixed indent, edge cases, indent-delta math on the replacement) and gets its own plan **Bʹ** later, after we measure how far LineTrimmed alone gets us.

**Architecture:**
- A new internal helper `lineTrimmedFind(haystack, needle string) (start, end, matches int)` lives in `internal/resolver/external_patches.go` next to the existing `findUnique`.
- It builds a normalized view of `haystack` and `needle` in which each line has its **trailing** whitespace (`[ \t]+$`) stripped, plus a parallel `[]int` offset map from normalized-byte-index → original-byte-index. CRLF/LF differences are also collapsed (LF in normalized view).
- After `findUnique` returns 0 matches, `resolveSearchReplace` calls `lineTrimmedFind`. On unique match, it maps the normalized `start`/`end` back to original offsets, sets `Expected = string(before[origStart:origEnd])` (the *real* bytes from the file at that position), keeps `Replacement = p.Replace` and `Conditions` identical to the strict path. On 0 or >1 matches, the original `StaleContent`/`AmbiguousMatch` errors are returned exactly as before.
- Applier is untouched. Its existing positional-fuzzy (`AllowFuzzy`/`FuzzyWindow`) keeps its role: post-write-time race recovery.

**Tech Stack:** Go 1.22+. No new packages, no new types in `internal/ops`.

---

## Invariants this must preserve

These are the safety properties that already hold for the strict path; the new path must not weaken any of them.

1. The op's `Range.Start/End` point into the **original** file content — not into the normalized view. (The applier reads the file fresh and computes line/col against that read.)
2. The op's `Expected` is exactly `original[origStart:origEnd]` — byte-for-byte. If file is later re-read by the applier and these bytes don't match, applier returns `StaleContent` like before.
3. `Conditions.FileHash` remains `p.FileHash`. If the LLM's hash doesn't match what's on disk at apply time, applier still rejects.
4. `Replacement` is exactly `p.Replace`. We do **not** transform indentation (that's plan Bʹ).
5. The forgiving path never widens behaviour for ambiguous needles: 2+ unique line-trimmed matches → `AmbiguousMatch` (fail closed).

---

## File Structure

**Modify:**
- `internal/resolver/external_patches.go` — add `lineTrimmedFind`, modify `resolveSearchReplace` to call it before hard-failing.

**Modify (test audit, end of plan):**
- `internal/resolver/external_patches_test.go` — keep existing strict-path tests as-is (regression coverage), add new cases for the forgiving path.

**Do not modify:**
- `internal/applier/*` — applier's strict re-check + positional fuzzy stay exactly as today.
- `internal/ops/*` — no new ops, no new fields on `Conditions`.
- LLM prompts, tools registry, protocol versions — no wire changes.

---

## Task 1: Add `lineTrimmedFind` helper

**Files:**
- Modify: `internal/resolver/external_patches.go`

The helper does three things:
1. Build a normalized version of `haystack` and `needle` where each line's trailing `[ \t]+` is removed and `\r\n` is collapsed to `\n`.
2. While building, record an offset map `origIdxOf[i]` = original-byte-index of the byte that ended up at normalized index `i`. The map's length is `len(normalizedHaystack)+1` so `origIdxOf[len(normalized)]` returns the end-of-haystack offset.
3. Search for `normalizedNeedle` in `normalizedHaystack` using the same "count up to 2 occurrences" pattern as `findUnique`. Return the *original* offsets via the map.

- [ ] **Step 1.1: Add `lineTrimmedFind` next to `findUnique`**

Insert this function after `findUnique` (currently around line 261 in `external_patches.go`):

```go
// lineTrimmedFind is a forgiving variant of findUnique that ignores trailing
// whitespace on every line and CRLF/LF differences. It is called only when
// strict findUnique returned 0 matches.
//
// Returns offsets into the *original* haystack so the resulting op stays
// byte-accurate against the on-disk file. Counts up to 2 occurrences:
// caller treats matches==0 as StaleContent and matches>1 as AmbiguousMatch,
// exactly like findUnique.
func lineTrimmedFind(haystack, needle string) (start, end, matches int) {
	if needle == "" {
		return 0, 0, 0
	}
	normHay, hayMap := normalizeTrailingWS(haystack)
	normNeedle, _ := normalizeTrailingWS(needle)
	if normNeedle == "" {
		return 0, 0, 0
	}

	idx := 0
	for {
		j := strings.Index(normHay[idx:], normNeedle)
		if j < 0 {
			break
		}
		matches++
		if matches == 1 {
			start = hayMap[idx+j]
			end = hayMap[idx+j+len(normNeedle)]
		}
		if matches > 1 {
			return start, end, matches
		}
		idx = idx + j + max(1, len(normNeedle))
	}
	return start, end, matches
}

// normalizeTrailingWS returns:
//   - normalized: a copy of s with `\r\n` collapsed to `\n` and trailing
//     `[ \t]+` removed from each line.
//   - origIdx: a slice of length len(normalized)+1 where origIdx[i] is the
//     byte offset in s that the normalized byte at position i was copied
//     from. origIdx[len(normalized)] == len(s) so callers can compute
//     end-of-match offsets cleanly.
func normalizeTrailingWS(s string) (normalized string, origIdx []int) {
	var b strings.Builder
	b.Grow(len(s))
	origIdx = make([]int, 0, len(s)+1)

	// Process line-by-line. We scan the input keeping the original index of
	// every byte we *emit*. Trailing whitespace on a line is dropped (its
	// indices are not recorded). CRLF is collapsed by skipping the '\r' and
	// emitting only the '\n' (record the index of the '\n' itself).
	i := 0
	for i < len(s) {
		// Find end of current line (index of '\n', or len(s) if no '\n').
		nl := strings.IndexByte(s[i:], '\n')
		var lineEnd int
		hasNL := false
		if nl < 0 {
			lineEnd = len(s)
		} else {
			lineEnd = i + nl
			hasNL = true
		}

		// Compute trim point: walk backwards over space/tab from lineEnd.
		// If the line ends with "\r\n", lineEnd points at the '\n', and we
		// must also skip the '\r' before trimming whitespace.
		contentEnd := lineEnd
		if hasNL && contentEnd > i && s[contentEnd-1] == '\r' {
			contentEnd-- // exclude the '\r' (collapse CRLF → LF)
		}
		for contentEnd > i {
			c := s[contentEnd-1]
			if c == ' ' || c == '\t' {
				contentEnd--
				continue
			}
			break
		}

		// Emit the trimmed line content with its original indices.
		for k := i; k < contentEnd; k++ {
			b.WriteByte(s[k])
			origIdx = append(origIdx, k)
		}
		// Emit the '\n' if there was one.
		if hasNL {
			b.WriteByte('\n')
			origIdx = append(origIdx, lineEnd) // record index of the '\n' in s
			i = lineEnd + 1
		} else {
			i = lineEnd
		}
	}

	// Sentinel for end-of-string offset lookups.
	origIdx = append(origIdx, len(s))
	return b.String(), origIdx
}
```

- [ ] **Step 1.2: Build to verify**

```bash
go build ./...
```

Expected: clean build. (`max` already exists in this file from the strict path.)

- [ ] **Step 1.3: Commit**

```bash
git add internal/resolver/external_patches.go
git commit -m "feat(resolver): add lineTrimmedFind helper (no callers yet)"
```

---

## Task 2: Wire forgiving path into `resolveSearchReplace`

**Files:**
- Modify: `internal/resolver/external_patches.go`

The current strict logic returns `StaleContent` immediately on `matches==0`. We change *only* that branch to first attempt `lineTrimmedFind`, then fall through to the existing error if still 0 matches. The `matches>1` branch is unchanged.

- [ ] **Step 2.1: Replace the `matches==0` branch**

Find this block in `resolveSearchReplace`:

```go
	start, end, matches := findUnique(string(before), p.Search)
	if matches == 0 {
		return ops.ReplaceRangeOp{}, protocol.NewError(protocol.StaleContent, "search block not found", map[string]any{
			"path":     p.Path,
			"search":   preview(p.Search, 200),
			"fileHash": store.ComputeSHA256(before),
		})
	}
	if matches > 1 {
```

Replace with:

```go
	start, end, matches := findUnique(string(before), p.Search)
	if matches == 0 {
		// Forgiving fallback: retry ignoring trailing whitespace on every
		// line and CRLF/LF differences. The op we build still references
		// the *original* bytes via Expected, so the applier's strict
		// re-check semantics are unchanged.
		ltStart, ltEnd, ltMatches := lineTrimmedFind(string(before), p.Search)
		switch ltMatches {
		case 0:
			return ops.ReplaceRangeOp{}, protocol.NewError(protocol.StaleContent, "search block not found", map[string]any{
				"path":     p.Path,
				"search":   preview(p.Search, 200),
				"fileHash": store.ComputeSHA256(before),
			})
		case 1:
			start, end = ltStart, ltEnd
			// Fall through to position computation below using the
			// recovered offsets. Expected is overwritten further down.
		default: // >1
			return ops.ReplaceRangeOp{}, protocol.NewError(protocol.AmbiguousMatch, "search block is ambiguous (line-trimmed)", map[string]any{
				"path":    p.Path,
				"matches": ltMatches,
				"search":  preview(p.Search, 200),
			})
		}
	}
	if matches > 1 {
```

- [ ] **Step 2.2: Override `Expected` with verbatim file bytes**

The strict path uses `Expected: p.Search`. For the forgiving path, that string differs from what's actually at `before[start:end]` (by the very trailing whitespace we ignored), and the applier would then reject. We must use the original substring.

Find the return statement at the bottom of `resolveSearchReplace`:

```go
	return ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: p.Path,
		Range: ops.Range{
			Start: startPos,
			End:   endPos,
		},
		Expected:    p.Search,
		Replacement: p.Replace,
		Conditions: ops.Conditions{
			FileHash:    p.FileHash,
			AllowFuzzy:  true,
			FuzzyWindow: 2,
		},
	}, nil
```

Change `Expected: p.Search` to `Expected: string(before[start:end])`. This is correct in **both** paths: in the strict path `before[start:end] == p.Search` already, so behaviour is unchanged; in the forgiving path it's the verbatim file bytes the applier will see.

```go
	return ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: p.Path,
		Range: ops.Range{
			Start: startPos,
			End:   endPos,
		},
		Expected:    string(before[start:end]),
		Replacement: p.Replace,
		Conditions: ops.Conditions{
			FileHash:    p.FileHash,
			AllowFuzzy:  true,
			FuzzyWindow: 2,
		},
	}, nil
```

- [ ] **Step 2.3: Build**

```bash
go build ./...
```

- [ ] **Step 2.4: Commit**

```bash
git add internal/resolver/external_patches.go
git commit -m "feat(resolver): forgiving line-trimmed retry before StaleContent

When the LLM's search block fails strict equality (typically because of
trailing whitespace differences or CRLF/LF on the wrong side), retry
once with trailing whitespace stripped per-line and CRLF collapsed.
On unique match, build the op against the original file bytes so the
applier's strict re-check still works. file_hash, fuzzy window, and
all other safety conditions are unchanged."
```

---

## Task 3: Test audit pass

**Files:**
- Modify: `internal/resolver/external_patches_test.go`

Single dedicated test pass per `user_prefs.md`. Two things to do:

1. Re-run the existing test suite to confirm strict-path behaviour didn't regress.
2. Add forgiving-path coverage: hits, misses, ambiguity, hash safety, replace-bytes preserved.

- [ ] **Step 3.1: Run existing tests, ensure no regression**

```bash
go test ./internal/resolver -count=1 -v
```

Expected: all existing tests still PASS. (They only exercised `Expected: p.Search`, which now becomes `Expected: before[start:end]` — but in the strict path those two strings are byte-equal, so assertions hold.)

- [ ] **Step 3.2: Add forgiving-path test cases**

Append the following to `internal/resolver/external_patches_test.go`. (If the file already has helpers like `mkTempFile` / `newTestPatch` reuse them; otherwise inline a minimal helper.)

```go
func TestResolveSearchReplace_Forgiving_TrailingWhitespace(t *testing.T) {
	// File on disk has "x = 1   " (trailing spaces). LLM passes "x = 1"
	// without the spaces. Strict findUnique returns 0; lineTrimmedFind
	// finds it once. Expected in the op must be "x = 1   " (verbatim).
	dir := t.TempDir()
	relPath := "a.go"
	full := filepath.Join(dir, relPath)
	original := "package a\n\nx = 1   \nfunc f() {}\n"
	if err := os.WriteFile(full, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	patch := externalpatch.Patch{
		Type:    externalpatch.TypeFileSearchReplace,
		Path:    relPath,
		Search:  "x = 1",            // no trailing spaces
		Replace: "x = 2",
	}
	got, err := ResolveExternalPatches(dir, []externalpatch.Patch{patch})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0].ReplaceRange == nil {
		t.Fatalf("expected 1 replace_range op, got %#v", got)
	}
	op := got[0].ReplaceRange
	if op.Expected != "x = 1   " {
		t.Errorf("Expected: want %q (verbatim file bytes), got %q", "x = 1   ", op.Expected)
	}
	if op.Replacement != "x = 2" {
		t.Errorf("Replacement: want %q, got %q", "x = 2", op.Replacement)
	}
}

func TestResolveSearchReplace_Forgiving_CRLF(t *testing.T) {
	dir := t.TempDir()
	relPath := "b.go"
	full := filepath.Join(dir, relPath)
	original := "package b\r\nx := 1\r\n"
	if err := os.WriteFile(full, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// LLM uses LF-only search.
	patch := externalpatch.Patch{
		Type:    externalpatch.TypeFileSearchReplace,
		Path:    relPath,
		Search:  "x := 1\n",
		Replace: "x := 2\n",
	}
	got, err := ResolveExternalPatches(dir, []externalpatch.Patch{patch})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	op := got[0].ReplaceRange
	if op.Expected != "x := 1\r\n" {
		t.Errorf("Expected: want CRLF-preserved %q, got %q", "x := 1\r\n", op.Expected)
	}
}

func TestResolveSearchReplace_Forgiving_AmbiguousFails(t *testing.T) {
	dir := t.TempDir()
	relPath := "c.go"
	full := filepath.Join(dir, relPath)
	// Same line twice, both with different trailing whitespace.
	original := "y = 1   \ny = 1\t\n"
	if err := os.WriteFile(full, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	patch := externalpatch.Patch{
		Type:    externalpatch.TypeFileSearchReplace,
		Path:    relPath,
		Search:  "y = 1",
		Replace: "y = 2",
	}
	_, err := ResolveExternalPatches(dir, []externalpatch.Patch{patch})
	if err == nil {
		t.Fatal("expected AmbiguousMatch, got nil")
	}
	var pErr *protocol.Error
	if !errors.As(err, &pErr) || pErr.Code != protocol.AmbiguousMatch {
		t.Fatalf("expected AmbiguousMatch protocol error, got %v", err)
	}
}

func TestResolveSearchReplace_Forgiving_StillStaleWhenUnrecoverable(t *testing.T) {
	dir := t.TempDir()
	relPath := "d.go"
	full := filepath.Join(dir, relPath)
	original := "package d\nz = 1\n"
	if err := os.WriteFile(full, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	patch := externalpatch.Patch{
		Type:    externalpatch.TypeFileSearchReplace,
		Path:    relPath,
		Search:  "totally absent",
		Replace: "...",
	}
	_, err := ResolveExternalPatches(dir, []externalpatch.Patch{patch})
	var pErr *protocol.Error
	if !errors.As(err, &pErr) || pErr.Code != protocol.StaleContent {
		t.Fatalf("expected StaleContent, got %v", err)
	}
}

func TestLineTrimmedFind_Unit(t *testing.T) {
	cases := []struct {
		name     string
		hay      string
		needle   string
		wantS    int
		wantE    int
		wantHits int
	}{
		{"trailing spaces in hay", "foo   \nbar\n", "foo\n", 0, 7, 1},
		{"crlf in hay only",       "foo\r\nbar\n", "foo\nbar", 0, 8, 1},
		{"empty needle",           "anything", "", 0, 0, 0},
		{"absent",                 "abc\n", "xyz", 0, 0, 0},
		{"two matches",            "x\t\nx \n", "x\n", 0, 2, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, e, hits := lineTrimmedFind(c.hay, c.needle)
			if hits != c.wantHits {
				t.Fatalf("hits: want %d, got %d", c.wantHits, hits)
			}
			if hits == 1 {
				if s != c.wantS || e != c.wantE {
					t.Errorf("offsets: want [%d,%d), got [%d,%d). substring=%q", c.wantS, c.wantE, s, e, c.hay[s:e])
				}
			}
		})
	}
}
```

Required imports (add only those not already present): `os`, `path/filepath`, `errors`, `github.com/orchestra/orchestra/internal/externalpatch`, `github.com/orchestra/orchestra/internal/protocol`.

- [ ] **Step 3.3: Run resolver tests**

```bash
go test ./internal/resolver -count=1 -v
```

Expected: all old tests + 5 new tests PASS.

- [ ] **Step 3.4: Run full test suite + race**

```bash
go vet ./...
go test ./... -count=1
go test -race ./internal/resolver ./internal/applier ./internal/agent -count=1
```

Expected: all green. Resolver change is local; applier/agent run as additional safety nets because they consume `Expected`/`Range` from resolver output.

- [ ] **Step 3.5: Commit**

```bash
git add internal/resolver/external_patches_test.go
git commit -m "test(resolver): cover line-trimmed forgiving path

Five new cases: trailing-whitespace recovery, CRLF/LF cross-mismatch,
ambiguity preserved as AmbiguousMatch, true-absence still surfaces as
StaleContent, plus a focused unit test on lineTrimmedFind for empty
needles, two-match short-circuit, and offset accuracy."
```

---

## Task 4: Live LLM smoke

**Files:** none — manual verification only.

This is a regression check that the change didn't break the live edit loop and (as a side benefit) that the model occasionally exercises the forgiving path. We do *not* contrive a synthetic failure case — real LLM behaviour is varied enough that the forgiving path will trigger naturally over time.

- [ ] **Step 4.1: Build the binary**

```bash
go build -o orchestra.exe ./cmd/orchestra
```

- [ ] **Step 4.2: Smoke an apply that requires a real edit**

Pick a tiny harmless edit. Example:

```bash
./orchestra.exe apply --debug "Добавь комментарий '// touched by smoke test' над декларацией ListTools в internal/tools/registry.go"
```

Expected behaviour: agent reads `internal/tools/registry.go`, returns a `file.search_replace` patch, resolver succeeds (strict or forgiving), CLI prints a diff. Whether strict or forgiving was used, the resulting `Expected` in `.orchestra/plan.json` must equal the actual bytes of the file at the matched range.

- [ ] **Step 4.3: Inspect plan.json**

```bash
cat .orchestra/plan.json | head -60
```

Verify the `expected` field on the op contains the exact substring you'd find at the same line range in the file (open the file at the printed line range and eyeball it). If they differ — that's a bug in this plan, stop and ask.

- [ ] **Step 4.4: Apply for real**

```bash
./orchestra.exe apply --apply --debug "<same query>"
```

Expected: the file is modified, `*.orchestra.bak` is created, no errors. Manually revert the change after confirming (`git checkout -- internal/tools/registry.go`).

---

## Self-review

- **Spec coverage:** Two functional changes — `lineTrimmedFind` (Task 1) and the wiring in `resolveSearchReplace` (Task 2 incl. `Expected = before[start:end]`). Both are covered. Tests in Task 3 cover all six invariants from the *Invariants* section: hash safety (StillStaleWhenUnrecoverable + applier path unchanged), `Expected` byte-accuracy (TrailingWhitespace + CRLF), no replacement transformation (asserted in TrailingWhitespace), ambiguity fail-closed (AmbiguousFails), strict path unchanged (existing tests).
- **Placeholder scan:** No "TBD" / "implement later" / "similar to Task N". Every code step shows the diff in full.
- **Type/method consistency:** `lineTrimmedFind` and `normalizeTrailingWS` defined in Task 1; consumed verbatim in Task 2 and tested with the same signatures in Task 3. `Expected: string(before[start:end])` defined in Task 2 and asserted in Task 3.

## Rollback

If the live smoke (Task 4) shows the forgiving path corrupts a real edit, revert with:

```bash
git revert <commit-from-Task-2>
```

The strict path returns. Tests in Task 3 keep their value as regression coverage even after revert (they pass against the strict-only baseline because the forgiving cases would simply turn into `StaleContent` again).
