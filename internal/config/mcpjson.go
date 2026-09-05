package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// MCPJSONName is the file name of the cross-tool MCP server manifest: the
// format Claude Code, Cursor and other MCP clients already read and write.
// Honoring it means a server someone configured for another agent works
// here without retyping it into .orchestra.yml.
const MCPJSONName = ".mcp.json"

// mcpJSONFile is the on-disk shape of .mcp.json.
type mcpJSONFile struct {
	MCPServers map[string]mcpJSONServer `json:"mcpServers"`
}

// mcpJSONServer is one entry — the union of the stdio and remote shapes
// every MCP client in this ecosystem uses. Exactly one of Command/URL is
// expected to be set; validateMCPTransport enforces that the same way it
// does for .orchestra.yml servers, once merged.
type mcpJSONServer struct {
	Command  string            `json:"command,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	URL      string            `json:"url,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
}

// LoadMCPJSON reads <projectRoot>/.mcp.json and returns its servers as
// MCPServerConfig. Returns (nil, nil) when the file does not exist — most
// projects won't have one, and that is not an error. A malformed file *is*
// an error: unlike a log line, a hand-edited config the user believes is
// active deserves to fail loudly rather than be silently skipped.
func LoadMCPJSON(projectRoot string) ([]MCPServerConfig, error) {
	path := filepath.Join(projectRoot, MCPJSONName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", MCPJSONName, err)
	}
	var f mcpJSONFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", MCPJSONName, err)
	}
	if len(f.MCPServers) == 0 {
		return nil, nil
	}
	out := make([]MCPServerConfig, 0, len(f.MCPServers))
	for name, s := range f.MCPServers {
		cfg := MCPServerConfig{
			Name:     name,
			URL:      s.URL,
			Env:      s.Env,
			Headers:  s.Headers,
			Disabled: s.Disabled,
		}
		if s.Command != "" {
			cfg.Command = append([]string{s.Command}, s.Args...)
		}
		out = append(out, cfg)
	}
	return out, nil
}

// MergeMCPServers combines the project's own mcp.servers (from
// .orchestra.yml, which Orchestra itself reads and writes) with servers
// discovered in .mcp.json (someone else's file, honored but not owned). A
// name already defined in own wins outright — .orchestra.yml is what an
// Orchestra user edits when they want to change a server's settings, and a
// silently-overridden field there would be confusing.
func MergeMCPServers(own, fromMCPJSON []MCPServerConfig) []MCPServerConfig {
	if len(fromMCPJSON) == 0 {
		return own
	}
	seen := make(map[string]bool, len(own))
	for _, s := range own {
		seen[s.Name] = true
	}
	merged := append([]MCPServerConfig(nil), own...)
	for _, s := range fromMCPJSON {
		if seen[s.Name] {
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

// maskMCPJSONServers strips mcp.servers entries that exist only because
// Load() merged them in from .mcp.json, before data is written to
// configPath. .mcp.json is a foreign file Orchestra reads but does not
// own — exactly like the local and global config layers — and without this,
// every programmatic Save() (mcp add/remove, the TUI/VS Code settings
// round-trip) would silently copy someone else's server list into the
// committed .orchestra.yml.
//
// A name already listed in .orchestra.yml before this save is explicit —
// the user (or a prior `mcp add`) put it there on purpose — and is kept
// even if .mcp.json also defines it.
func maskMCPJSONServers(configPath string, data []byte) ([]byte, error) {
	fromJSON, err := LoadMCPJSON(filepath.Dir(configPath))
	if err != nil || len(fromJSON) == 0 {
		return data, nil
	}
	jsonOwned := make(map[string]bool, len(fromJSON))
	for _, s := range fromJSON {
		jsonOwned[s.Name] = true
	}

	explicit := map[string]bool{}
	for _, s := range ownMCPServers(configPath) {
		explicit[s.Name] = true
	}

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return data, nil // best-effort, matches maskGlobalConfig/maskLocalOverlay
	}
	mcpSection, _ := doc["mcp"].(map[string]any)
	if mcpSection == nil {
		return data, nil
	}
	servers, _ := mcpSection["servers"].([]any)
	if servers == nil {
		return data, nil
	}
	kept := make([]any, 0, len(servers))
	for _, entry := range servers {
		if m, ok := entry.(map[string]any); ok {
			name, _ := m["name"].(string)
			if jsonOwned[name] && !explicit[name] {
				continue
			}
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		delete(mcpSection, "servers")
		if len(mcpSection) == 0 {
			delete(doc, "mcp")
		}
	} else {
		mcpSection["servers"] = kept
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return data, nil
	}
	return out, nil
}
