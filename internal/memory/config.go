package memory

import "strings"

// Inject mode controls how much memory is eagerly injected into the system prompt.
const (
	ModeEager  = "eager"  // full tiered inject up to inject_kb (best for small local models)
	ModeLazy   = "lazy"   // minimal header + memory_read on demand (saves context)
	ModeHybrid = "hybrid" // ORCHESTRA + session + recent agent entries; rest via memory_read
)

// Config controls project/session/global memory behaviour.
type Config struct {
	// InjectKB caps eager <project_memory> injection (default 8).
	InjectKB int `yaml:"inject_kb"`
	// LazyKB caps ORCHESTRA.md injected into fs.read lazy reminders (default 4).
	LazyKB int `yaml:"lazy_kb"`
	// Mode: eager | lazy | hybrid (default hybrid).
	Mode string `yaml:"mode"`
	// GlobalEnabled includes ~/.orchestra/memory.md in loads.
	GlobalEnabled bool `yaml:"global_enabled"`
	// SessionEnabled includes per-session file .orchestra/memory/sessions/<id>.md.
	SessionEnabled bool `yaml:"session_enabled"`
	// MaxAgentKB triggers compaction of .orchestra/memory/agent.md (keep recent tail).
	MaxAgentKB int `yaml:"max_agent_kb"`
}

// DefaultConfig returns production defaults tuned for small-context local LLMs.
func DefaultConfig() Config {
	return Config{
		InjectKB:       8,
		LazyKB:         4,
		Mode:           ModeHybrid,
		GlobalEnabled:  true,
		SessionEnabled: true,
		MaxAgentKB:     128,
	}
}

// Normalize fills zero values and canonicalizes mode.
func (c *Config) Normalize() {
	def := DefaultConfig()
	if c.InjectKB <= 0 {
		c.InjectKB = def.InjectKB
	}
	if c.LazyKB <= 0 {
		c.LazyKB = def.LazyKB
	}
	if strings.TrimSpace(c.Mode) == "" {
		c.Mode = def.Mode
	}
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	switch c.Mode {
	case ModeEager, ModeLazy, ModeHybrid:
	default:
		c.Mode = ModeHybrid
	}
	if c.MaxAgentKB <= 0 {
		c.MaxAgentKB = def.MaxAgentKB
	}
}

// InjectBytes returns the eager inject byte cap.
func (c Config) InjectBytes() int {
	return c.InjectKB * 1024
}

// LazyBytes returns the lazy ORCHESTRA.md reminder cap.
func (c Config) LazyBytes() int {
	return c.LazyKB * 1024
}

// MaxAgentBytes returns the agent.md compaction threshold.
func (c Config) MaxAgentBytes() int {
	return c.MaxAgentKB * 1024
}
