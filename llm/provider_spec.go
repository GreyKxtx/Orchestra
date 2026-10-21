package llm

import (
	"net/url"
	"strings"
)

// providerSpec is what one hosted API accepts where it differs from the
// OpenAI-compatible baseline every client starts from. The differences are
// not cosmetic: each row below was a request the provider refused, or a
// setting it silently ignored.
//
// The spec is chosen by the endpoint, not by the config's provider field: a
// local vLLM is configured with `provider: openai` (it speaks the dialect)
// and must not be treated as OpenAI's own API.
type providerSpec struct {
	// key is the catalogue key of the hosted provider, "" for a local or
	// unknown OpenAI-compatible server.
	key string

	// modelsAuth is how the key goes on GET …/models: "bearer" (the
	// baseline), "x-api-key" (Anthropic) or "api-key" (Azure).
	modelsAuth string

	// cloud: an unset max_tokens is left off the wire, so the provider
	// answers up to the model's own output ceiling. Locally the default is
	// sent instead — vLLM without one falls back to its generation config
	// (~50k) and overflows a small --max-model-len — but a hosted model
	// capped at 4096 cut long answers and file writes short, and a larger
	// fixed default is refused by the providers whose ceiling is below it
	// (DeepSeek's is 8192).
	cloud bool
}

// specFor picks the spec for an endpoint. provider is the config's provider
// field, consulted only where the endpoint does not tell (Anthropic behind a
// proxy, Azure by name).
func specFor(provider, apiBase string) providerSpec {
	host := endpointHost(apiBase)
	if host != "" {
		for _, p := range ProviderCatalog {
			if p.Local || p.DefaultAPIBase == "" {
				continue
			}
			if endpointHost(p.DefaultAPIBase) == host {
				return newSpec(p.Key)
			}
		}
		if isAzureEndpoint(apiBase) {
			return newSpec("azure")
		}
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic":
		return newSpec("anthropic")
	case "azure":
		return newSpec("azure")
	}
	return providerSpec{modelsAuth: "bearer"}
}

func newSpec(key string) providerSpec {
	s := providerSpec{key: key, modelsAuth: "bearer", cloud: true}
	switch key {
	case "anthropic":
		s.modelsAuth = "x-api-key"
	case "azure":
		s.modelsAuth = "api-key"
	}
	return s
}

// IsHostedEndpoint reports whether an endpoint is a hosted provider's own
// API (the catalogue's cloud and gateway hosts, Azure, Anthropic). Its
// generation settings are its own: the main llm:'s max_tokens and
// temperature are tuned for a local server and are not inherited.
func IsHostedEndpoint(provider, apiBase string) bool {
	return specFor(provider, apiBase).cloud
}

// modelsURLs are the listing endpoints to try, in order.
func (s providerSpec) modelsURLs(base, azureVersion string) []string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	switch s.key {
	case "anthropic":
		// One page holds every model: the default page is 20.
		return []string{strings.TrimSuffix(base, "/v1") + "/v1/models?limit=1000"}
	case "azure":
		v := strings.TrimSpace(azureVersion)
		if v == "" {
			v = defaultAzureAPIVersion
		}
		return []string{strings.TrimSuffix(base, "/openai") + "/openai/models?api-version=" + url.QueryEscape(v)}
	case "":
		// A local server: LM Studio and Ollama serve /v1/models from their
		// bare root, a vLLM base already ends in /v1.
		if strings.HasSuffix(base, "/v1") {
			return []string{base + "/models"}
		}
		return []string{base + "/v1/models", base + "/models"}
	default:
		// Hosted bases carry their version (…/v1, …/v1beta/openai,
		// …/openai/v1): the listing is right under them.
		return []string{base + "/models"}
	}
}

// maxTokensField is the request field the completion budget goes in.
//
// OpenAI's reasoning models (o-series, gpt-5) refuse max_tokens and take
// max_completion_tokens, which OpenAI's own API takes for every chat model.
// Azure follows its api-version: max_completion_tokens only where the model
// needs it, since an older version does not know the field.
func (s providerSpec) maxTokensField(model string) string {
	switch s.key {
	case "openai":
		return "max_completion_tokens"
	case "azure":
		if openAIReasoningModel(model) {
			return "max_completion_tokens"
		}
	}
	return "max_tokens"
}

// sendsTemperature reports whether a configured temperature goes on the
// wire. OpenAI's reasoning models refuse any but the default ("Unsupported
// value: 'temperature'"), wherever they are served from.
func (s providerSpec) sendsTemperature(model string) bool {
	return !openAIReasoningModel(model)
}

// openAIReasoningModel reports OpenAI's reasoning families: o1, o3, o4-mini
// and gpt-5 with their dated and sized variants, bare or with a gateway's
// "openai/" prefix.
func openAIReasoningModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	if strings.HasPrefix(m, "gpt-5") {
		return true
	}
	if len(m) >= 2 && m[0] == 'o' && m[1] >= '1' && m[1] <= '9' {
		return len(m) == 2 || m[2] == '-' || m[2] == '.'
	}
	return false
}
