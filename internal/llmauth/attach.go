package llmauth

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/llm"
)

// Attach resolves cfg.Auth into cfg.TokenSource. It is a no-op when the
// provider has no auth block.
//
// A provider configured for OAuth but not yet logged into still attaches
// successfully: the error is deferred into the token source itself, so it
// surfaces at request time where it can name the login command. Failing here
// instead would break every unrelated command at config load -- including
// the `orchestra auth login` that fixes it.
func Attach(ctx context.Context, name string, cfg *llm.LLMConfig) error {
	if cfg == nil || cfg.Auth == nil {
		return nil
	}
	if err := cfg.Auth.Validate(); err != nil {
		return fmt.Errorf("provider %q: %w", name, err)
	}
	if len(cfg.Auth.TokenCommand) > 0 {
		cfg.TokenSource = newCommandTokenSource(name, cfg.Auth.TokenCommand, cfg.Auth.EffectiveTokenCommandTTL())
		return nil
	}

	src, err := TokenSourceFor(ctx, name, *cfg.Auth.OAuth)
	if err != nil {
		deferred := err
		cfg.TokenSource = func() (string, error) { return "", deferred }
		return nil
	}
	cfg.TokenSource = src
	return nil
}
