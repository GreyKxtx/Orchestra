package llm

import (
	"fmt"
	"strings"
	"time"
)

// AuthConfig configures how Orchestra obtains a bearer for this provider.
// Exactly one mechanism may be set.
//
// This is plain data. Resolving it into a working token source lives in
// internal/llmauth, so the llm module keeps zero dependencies.
type AuthConfig struct {
	OAuth *OAuthConfig `yaml:"oauth,omitempty"`

	// TokenCommand is an external helper that prints a bearer on stdout, in
	// the spirit of a git credential helper.
	TokenCommand []string `yaml:"token_command,omitempty"`
	// TokenCommandTTL caches the helper's output. Zero means five minutes.
	TokenCommandTTL time.Duration `yaml:"token_command_ttl,omitempty"`
}

// OAuthConfig names the endpoints and client identity Orchestra authorizes
// with. Orchestra ships no presets: these values come from the user's own
// registered application or their organization's gateway. See
// docs/superpowers/specs/2026-09-08-llm-oauth-design.md, "Non-goals".
type OAuthConfig struct {
	AuthURL       string `yaml:"auth_url,omitempty"`
	TokenURL      string `yaml:"token_url,omitempty"`
	DeviceAuthURL string `yaml:"device_auth_url,omitempty"`
	ClientID      string `yaml:"client_id,omitempty"`
	// ClientSecret is deliberately absent from .orchestra.yml in practice:
	// supply it through .orchestra.local.yml or the environment, the way
	// every other secret in this project is handled.
	ClientSecret string            `yaml:"client_secret,omitempty"`
	Scopes       []string          `yaml:"scopes,omitempty"`
	Flow         string            `yaml:"flow,omitempty"` // browser (default) | device
	ExtraParams  map[string]string `yaml:"extra_params,omitempty"`
}

// EffectiveFlow returns the configured flow, defaulting to browser.
func (o *OAuthConfig) EffectiveFlow() string {
	if o == nil {
		return ""
	}
	if f := strings.TrimSpace(o.Flow); f != "" {
		return f
	}
	return "browser"
}

// EffectiveTokenCommandTTL returns the configured cache lifetime for
// token_command output, defaulting to five minutes.
func (a *AuthConfig) EffectiveTokenCommandTTL() time.Duration {
	if a == nil || a.TokenCommandTTL <= 0 {
		return 5 * time.Minute
	}
	return a.TokenCommandTTL
}

// Validate reports a configuration error in the auth block.
func (a *AuthConfig) Validate() error {
	if a == nil {
		return nil
	}
	hasOAuth := a.OAuth != nil
	hasCommand := len(a.TokenCommand) > 0
	if hasOAuth == hasCommand {
		return fmt.Errorf("auth: set exactly one of oauth or token_command")
	}
	if hasCommand {
		if strings.TrimSpace(a.TokenCommand[0]) == "" {
			return fmt.Errorf("auth: token_command's first element must be the program to run")
		}
		return nil
	}
	o := a.OAuth
	if strings.TrimSpace(o.AuthURL) == "" {
		return fmt.Errorf("auth.oauth: auth_url is required")
	}
	if strings.TrimSpace(o.TokenURL) == "" {
		return fmt.Errorf("auth.oauth: token_url is required")
	}
	if strings.TrimSpace(o.ClientID) == "" {
		return fmt.Errorf("auth.oauth: client_id is required")
	}
	switch o.EffectiveFlow() {
	case "browser":
	case "device":
		if strings.TrimSpace(o.DeviceAuthURL) == "" {
			return fmt.Errorf("auth.oauth: flow: device requires device_auth_url")
		}
	default:
		return fmt.Errorf("auth.oauth: unknown flow %q (want browser or device)", o.Flow)
	}
	return nil
}
