package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/protocol/schema"
)

// The write tool reported success and the file on disk was zero bytes.
//
// Caught by the eval task two_files, which fails roughly one run in four. The
// model sent the right thing — the proxy in front of the model server has the
// bytes — and both tools answered with a real result:
//
//	{"path":"util.go","bytes_written":55,
//	 "applied":"util.go written: 4 lines added at line 2",
//	 "changed_region":"1: package main\n2: \n3: func Double(n int) int {…"}
//
// while util.go and main.go were both 0 bytes afterwards. The fixture files
// are not empty to begin with, so this is content destroyed, not content never
// written.
//
// bytes_written and changed_region both come from the REQUEST, so neither is
// evidence about the disk: in dry-run staging the tool answers without
// touching it. The disk write happens later, in CommitStagedPath, and nothing
// logs its result. This drives that stretch directly.

// twoFilesFixture mirrors the eval task: a package with a stub main and an
// almost-empty util, both of which the turn rewrites.
func twoFilesFixture(t *testing.T) (root, utilPath, mainPath string) {
	t.Helper()
	root = t.TempDir()
	files := map[string]string{
		"go.mod":  "module evalws\n\ngo 1.21\n",
		"util.go": "package main\n",
		"main.go": "package main\n\nfunc main() {\n}\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, filepath.Join(root, "util.go"), filepath.Join(root, "main.go")
}

const newUtilGo = "package main\n\nfunc Double(n int) int {\n\treturn n * 2\n}\n"
const newMainGo = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(Double(21))\n}\n"

// Two writes in one turn, each pinned to the hash the model read, then a final
// with nothing left to do — the exact shape of the failing run.
func TestWrite_BothFilesReachDiskWithTheirContent(t *testing.T) {
	root, utilPath, mainPath := twoFilesFixture(t)

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	client := &recordingLLM{steps: []string{
		`{"type":"tool_call","tool":{"name":"read","input":{"path":"util.go"}}}`,
		`{"type":"tool_call","tool":{"name":"read","input":{"path":"main.go"}}}`,
		`{"type":"tool_call","tool":{"name":"write","input":{"path":"util.go",` +
			`"content":` + jsonString(newUtilGo) + `,` +
			`"file_hash":` + jsonString(cache.ComputeSHA256([]byte("package main\n"))) + `}}}`,
		`{"type":"tool_call","tool":{"name":"write","input":{"path":"main.go",` +
			`"content":` + jsonString(newMainGo) + `,` +
			`"file_hash":` + jsonString(cache.ComputeSHA256([]byte("package main\n\nfunc main() {\n}\n"))) + `}}}`,
		`{"patches":[]}`,
	}}

	ag, err := New(client, v, tr, Options{MaxSteps: 8, Apply: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := ag.Run(context.Background(),
		nil, "Add Double to util.go and call it from main.go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, f := range []struct{ name, path, want string }{
		{"util.go", utilPath, newUtilGo},
		{"main.go", mainPath, newMainGo},
	} {
		got, readErr := os.ReadFile(f.path)
		if readErr != nil {
			t.Errorf("%s: %v", f.name, readErr)
			continue
		}
		if len(got) == 0 {
			t.Errorf("%s is empty on disk — the write reported success and destroyed the file", f.name)
			continue
		}
		if string(got) != f.want {
			t.Errorf("%s on disk is not what the write sent:\ngot:\n%s\nwant:\n%s", f.name, got, f.want)
		}
	}
}
