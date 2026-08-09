package e2e_agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/tools"
)

const ambiguousMatchMaxRate = 0.10

type scopedEditCase struct {
	name         string
	fileContent  string
	relPath      string
	symbol       string
	lineStart    int
	lineEnd      int
	search       string
	replace      string
	expectAmbig  bool
}

// TestEval_AmbiguousMatchRate_WithTargetSymbol measures AmbiguousMatch rate when
// target_symbol scoping is applied via CKG line ranges (Planner–Worker P5).
// Acceptance: among disambiguation scenarios, AmbiguousMatch rate < 10%.
func TestEval_AmbiguousMatchRate_WithTargetSymbol(t *testing.T) {
	cases := []scopedEditCase{
		{
			name: "duplicate if-err scoped to A",
			fileContent: "package main\n\nfunc A() {\n\tif err != nil {}\n}\n\nfunc B() {\n\tif err != nil {}\n}\n",
			relPath: "dup.go", symbol: "A", lineStart: 3, lineEnd: 5,
			search: "if err != nil {}", replace: "if err != nil { return err }",
		},
		{
			name: "duplicate if-err scoped to B",
			fileContent: "package main\n\nfunc A() {\n\tif err != nil {}\n}\n\nfunc B() {\n\tif err != nil {}\n}\n",
			relPath: "dup2.go", symbol: "B", lineStart: 7, lineEnd: 9,
			search: "if err != nil {}", replace: "if err != nil { return err }",
		},
		{
			name: "return stmt scoped to add",
			fileContent: "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n\nfunc sub(a, b int) int {\n\treturn a - b\n}\n",
			relPath: "math.go", symbol: "add", lineStart: 3, lineEnd: 5,
			search: "return a + b", replace: "return a + b + 1",
		},
		{
			name: "return stmt scoped to sub",
			fileContent: "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n\nfunc sub(a, b int) int {\n\treturn a - b\n}\n",
			relPath: "math2.go", symbol: "sub", lineStart: 7, lineEnd: 9,
			search: "return a - b", replace: "return a - b - 1",
		},
		{
			name: "assignment scoped to Handler",
			fileContent: "package main\n\nfunc Handler() {\n\tx := 1\n\tx = 2\n}\n\nfunc Other() {\n\tx := 1\n\tx = 2\n}\n",
			relPath: "handler.go", symbol: "Handler", lineStart: 3, lineEnd: 6,
			search: "x = 2", replace: "x = 3",
		},
		{
			name: "duplicate inside scope still ambiguous",
			fileContent: "package main\n\nfunc X() {\n\ta()\n\ta()\n}\n",
			relPath: "inner.go", symbol: "X", lineStart: 3, lineEnd: 6,
			search: "a()", replace: "b()",
			expectAmbig: true,
		},
	}

	var disambigTotal, disambigAmbig int

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.relPath), []byte(tc.fileContent), 0644); err != nil {
				t.Fatalf("write file: %v", err)
			}

			r, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: true})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			t.Cleanup(func() { r.Close() })

			hash := cache.ComputeSHA256([]byte(tc.fileContent))
			if err := r.SeedCKGSymbolForTest(context.Background(), tc.relPath, hash, tc.symbol, tc.lineStart, tc.lineEnd); err != nil {
				t.Fatalf("SeedCKGSymbolForTest: %v", err)
			}

			if !tc.expectAmbig {
				disambigTotal++
			}

			_, err = r.FSEdit(context.Background(), tools.FSEditRequest{
				Path:         tc.relPath,
				Search:       tc.search,
				Replace:      tc.replace,
				FileHash:     hash,
				TargetSymbol: tc.symbol,
			})

			if tc.expectAmbig {
				if err == nil {
					t.Fatal("expected AmbiguousMatch, got success")
				}
				pe, ok := protocol.AsError(err)
				if !ok || pe.Code != protocol.AmbiguousMatch {
					t.Fatalf("expected AmbiguousMatch, got %v", err)
				}
				return
			}

			if err != nil {
				if pe, ok := protocol.AsError(err); ok && pe.Code == protocol.AmbiguousMatch {
					disambigAmbig++
				}
				t.Fatalf("scoped edit failed: %v", err)
			}

			staged := r.StagedFileContent()
			got, ok := staged[tc.relPath]
			if !ok || !strings.Contains(got, tc.replace) {
				t.Fatalf("staged content missing replace %q: %q", tc.replace, got)
			}
		})
	}

	if disambigTotal == 0 {
		t.Fatal("no disambiguation scenarios")
	}
	rate := float64(disambigAmbig) / float64(disambigTotal)
	t.Logf("AmbiguousMatch rate (target_symbol disambiguation): %d/%d = %.2f%%",
		disambigAmbig, disambigTotal, rate*100)
	if rate >= ambiguousMatchMaxRate {
		t.Fatalf("AmbiguousMatch rate %.2f%% ≥ %.0f%% acceptance threshold",
			rate*100, ambiguousMatchMaxRate*100)
	}
}
