package applier

import (
	"os"
	"strings"
	"testing"
)

func TestUnifiedPatch_Modify(t *testing.T) {
	diff := UnifiedPatch([]FileDiff{{
		Path:   "hello.go",
		Before: "package main\n\nfunc main() {}\n",
		After:  "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n",
	}})
	if !strings.Contains(diff, "--- a/hello.go") {
		t.Fatalf("missing old header:\n%s", diff)
	}
	if !strings.Contains(diff, "+++ b/hello.go") {
		t.Fatalf("missing new header:\n%s", diff)
	}
	if !strings.Contains(diff, "@@") {
		t.Fatalf("missing hunk:\n%s", diff)
	}
}

func TestUnifiedPatch_CreateAndDelete(t *testing.T) {
	created := UnifiedPatch([]FileDiff{{
		Path:   "new.txt",
		Before: "",
		After:  "hello\n",
	}})
	if !strings.Contains(created, "--- /dev/null") {
		t.Fatalf("create should use /dev/null old:\n%s", created)
	}
	if !strings.Contains(created, "+++ b/new.txt") {
		t.Fatalf("create missing new path:\n%s", created)
	}

	deleted := UnifiedPatch([]FileDiff{{
		Path:   "old.txt",
		Before: "bye\n",
		After:  "",
	}})
	if !strings.Contains(deleted, "+++ /dev/null") {
		t.Fatalf("delete should use /dev/null new:\n%s", deleted)
	}
}

func TestUnifiedPatch_Empty(t *testing.T) {
	if UnifiedPatch(nil) != "" {
		t.Fatal("expected empty string for nil diffs")
	}
}

func TestWriteUnifiedPatch(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.patch"
	err := WriteUnifiedPatch(path, []FileDiff{{
		Path:   "f.txt",
		Before: "a\n",
		After:  "b\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "f.txt") {
		t.Fatalf("patch missing path:\n%s", data)
	}
}

func TestDefaultPatchPath(t *testing.T) {
	p := DefaultPatchPath(".orchestra/patches")
	if !strings.Contains(p, "orchestra-") || !strings.HasSuffix(p, ".patch") {
		t.Fatalf("unexpected path: %s", p)
	}
}
