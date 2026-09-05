package llm

import "testing"

func TestIsKnownCloudEndpoint(t *testing.T) {
	cases := []struct {
		apiBase string
		want    bool
	}{
		{"https://openrouter.ai/api/v1", true},
		{"https://api.openai.com/v1", true},
		{"https://api.anthropic.com", true},
		{"HTTPS://API.ANTHROPIC.COM/", true},
		{"https://api.groq.com/openai/v1", true},

		// Local runtimes: a model name that happens to match a hosted one
		// must not acquire that model's price.
		{"http://localhost:1234/v1", false},
		{"http://127.0.0.1:11434", false},
		{"http://192.168.1.50:8000/v1", false},
		// A self-hosted vLLM behind a tunnel is not a vendor endpoint either,
		// even though the host is public — this is the shape the field run
		// actually used.
		{"https://abc123.ngrok-free.app/v1", false},
		{"", false},
		{"not a url", false},
	}
	for _, tc := range cases {
		if got := IsKnownCloudEndpoint(tc.apiBase); got != tc.want {
			t.Errorf("IsKnownCloudEndpoint(%q) = %v, want %v", tc.apiBase, got, tc.want)
		}
	}
}
