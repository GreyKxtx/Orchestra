package llm

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// defaultAzureAPIVersion is the GA version used when the config pins none.
// Azure requires api-version on every request and rejects the call without
// it, so there has to be a default; a GA release (not a preview) is the one
// that stays available longest.
const defaultAzureAPIVersion = "2024-10-21"

// azureFromConfig returns the Azure settings for cfg, or nil for a plain
// OpenAI-compatible endpoint. Provider "azure" is enough on its own — the
// deployment and version both have defaults.
func azureFromConfig(cfg LLMConfig) *AzureConfig {
	if cfg.Azure != nil {
		a := *cfg.Azure
		return &a
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Provider), "azure") {
		return &AzureConfig{}
	}
	return nil
}

// azureChatCompletionsURL builds the deployment-scoped endpoint.
//
// Azure does not serve /v1/chat/completions with the model in the body: the
// model is the deployment in the path, and the API version is a required
// query parameter.
func (c *OpenAIClient) azureChatCompletionsURL() (string, error) {
	base := strings.TrimSuffix(strings.TrimSpace(c.baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("api_base is empty")
	}
	// The portal shows the endpoint both bare and with /openai; accept either
	// rather than emitting /openai/openai/deployments.
	base = strings.TrimSuffix(base, "/openai")

	deployment := strings.TrimSpace(c.azure.Deployment)
	if deployment == "" {
		deployment = strings.TrimSpace(c.model)
	}
	if deployment == "" {
		return "", fmt.Errorf("azure: neither a deployment nor a model is configured")
	}
	version := strings.TrimSpace(c.azure.APIVersion)
	if version == "" {
		version = defaultAzureAPIVersion
	}
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		base, url.PathEscape(deployment), url.QueryEscape(version)), nil
}

// setAuthHeader applies the endpoint's authentication scheme. Azure OpenAI
// authenticates with an api-key header and rejects an Authorization bearer;
// every other OpenAI-compatible endpoint is the reverse.
func (c *OpenAIClient) setAuthHeader(h http.Header) {
	if c.apiKey == "" {
		return
	}
	if c.azure != nil {
		h.Set("api-key", c.apiKey)
		return
	}
	h.Set("Authorization", "Bearer "+c.apiKey)
}

// isAzureEndpoint reports whether a host is an Azure OpenAI resource. Those
// hosts are per-customer, so ProviderCatalog cannot list them, but they bill
// like any vendor endpoint.
func isAzureEndpoint(apiBase string) bool {
	h := endpointHost(apiBase)
	return strings.HasSuffix(h, ".openai.azure.com") ||
		strings.HasSuffix(h, ".cognitiveservices.azure.com")
}
