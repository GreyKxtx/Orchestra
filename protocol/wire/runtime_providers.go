package wire

import ()

// runtime.list_providers.

// RuntimeListProvidersParams lists catalog + named providers.
type RuntimeListProvidersParams struct {
	// Probe fetches /models for each configured (ready) provider. Default false.
	Probe *bool `json:"probe,omitempty"`
	// ProbeKey limits probe to one provider key (catalog or named).
	ProbeKey string `json:"probe_key,omitempty"`
	// IncludeSecrets returns api_key in entries (settings UI only — local trusted client).
	IncludeSecrets *bool `json:"include_secrets,omitempty"`
}

// RuntimeProviderEntry is one selectable provider in settings UI.
type RuntimeProviderEntry struct {
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	Category     string              `json:"category"`
	APIBase      string              `json:"api_base"`
	Active       bool                `json:"active"`
	Ready        bool                `json:"ready"`
	Configured   bool                `json:"configured"`
	APIKeySet    bool                `json:"api_key_set"`
	APIKey       string              `json:"api_key,omitempty"`
	NeedsKey     bool                `json:"needs_key"`
	Named        bool                `json:"named"`
	Custom       bool                `json:"custom"`
	CurrentModel string              `json:"current_model,omitempty"`
	Models       []RuntimeModelEntry `json:"models,omitempty"`
	ModelsError  string              `json:"models_error,omitempty"`
	ModelCount   int                 `json:"model_count"`
}

// RuntimeListProvidersResult is returned by runtime.list_providers.
type RuntimeListProvidersResult struct {
	Providers      []RuntimeProviderEntry `json:"providers"`
	ActiveProvider string                 `json:"active_provider"`
	ActiveModel    string                 `json:"active_model"`
}
