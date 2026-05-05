package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
			name:  "only newline",
			input: "\n",
			want:  "1: \n",
		},
		{
			name:  "padding at 100 lines",
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
