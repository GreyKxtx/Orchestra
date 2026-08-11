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
