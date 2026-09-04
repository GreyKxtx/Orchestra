package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// A user-level config at ~/.orchestra/config.yml holds the settings that are
// the same in every checkout — provider endpoints, API keys, tier layout,
// editor preferences. Without it each new project starts from nothing and the
// same credentials get retyped (and committed) per repo.
//
// Precedence, lowest first:
//
//	~/.orchestra/config.yml   user-wide defaults
//	<project>/.orchestra.yml  the shared, committed project config
//	<project>/.orchestra.local.yml  machine-specific overlay (secrets)
//
// Ownership mirrors the local overlay: any key the global file defines is
// owned by it, so Save restores that key to its previous on-disk project value
// instead of writing the inherited one. .orchestra.yml is committed, and a
// settings round-trip through the TUI must not publish a key the user put in
// their home directory.

// GlobalConfigName is the file name of the user-level config, resolved under
// ~/.orchestra/.
const GlobalConfigName = "config.yml"

// GlobalConfigPath returns the user-level config path, or "" when the home
// directory cannot be resolved.
func GlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".orchestra", GlobalConfigName)
}

// readGlobalConfigMap loads the user-level config as a generic map. Returns a
// nil map (and no error) when there is no global config.
func readGlobalConfigMap() (map[string]any, error) {
	path := GlobalConfigPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	// project_root is meaningless outside a project: inherited, it would point
	// every checkout at one directory and the agent would edit the wrong tree.
	delete(m, "project_root")
	return m, nil
}

// mergeGlobalConfig deep-merges the project config over the user-level one and
// returns bytes ready for unmarshalling. Returns projectData unchanged when
// there is no global config.
func mergeGlobalConfig(projectData []byte) ([]byte, error) {
	base, err := readGlobalConfigMap()
	if err != nil {
		return nil, err
	}
	if len(base) == 0 {
		return projectData, nil
	}
	var over map[string]any
	if err := yaml.Unmarshal(projectData, &over); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	merged := deepMergeMaps(base, over)
	out, err := yaml.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("merge %s: %w", GlobalConfigName, err)
	}
	return out, nil
}

// maskGlobalConfig resets every leaf the global config defines to its current
// on-disk value in the project file (or removes it when the project never had
// it), so inherited values are never persisted into the committed config.
func maskGlobalConfig(configPath string, data []byte) ([]byte, error) {
	over, err := readGlobalConfigMap()
	if err != nil {
		return nil, err
	}
	if len(over) == 0 {
		return data, nil
	}

	var next map[string]any
	if err := yaml.Unmarshal(data, &next); err != nil {
		return nil, fmt.Errorf("mask global config: parse new config: %w", err)
	}
	if next == nil {
		next = map[string]any{}
	}

	// Best-effort: the project file may not exist yet (first save).
	var base map[string]any
	if baseData, readErr := os.ReadFile(configPath); readErr == nil {
		_ = yaml.Unmarshal(baseData, &base)
	}

	restoreOverlayLeaves(next, base, over)

	out, err := yaml.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("mask global config: %w", err)
	}
	return out, nil
}
