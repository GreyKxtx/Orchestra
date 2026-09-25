package wire_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/orchestra/orchestra/protocol/wire"
	"github.com/orchestra/orchestra/protocol/wire/internal/wiregen"
)

// The generated copies of the contract are committed, so a client builds
// without Go; this keeps them current. It fails when a wire type changed
// and `go generate ./protocol/wire/...` was not run.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	files, err := wiregen.Generate(".")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	root := filepath.Join("..", "..")
	var paths []string
	for rel := range files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		want := files[rel]
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s: %v — run: go generate ./protocol/wire/...", rel, err)
			continue
		}
		// A Windows checkout may carry CRLF; the contract is the text.
		got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale — run: go generate ./protocol/wire/...", rel)
		}
	}
}

// Every method a connection answers has its signature listed, once.
func TestSignatures_CoverEveryMethod(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range wire.Signatures() {
		if seen[s.Method] {
			t.Errorf("%s is listed twice", s.Method)
		}
		seen[s.Method] = true
	}
	for _, m := range wire.Methods() {
		if m == wire.MethodCancelRequest {
			continue
		}
		if !seen[m] {
			t.Errorf("%s has no signature in protocol/wire/signatures.go", m)
		}
		delete(seen, m)
	}
	for m := range seen {
		t.Errorf("%s has a signature but is not a method", m)
	}
}
