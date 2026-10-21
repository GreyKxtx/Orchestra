package config

import (
	neturl "net/url"
	"strings"
)

// InitPlaceholderModel is the model `orchestra init` used to write into every
// new project, whether or not anything served it.
const InitPlaceholderModel = "qwen2.5-coder-7b"

// dropInitPlaceholder forgets the placeholder where it cannot be a choice:
// on an endpoint that is not this machine's. A project was left on Anthropic
// with qwen2.5-coder-7b, because Settings carried the placeholder over to the
// provider picked after it, and every turn asked Anthropic for a model it
// does not have. The model is then unset, as one never picked is; a save
// writes it out. On localhost it stays: LM Studio serving that model is a
// real setup.
func (c *ProjectConfig) dropInitPlaceholder() {
	if placeholderOffMachine(c.LLM) {
		c.LLM.Model = ""
	}
	for name, p := range c.Providers {
		if placeholderOffMachine(p) {
			p.Model = ""
			c.Providers[name] = p
		}
	}
}

func placeholderOffMachine(l LLMConfig) bool {
	if !strings.EqualFold(strings.TrimSpace(l.Model), InitPlaceholderModel) {
		return false
	}
	base := strings.TrimSpace(l.APIBase)
	if base == "" {
		return false
	}
	u, err := neturl.Parse(base)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return !isLoopbackHost(u.Hostname())
}
