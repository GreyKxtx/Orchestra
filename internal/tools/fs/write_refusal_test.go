package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// A refused write has to tell a local model what to send next. "fs.write
// requires file_hash (for overwrite) or must_not_exist=true (for create)" did
// not: the file existed, the model had not read it, and it tried
// must_not_exist next — which the file's existence refuses too.
func TestFSWrite_RefusalsSayWhatToSendNext(t *testing.T) {
	cases := []struct {
		name string
		req  tools.FSWriteRequest
		want []string
	}{
		{
			name: "overwrite without file_hash",
			req:  tools.FSWriteRequest{Path: "app.js", Content: "let x = 2;\n"},
			want: []string{"app.js", "already exists", "read", "file_hash", "edit"},
		},
		{
			name: "must_not_exist on an existing file",
			req:  tools.FSWriteRequest{Path: "app.js", Content: "let x = 2;\n", MustNotExist: true},
			want: []string{"app.js", "already exists", "read", "file_hash", "edit"},
		},
		{
			name: "file_hash of another version",
			req: tools.FSWriteRequest{Path: "app.js", Content: "let x = 2;\n",
				FileHash: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			want: []string{"app.js", "changed since", "read", "file_hash"},
		},
	}
	for _, staged := range []bool{false, true} {
		for _, tc := range cases {
			name := tc.name
			if staged {
				name += " (staged turn)"
			}
			t.Run(name, func(t *testing.T) {
				r, root := newWriteRunner(t)
				if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("one\ntwo\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				if staged {
					turn := r.NewTurn(tools.TurnOptions{DryRun: true, Apply: true})
					defer turn.Close()
					ctx = tools.WithTurn(ctx, turn)
				}
				_, err := r.FSWrite(ctx, tc.req)
				if err == nil {
					t.Fatal("the write must be refused")
				}
				for _, w := range tc.want {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("error %q does not say %q", err.Error(), w)
					}
				}
				if b, _ := os.ReadFile(filepath.Join(root, "app.js")); string(b) != "one\ntwo\n" {
					t.Fatalf("a refused write changed the file: %q", b)
				}
			})
		}
	}
}
