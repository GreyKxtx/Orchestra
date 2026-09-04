package config

import (
	"fmt"
	"strings"
)

// Warnings reports configuration that loads cleanly but will not do what it
// looks like it does.
//
// These are not errors — the run proceeds — but a silent no-op is worse than a
// loud one. A field run spent fifty-two sessions with an embedding provider
// configured and no embedding model: semantic_search was therefore never
// registered, memory_search quietly degraded to substring matching, and
// nothing anywhere said so.
func (c *ProjectConfig) Warnings() []string {
	if c == nil {
		return nil
	}
	var out []string

	if strings.TrimSpace(c.Embed.Model) == "" {
		if provider := strings.TrimSpace(c.Embed.Provider); provider != "" {
			out = append(out, fmt.Sprintf(
				"embed.provider is %q but embed.model is empty — semantic_search will not be offered to the model and memory_search falls back to substring matching. Set embed.model, or drop embed.provider.",
				provider))
		}
	}

	return out
}

// FprintWarnings writes Warnings() to w, one per line, prefixed for a log.
// Returns how many were written so callers can stay quiet when there are none.
func (c *ProjectConfig) FprintWarnings(w interface{ Write([]byte) (int, error) }) int {
	warns := c.Warnings()
	for _, warn := range warns {
		fmt.Fprintf(w, "orchestra: config warning: %s\n", warn)
	}
	return len(warns)
}
