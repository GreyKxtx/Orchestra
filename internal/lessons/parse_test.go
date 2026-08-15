package lessons

import "testing"

func TestLastEntryOfKind(t *testing.T) {
	root := t.TempDir()
	if err := Append(root, Entry{Dept: "eng", Kind: KindAntiPattern, Task: "fail"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(root, Entry{Dept: "eng", Kind: KindPattern, Task: "ok", Verify: "passed"}); err != nil {
		t.Fatal(err)
	}
	e, ok := LastEntryOfKind(root, "eng", KindPattern)
	if !ok || e.Task != "ok" {
		t.Fatalf("entry=%+v ok=%v", e, ok)
	}
}
