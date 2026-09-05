package memory

import "testing"

func TestEntrySimilarity(t *testing.T) {
	cases := []struct {
		name    string
		a, b    string
		wantDup bool
	}{
		{
			name:    "same fact, reworded",
			a:       "The build runs via make build, not go build",
			b:       "Build runs via make build (not go build)",
			wantDup: true,
		},
		{
			name:    "identical",
			a:       "Tests live in internal/agent",
			b:       "Tests live in internal/agent",
			wantDup: true,
		},
		{
			name:    "same topic, different fact",
			a:       "The build runs via make build",
			b:       "The tests run via make test",
			wantDup: false,
		},
		{
			name:    "unrelated",
			a:       "Prefers Russian in chat",
			b:       "The HTTP server listens on 8080",
			wantDup: false,
		},
		{
			// A short note must not match everything just for being short.
			name:    "short and different",
			a:       "use gofmt",
			b:       "use ripgrep",
			wantDup: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := entrySimilarity(tc.a, tc.b)
			isDup := got >= dedupThreshold
			if isDup != tc.wantDup {
				t.Errorf("similarity(%q, %q) = %.2f, dup=%v want dup=%v", tc.a, tc.b, got, isDup, tc.wantDup)
			}
		})
	}
}

func TestEntrySimilarity_IgnoresTheTimestampLine(t *testing.T) {
	// Stored entries carry "*<ts>* [type]" headers. Two different facts must
	// not look similar just because both headers are nearly identical.
	a := "*2026-09-05T10:00:00Z* [project]\n\nThe build runs via make build"
	b := "*2026-09-05T10:00:01Z* [project]\n\nThe HTTP server listens on 8080"
	if got := entrySimilarity(a, b); got >= dedupThreshold {
		t.Errorf("similarity = %.2f; headers dominated the comparison", got)
	}
}

func TestFindNearDuplicate(t *testing.T) {
	entries := []string{
		"*t1* [project]\n\nTests live in internal/agent",
		"*t2* [project]\n\nThe build runs via make build, not go build",
		"*t3* [feedback]\n\nDo not reformat untouched files",
	}
	i, ok := findNearDuplicate(entries, "Build runs via make build (not go build)")
	if !ok || i != 1 {
		t.Fatalf("findNearDuplicate = %d/%v, want index 1", i, ok)
	}

	if _, ok := findNearDuplicate(entries, "The HTTP server listens on port 8080"); ok {
		t.Error("a genuinely new fact must not match anything")
	}
	if _, ok := findNearDuplicate(nil, "anything"); ok {
		t.Error("an empty memory has nothing to match")
	}
}
