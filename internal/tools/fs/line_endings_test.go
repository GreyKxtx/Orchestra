package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// A model writes "\n". In a project whose files are CRLF that left every new
// file the odd one out — the editor flagged it, and a diff of a rewritten file
// showed every line changed.
func TestFSWrite_FollowsTheProjectsLineEndings(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "disk"
		if staged {
			name = "staged turn"
		}
		t.Run(name, func(t *testing.T) {
			r, root := newWriteRunner(t)
			mustWrite(t, filepath.Join(root, "index.html"), "<html>\r\n<body>\r\n</body>\r\n</html>\r\n")
			mustWrite(t, filepath.Join(root, "style.css"), "body {\r\n  margin: 0;\r\n}\r\n")
			mustWrite(t, filepath.Join(root, "lf", "a.txt"), "one\ntwo\n")
			ctx := context.Background()
			if staged {
				turn := r.NewTurn(tools.TurnOptions{DryRun: true, Apply: true})
				defer turn.Close()
				ctx = tools.WithTurn(ctx, turn)
			}

			// A new file beside CRLF files.
			if _, err := r.FSWrite(ctx, tools.FSWriteRequest{Path: "game.js", Content: "let a = 1;\nlet b = 2;\n"}); err != nil {
				t.Fatalf("write game.js: %v", err)
			}
			// A new file in a directory of its own, under a CRLF root.
			if _, err := r.FSWrite(ctx, tools.FSWriteRequest{Path: "js/util.js", Content: "let c = 3;\nlet d = 4;\n"}); err != nil {
				t.Fatalf("write js/util.js: %v", err)
			}
			// A new file beside LF files stays LF.
			if _, err := r.FSWrite(ctx, tools.FSWriteRequest{Path: "lf/b.txt", Content: "three\nfour\n"}); err != nil {
				t.Fatalf("write lf/b.txt: %v", err)
			}
			// A rewrite of a CRLF file keeps CRLF.
			read, err := r.FSRead(ctx, tools.FSReadRequest{Path: "style.css"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.FSWrite(ctx, tools.FSWriteRequest{Path: "style.css", Content: "body {\n  margin: 1px;\n}\n", FileHash: read.FileHash}); err != nil {
				t.Fatalf("rewrite style.css: %v", err)
			}

			want := map[string]string{
				"game.js":    "let a = 1;\r\nlet b = 2;\r\n",
				"js/util.js": "let c = 3;\r\nlet d = 4;\r\n",
				"lf/b.txt":   "three\nfour\n",
				"style.css":  "body {\r\n  margin: 1px;\r\n}\r\n",
			}
			if staged {
				got := r.StagedFileContent(ctx)
				for p, w := range want {
					if got[p] != w {
						t.Errorf("staged %s = %q, want %q", p, got[p], w)
					}
				}
				return
			}
			for p, w := range want {
				b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
				if err != nil {
					t.Fatal(err)
				}
				if string(b) != w {
					t.Errorf("%s = %q, want %q", p, b, w)
				}
			}
		})
	}
}

// A file with one line, or content with no newline at all, has nothing to fit.
func TestFSWrite_LeavesSingleLineContentAlone(t *testing.T) {
	r, root := newWriteRunner(t)
	mustWrite(t, filepath.Join(root, "a.txt"), "x\r\ny\r\n")
	if _, err := r.FSWrite(context.Background(), tools.FSWriteRequest{Path: "b.txt", Content: "single"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "b.txt")); strings.Contains(string(b), "\r") {
		t.Fatalf("b.txt = %q", b)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A shell script is LF whatever sits beside it: bash reads a CRLF script as
// "\r: command not found".
func TestFSWrite_AScriptStaysLF(t *testing.T) {
	r, root := newWriteRunner(t)
	mustWrite(t, filepath.Join(root, "build.ps1"), "Write-Host a\r\nWrite-Host b\r\n")
	mustWrite(t, filepath.Join(root, "run.bat"), "@echo off\r\necho a\r\n")
	for p, content := range map[string]string{
		"build.sh": "set -e\necho build\n",
		"run":      "#!/usr/bin/env bash\necho run\n",
		"tool.py":  "#!/usr/bin/env python3\nprint(1)\n",
	} {
		if _, err := r.FSWrite(context.Background(), tools.FSWriteRequest{Path: p, Content: content}); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		if b, _ := os.ReadFile(filepath.Join(root, p)); string(b) != content {
			t.Errorf("%s = %q, want it as written", p, b)
		}
	}
}

// Files of the same kind decide first: a new .go file beside LF .go files is
// LF even when the CRLF .bat and .ps1 files beside it outnumber them.
func TestFSWrite_SameExtensionNeighboursDecide(t *testing.T) {
	r, root := newWriteRunner(t)
	mustWrite(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	mustWrite(t, filepath.Join(root, "a.bat"), "@echo off\r\necho a\r\n")
	mustWrite(t, filepath.Join(root, "b.bat"), "@echo off\r\necho b\r\n")
	mustWrite(t, filepath.Join(root, "c.ps1"), "Write-Host c\r\nWrite-Host d\r\n")
	content := "package main\n\nfunc helper() {}\n"
	if _, err := r.FSWrite(context.Background(), tools.FSWriteRequest{Path: "helper.go", Content: content}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "helper.go")); string(b) != content {
		t.Fatalf("helper.go = %q, want LF like main.go", b)
	}
}
