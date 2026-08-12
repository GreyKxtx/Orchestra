package agent

import "testing"

func TestWorkerPathInEditScope(t *testing.T) {
	allowed := []string{"internal/api/handler.go", "pkg/foo/bar.go"}
	tests := []struct {
		path string
		want bool
	}{
		{"internal/api/handler.go", true},
		{"./internal/api/handler.go", true},
		{"internal/other.go", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := workerPathInEditScope(tc.path, allowed); got != tc.want {
			t.Errorf("workerPathInEditScope(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
	if workerPathInEditScope("any.go", nil) != true {
		t.Fatal("empty allowed should permit any path")
	}
}

func TestCheckProductEditScope(t *testing.T) {
	a := &Agent{opts: Options{Mode: ModeProduct}}

	// Reads are unrestricted (brownfield context).
	if err := a.checkProductEditScope("read", []byte(`{"path":"internal/core/core.go"}`)); err != nil {
		t.Fatalf("read should pass: %v", err)
	}
	// Writes inside .orchestra/product/ are allowed.
	for _, p := range []string{".orchestra/product/PRD.md", "./.orchestra/product/User_Stories.md"} {
		if err := a.checkProductEditScope("write", []byte(`{"path":"`+p+`","content":"x"}`)); err != nil {
			t.Fatalf("write %s should pass: %v", p, err)
		}
	}
	// Writes anywhere else are denied.
	for _, p := range []string{"main.go", ".orchestra/state.md", "docs/PRD.md"} {
		if err := a.checkProductEditScope("edit", []byte(`{"path":"`+p+`","search":"x","replace":"y"}`)); err == nil {
			t.Fatalf("edit %s must be denied", p)
		}
	}
	// Other modes are untouched by this check.
	b := &Agent{opts: Options{Mode: ModeBuild}}
	if err := b.checkProductEditScope("write", []byte(`{"path":"main.go","content":"x"}`)); err != nil {
		t.Fatalf("build mode must not be product-scoped: %v", err)
	}
}

func TestCheckWorkerEditScope(t *testing.T) {
	a := &Agent{
		opts: Options{
			Mode:            ModeWorker,
			WorkerEditPaths: []string{"a.go"},
		},
	}
	if err := a.checkWorkerEditScope("read", []byte(`{"path":"b.go"}`)); err != nil {
		t.Fatalf("read should pass: %v", err)
	}
	if err := a.checkWorkerEditScope("edit", []byte(`{"path":"b.go","search":"x","replace":"y"}`)); err == nil {
		t.Fatal("expected scope violation on b.go")
	}
	if err := a.checkWorkerEditScope("edit", []byte(`{"path":"a.go","search":"x","replace":"y"}`)); err != nil {
		t.Fatalf("a.go should be allowed: %v", err)
	}
}
