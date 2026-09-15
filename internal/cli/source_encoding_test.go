package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Go sources had text saved as UTF-8, read back as cp1251 and saved again: an
// em dash became three characters. Most of it sat in comments, but not all —
// the model's "called too many times" refusal wrapped "STOP. The tool" in garbage,
// the LSP_ERRORS hint carried a broken dash into every edit with errors, and
// the TUI onboarding told users "LM Studio" followed by garbled Cyrillic. The
// sequences below are what the common punctuation turns into; real text
// never contains them.
func TestSourcesCarryNoDoubleEncodedText(t *testing.T) {
	signatures := map[string]string{
		"\xd0\xb2\xd0\x82":     "a dash, quote or ellipsis (U+2013..U+2026)",
		"\xd0\xb2\xe2\x80\xa0": "an arrow (U+2190..)",
		"\xd0\xb2\xe2\x80\xba": "a symbol (U+26xx)",
		"\xd0\xb2\xe2\x80\x93": "a block element (U+25xx)",
		"\xd0\x92\xc2\xab":     "«",
		"\xd0\x92\xc2\xbb":     "»",
		"\xd0\x93\xe2\x80\x94": "×",
	}
	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal", "ui/tui", "cmd", "protocol", "llm", "tests"} {
		base := filepath.Join(root, filepath.FromSlash(dir))
		if _, err := os.Stat(base); err != nil {
			continue
		}
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for i, line := range strings.Split(string(data), "\n") {
				for sig, what := range signatures {
					if strings.Contains(line, sig) {
						t.Errorf("%s:%d: double-encoded %s: %s", path, i+1, what, strings.TrimSpace(line))
					}
				}
			}
			return nil
		})
	}
}
