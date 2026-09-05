package llm

import (
	"net/http"
	"testing"
)

func TestAzure_ChatCompletionsURL(t *testing.T) {
	cases := []struct {
		name string
		cfg  LLMConfig
		want string
	}{
		{
			name: "deployment and version from config",
			cfg: LLMConfig{
				Provider: "azure",
				APIBase:  "https://my-resource.openai.azure.com",
				Model:    "gpt-4o",
				Azure:    &AzureConfig{Deployment: "prod-gpt4o", APIVersion: "2025-01-01-preview"},
			},
			want: "https://my-resource.openai.azure.com/openai/deployments/prod-gpt4o/chat/completions?api-version=2025-01-01-preview",
		},
		{
			name: "deployment defaults to the model name",
			cfg: LLMConfig{
				Provider: "azure",
				APIBase:  "https://my-resource.openai.azure.com/",
				Model:    "gpt-4o-mini",
			},
			want: "https://my-resource.openai.azure.com/openai/deployments/gpt-4o-mini/chat/completions?api-version=" + defaultAzureAPIVersion,
		},
		{
			// The portal shows the endpoint both with and without /openai;
			// pasting either must not produce /openai/openai/deployments.
			name: "api_base already ending in /openai",
			cfg: LLMConfig{
				Provider: "azure",
				APIBase:  "https://my-resource.openai.azure.com/openai",
				Model:    "gpt-4o",
			},
			want: "https://my-resource.openai.azure.com/openai/deployments/gpt-4o/chat/completions?api-version=" + defaultAzureAPIVersion,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewOpenAIClient(tc.cfg).chatCompletionsURL()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("URL =\n  %s\nwant\n  %s", got, tc.want)
			}
		})
	}
}

func TestAzure_UsesApiKeyHeaderNotBearer(t *testing.T) {
	c := NewOpenAIClient(LLMConfig{
		Provider: "azure", APIBase: "https://r.openai.azure.com", Model: "gpt-4o", APIKey: "secret",
	})
	h := http.Header{}
	c.setAuthHeader(h)
	if got := h.Get("api-key"); got != "secret" {
		t.Errorf("api-key header = %q, want the key", got)
	}
	if got := h.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want empty (Azure rejects Bearer)", got)
	}
}

func TestNonAzure_KeepsBearerAuth(t *testing.T) {
	c := NewOpenAIClient(LLMConfig{APIBase: "https://api.openai.com/v1", Model: "gpt-4o", APIKey: "secret"})
	h := http.Header{}
	c.setAuthHeader(h)
	if got := h.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization = %q, want Bearer", got)
	}
	if h.Get("api-key") != "" {
		t.Error("non-Azure request must not carry api-key")
	}
	// And the plain endpoint is untouched by the Azure branch.
	got, err := c.chatCompletionsURL()
	if err != nil || got != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("URL = %q err = %v", got, err)
	}
}

func TestAzure_IsSelectableFromTheProviderCatalog(t *testing.T) {
	e, ok := FindCatalogProvider("azure")
	if !ok {
		t.Fatal("azure is missing from ProviderCatalog, so no UI can offer it")
	}
	if !e.NeedsKey || !e.EndpointEditable || e.Local {
		t.Errorf("azure catalog entry = %+v, want a key-needing editable cloud endpoint", e)
	}
	if e.DefaultAPIBase != "" {
		t.Errorf("DefaultAPIBase = %q, want empty — the host is per-resource", e.DefaultAPIBase)
	}
}

func TestAzure_EndpointIsBilled(t *testing.T) {
	// Azure resource hosts are per-customer, so the catalogue cannot list
	// them — but they do send a bill, and the built-in price table must apply.
	if !IsKnownCloudEndpoint("https://my-resource.openai.azure.com/") {
		t.Error("an Azure OpenAI endpoint must count as a hosted provider")
	}
	if IsKnownCloudEndpoint("https://azure.example.com/v1") {
		t.Error("a look-alike host must not")
	}
}
