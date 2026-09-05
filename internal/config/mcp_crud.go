package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ownMCPServers reads just the mcp.servers list explicitly present in
// configPath's own YAML — no local/global overlay merge, no .mcp.json
// merge. Best-effort: returns nil on any read or parse failure, matching
// the other config layers' "missing/broken base file" handling.
func ownMCPServers(configPath string) []MCPServerConfig {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var prior ProjectConfig
	if yaml.Unmarshal(raw, &prior) != nil {
		return nil
	}
	return prior.MCP.Servers
}

// SetMCPServer adds srv to .orchestra.yml's mcp.servers, replacing any
// existing entry with the same name. Goes through the same Load/mutate/Save
// path as every other programmatic edit (the TUI and VS Code settings
// panels), so it gets the same secret- and .mcp.json-masking on write.
func SetMCPServer(configPath string, srv MCPServerConfig) error {
	if err := validateMCPTransport(0, srv.Name, srv); err != nil {
		return err
	}
	cfg, err := Load(configPath)
	if err != nil {
		return err
	}
	replaced := false
	for i, s := range cfg.MCP.Servers {
		if s.Name == srv.Name {
			cfg.MCP.Servers[i] = srv
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.MCP.Servers = append(cfg.MCP.Servers, srv)
	}
	return Save(configPath, cfg)
}

// RemoveMCPServer removes the named server from .orchestra.yml's own
// mcp.servers. Only a server this file actually lists is removable —
// reporting removed=true for a name that lives in .mcp.json would be a
// lie, since that file is untouched and the next Load() would bring the
// server right back.
func RemoveMCPServer(configPath, name string) (removed bool, err error) {
	own := ownMCPServers(configPath)
	found := false
	for _, s := range own {
		if s.Name == name {
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}

	cfg, err := Load(configPath)
	if err != nil {
		return false, err
	}
	filtered := cfg.MCP.Servers[:0]
	for _, s := range cfg.MCP.Servers {
		if s.Name != name {
			filtered = append(filtered, s)
		}
	}
	cfg.MCP.Servers = filtered
	if err := Save(configPath, cfg); err != nil {
		return false, err
	}
	return true, nil
}

// GetMCPServer looks up one server by name in the merged view — both
// .orchestra.yml's own servers and anything picked up from .mcp.json — since
// this is inspection, not an edit, and an operator asking "what will
// Orchestra actually use for X" wants the answer regardless of which file
// defines it.
func GetMCPServer(configPath, name string) (MCPServerConfig, bool, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return MCPServerConfig{}, false, fmt.Errorf("load config: %w", err)
	}
	for _, s := range cfg.MCP.Servers {
		if s.Name == name {
			return s, true, nil
		}
	}
	return MCPServerConfig{}, false, nil
}
