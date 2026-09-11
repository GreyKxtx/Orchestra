package core

import "testing"

// A config with no llm.provider is normal — `orchestra init` writes one, and
// the agent works from api_base and model alone. Every provider-shaped
// surface still needs the endpoint to name itself, or nothing shows as
// current and there is nothing to change: that is what the settings panel
// looked like while the chat was talking to a model.
func TestProviderForAPIBase(t *testing.T) {
	cases := map[string]string{
		"http://localhost:1234/v1":     "lmstudio",
		"http://localhost:1234":        "lmstudio",
		"http://localhost:1234/":       "lmstudio",
		"HTTP://LOCALHOST:1234/V1":     "lmstudio",
		"http://localhost:11434":       "ollama",
		"https://api.openai.com/v1":    "openai",
		"https://openrouter.ai/api/v1": "openrouter",
		// vLLM's catalogue endpoint already carries /v1; stripping it must not
		// make it collide with anything else.
		"http://localhost:8000/v1": "vllm",
		// No match stays empty rather than falling onto some entry, and the
		// two entries with no default endpoint never claim another's URL.
		"https://example.invalid/v1":    "",
		"https://mine.openai.azure.com": "",
		"":                              "",
		"   ":                           "",
	}
	for in, want := range cases {
		if got := providerForAPIBase(in); got != want {
			t.Errorf("providerForAPIBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormaliseAPIBaseIsIdempotent(t *testing.T) {
	for _, s := range []string{
		"http://localhost:1234/v1/",
		"http://localhost:1234/v1",
		"http://localhost:1234/",
		"http://localhost:1234",
	} {
		got := normaliseAPIBase(s)
		if got != "http://localhost:1234" {
			t.Errorf("normaliseAPIBase(%q) = %q", s, got)
		}
		if again := normaliseAPIBase(got); again != got {
			t.Errorf("normaliseAPIBase is not idempotent: %q -> %q", got, again)
		}
	}
}
