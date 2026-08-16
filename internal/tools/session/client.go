package session

import (
	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/memory"
)

// Client executes session-scoped tools (memory, runtime traces).
type Client struct {
	Root      string
	sessionID func() string
	memoryCfg func() memory.Config
	embedCfg  func() config.EmbedConfig
	ckgStore  func() *ckg.Store
}

// NewClient wires session tools.
func NewClient(
	root string,
	sessionID func() string,
	memoryCfg func() memory.Config,
	embedCfg func() config.EmbedConfig,
	ckgStore func() *ckg.Store,
) *Client {
	return &Client{
		Root:      root,
		sessionID: sessionID,
		memoryCfg: memoryCfg,
		embedCfg:  embedCfg,
		ckgStore:  ckgStore,
	}
}

func (c *Client) sid() string {
	if c == nil || c.sessionID == nil {
		return ""
	}
	return c.sessionID()
}

func (c *Client) memCfg() memory.Config {
	if c == nil || c.memoryCfg == nil {
		return memory.DefaultConfig()
	}
	cfg := c.memoryCfg()
	cfg.Normalize()
	return cfg
}

func (c *Client) embedConfig() config.EmbedConfig {
	if c == nil || c.embedCfg == nil {
		return config.EmbedConfig{}
	}
	return c.embedCfg()
}

func (c *Client) store() *ckg.Store {
	if c == nil || c.ckgStore == nil {
		return nil
	}
	return c.ckgStore()
}
