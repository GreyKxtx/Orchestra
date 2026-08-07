package view

import "testing"

func TestFindProviderByKey(t *testing.T) {
	p, ok := FindProviderByKey("ollama")
	if !ok || p.Key != "ollama" || !p.EndpointEditable {
		t.Fatalf("unexpected ollama entry: %+v ok=%v", p, ok)
	}
	_, ok = FindProviderByKey("missing")
	if ok {
		t.Fatal("expected missing provider")
	}
}

func TestProviderWithSavedEndpoint(t *testing.T) {
	p := ProviderWithSavedEndpoint("lmstudio", "http://192.168.0.5:1234")
	if p.Endpoint != "http://192.168.0.5:1234" {
		t.Fatalf("endpoint = %q", p.Endpoint)
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	if got := NormalizeEndpoint("http://localhost:1234/"); got != "http://localhost:1234" {
		t.Fatalf("got %q", got)
	}
}

func TestDialogProviders_HasCommunityGateways(t *testing.T) {
	keys := map[string]bool{}
	for _, p := range DialogProviders {
		keys[p.Key] = true
	}
	for _, want := range []string{"openrouter", "groq", "google", "deepseek", "custom", "vllm"} {
		if !keys[want] {
			t.Fatalf("missing provider %q in catalog", want)
		}
	}
}
