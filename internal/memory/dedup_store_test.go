package memory

import (
	"os"
	"strings"
	"testing"
)

func TestAppendTyped_RestatingAFactReplacesItInPlace(t *testing.T) {
	s, root := typedStore(t)
	if _, _, err := s.AppendTyped("project", TypeProject, "The build runs via make build, not go build"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AppendTyped("project", TypeProject, "Build runs via make build (not go build)"); err != nil {
		t.Fatal(err)
	}
	body := agentFile(t, root)
	if n := len(splitEntries(body)); n != 1 {
		t.Fatalf("entries = %d, want 1 — the fact was restated, not added\n%s", n, body)
	}
	if !strings.Contains(body, "(not go build)") {
		t.Errorf("the newer wording did not win:\n%s", body)
	}
}

func TestAppendTyped_ADifferentFactIsStillAdded(t *testing.T) {
	s, root := typedStore(t)
	if _, _, err := s.AppendTyped("project", TypeProject, "The build runs via make build"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AppendTyped("project", TypeProject, "The HTTP server listens on port 8080"); err != nil {
		t.Fatal(err)
	}
	if n := len(splitEntries(agentFile(t, root))); n != 2 {
		t.Fatalf("entries = %d, want 2", n)
	}
}

func TestAppendTyped_ReplacingKeepsAPin(t *testing.T) {
	// A pin is the user saying "never lose this". Restating the fact must not
	// quietly strip that.
	s, root := typedStore(t)
	if _, _, err := s.AppendTyped("project", TypeProject, "[pin] The build runs via make build, not go build"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AppendTyped("project", TypeProject, "Build runs via make build (not go build)"); err != nil {
		t.Fatal(err)
	}
	body := agentFile(t, root)
	if !strings.Contains(body, "[pin]") {
		t.Errorf("the pin was lost on replace:\n%s", body)
	}
	if n := len(splitEntries(body)); n != 1 {
		t.Fatalf("entries = %d, want 1\n%s", n, body)
	}
}

func TestAppendTyped_ReplacingTakesTheNewType(t *testing.T) {
	// The same sentence written again as feedback means the user is now
	// telling you it is a rule, not a fact.
	s, root := typedStore(t)
	if _, _, err := s.AppendTyped("project", TypeProject, "Do not reformat files you did not edit"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AppendTyped("project", TypeFeedback, "Do not reformat files you did not edit"); err != nil {
		t.Fatal(err)
	}
	body := agentFile(t, root)
	if strings.Contains(body, "[project]") || !strings.Contains(body, "[feedback]") {
		t.Errorf("type was not upgraded:\n%s", body)
	}
}

func TestAppendTyped_SessionScopeStillAppends(t *testing.T) {
	// Session memory is a running log of one conversation; collapsing
	// repetition there would erase the fact that something recurred.
	dir := t.TempDir()
	cfg := Config{SessionEnabled: true}
	cfg.Normalize()
	s := NewStore(dir, "sess-1", cfg)
	for i := 0; i < 2; i++ {
		if _, _, err := s.AppendTyped("session", TypeProject, "Looked at internal/agent/agent_step.go"); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(s.sessionFilePath())
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	if n := len(splitEntries(raw)); n != 2 {
		t.Fatalf("session entries = %d, want 2\n%s", n, raw)
	}
}

func TestAppendEntry_ReportsWhetherItReplaced(t *testing.T) {
	// Whether a write grew memory or updated it is the only way to tell,
	// after a run, that deduplication did anything at all.
	s, _ := typedStore(t)
	res, err := s.AppendEntry("project", TypeProject, "The build runs via make build, not go build")
	if err != nil {
		t.Fatal(err)
	}
	if res.Replaced {
		t.Error("the first write of a fact cannot be a replacement")
	}
	if res.Type != TypeProject || res.Path == "" {
		t.Errorf("res = %+v", res)
	}

	res, err = s.AppendEntry("project", TypeFeedback, "Build runs via make build (not go build)")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replaced {
		t.Error("restating a fact must be reported as a replacement")
	}
	if res.Type != TypeFeedback {
		t.Errorf("res.Type = %q, want the new type", res.Type)
	}
}
