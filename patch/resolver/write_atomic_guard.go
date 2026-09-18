package resolver

import (
	"strings"

	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/protocol"
)

// file.write_atomic replaces a whole file. That is exactly what it is for,
// and it is also how a model destroys work in one move. The fs.write tool
// resolves through here too, so this is the one place that sees every
// whole-file replacement.
//
// Three shapes showed up across evaluation runs against a local model, all
// after the model had already edited the file correctly. Each carried the
// right file_hash, so the staleness check passed and the content went to
// disk:
//
//   - content "" — a 260-byte source file replaced with nothing.
//   - content "// ... (file content with MaxRetries = 10) ..." — 309 lines
//     replaced with a note describing what should have been there.
//   - content "\tMaxRetries    = 10\n" — the same 309-line file replaced by
//     the one line the model had just changed inside it.
//
// None is a plausible intent, and none is recoverable from inside the turn:
// the model reports success and moves on. The hash condition cannot catch
// them, because the hash is right — it is the content that is wrong.
const (
	// elisionMaxLines is how short a replacement has to be before an
	// ellipsis in it reads as "I left the rest out" rather than as prose.
	elisionMaxLines = 3

	// shrinkFactor is how much smaller than the original a replacement must
	// be before it stops looking like a rewrite. A real rewrite can shrink a
	// file a lot; dropping to a twentieth of its size is a different thing.
	shrinkFactor = 20
)

// DestructiveWriteReason reports why replacing existing with replacement
// would destroy the file rather than rewrite it, or "" when the write is
// fine. Exported so the staging (dry-run) path can ask the same question
// without going through patch resolution.
func DestructiveWriteReason(existing, replacement string) string {
	if len(strings.TrimSpace(existing)) == 0 {
		return "" // nothing to lose
	}
	trimmed := strings.TrimSpace(replacement)

	if trimmed == "" {
		return "would replace it with an empty file, erasing " + itoa(len(existing)) +
			" bytes — to delete it use fs.delete, to change part of it use edit"
	}
	if len(existing) < len(trimmed)*shrinkFactor {
		return "" // not a drastic shrink; a rewrite this size is plausible
	}

	if strings.Contains(trimmed, "...") || strings.Contains(trimmed, "…") {
		if strings.Count(trimmed, "\n") < elisionMaxLines {
			return "the content looks like a placeholder for the real file (" +
				itoa(len(existing)) + " bytes replaced by " + itoa(len(replacement)) +
				") — a whole-file write must carry the complete new file; to change one part use edit"
		}
	}
	if strings.Contains(existing, trimmed) {
		return "the content is a verbatim fragment of the file it would replace (" +
			itoa(len(existing)) + " bytes replaced by " + itoa(len(replacement)) +
			") — a whole-file write must carry the complete new file; to change one part use edit"
	}
	// The fourth shape (27B, edit_in_large_file, 2026-09-18): after a correct
	// edit the model rewrote the 308-line file as the eight lines it had seen
	// in the edit result's changed_region — the top of the file with one line
	// (the package clause) dropped. Not verbatim, so the check above let it
	// through; 300 lines of work gone. A drastic shrink whose lines nearly all
	// already stand in the file is the same mistake with a typo in it.
	if known, total := linesAlreadyInFile(existing, trimmed); total >= fragmentMinLines &&
		known*100 >= total*fragmentKnownPct {
		return "the content is " + itoa(total) + " lines of which " + itoa(known) +
			" already stand in the file it would replace (" + itoa(len(existing)) +
			" bytes replaced by " + itoa(len(replacement)) +
			") — a whole-file write must carry the complete new file; to change one part use edit"
	}
	return ""
}

const (
	// fragmentMinLines is how many non-blank lines a replacement needs before
	// the line-overlap test means anything; two lines match by accident.
	fragmentMinLines = 3
	// fragmentKnownPct is the share of the replacement's lines that must
	// already be in the file for it to read as a fragment rather than a rewrite.
	fragmentKnownPct = 80
)

// linesAlreadyInFile counts the replacement's non-blank lines (trimmed) and
// how many of them occur, trimmed, as whole lines of existing.
func linesAlreadyInFile(existing, replacement string) (known, total int) {
	have := make(map[string]struct{})
	for _, l := range strings.Split(existing, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			have[t] = struct{}{}
		}
	}
	for _, l := range strings.Split(replacement, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		total++
		if _, ok := have[t]; ok {
			known++
		}
	}
	return known, total
}

// checkNotDestructive refuses a write_atomic that would erase a file's
// contents rather than rewrite them. mustNotExist patches create a new file
// and are never destructive.
func checkNotDestructive(projectRoot string, p patches.Patch, mustNotExist bool) error {
	if mustNotExist {
		return nil
	}
	existing, _, err := readFileOrEmpty(projectRoot, p.Path)
	if err != nil {
		// A file we cannot read is not one we can judge; the applier's own
		// conditions still guard the write.
		return nil
	}
	reason := DestructiveWriteReason(string(existing), p.Content)
	if reason == "" {
		return nil
	}
	return protocol.NewError(protocol.InvalidLLMOutput,
		"refusing to write "+p.Path+": "+reason,
		map[string]any{
			"path":           p.Path,
			"existing_bytes": len(existing),
			"content_bytes":  len(p.Content),
		})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
