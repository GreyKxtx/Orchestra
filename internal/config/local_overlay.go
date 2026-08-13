package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// .orchestra.local.yml is a personal, gitignored overlay merged on top of
// .orchestra.yml at load time. It is meant for secrets (llm.api_key,
// providers.*.api_key) and personal overrides (local api_base) that must not
// land in the shared, committed config.
//
// Ownership rule: any key present in the overlay is owned by the overlay.
// Load deep-merges overlay values over the main file; Save restores those
// keys to their current on-disk values (or drops them) before writing, so a
// settings round-trip through the UI can never leak a local secret into
// .orchestra.yml.

// LocalOverlayName is the filename of the personal config overlay, resolved
// next to .orchestra.yml.
const LocalOverlayName = ".orchestra.local.yml"

func localOverlayPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), LocalOverlayName)
}

// mergeLocalOverlay deep-merges the local overlay (if present) over the raw
// main-config YAML and returns bytes ready for unmarshalling. Returns
// mainData unchanged when no overlay exists.
func mergeLocalOverlay(configPath string, mainData []byte) ([]byte, error) {
	overData, err := os.ReadFile(localOverlayPath(configPath))
	if err != nil {
		if os.IsNotExist(err) {
			return mainData, nil
		}
		return nil, fmt.Errorf("read %s: %w", LocalOverlayName, err)
	}

	var base, over map[string]any
	if err := yaml.Unmarshal(mainData, &base); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	if err := yaml.Unmarshal(overData, &over); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", LocalOverlayName, err)
	}
	if len(over) == 0 {
		return mainData, nil
	}
	if base == nil {
		base = map[string]any{}
	}
	merged := deepMergeMaps(base, over)
	out, err := yaml.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("merge %s: %w", LocalOverlayName, err)
	}
	return out, nil
}

// deepMergeMaps merges src into dst recursively: nested mappings merge
// key-by-key, everything else (scalars, sequences) is replaced by src.
func deepMergeMaps(dst, src map[string]any) map[string]any {
	for k, v := range src {
		if sv, ok := v.(map[string]any); ok {
			if dv, ok2 := dst[k].(map[string]any); ok2 {
				dst[k] = deepMergeMaps(dv, sv)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}

// maskLocalOverlay prepares config bytes for writing to configPath: every
// leaf key defined in the overlay is restored to its current on-disk value in
// the main file (or removed when the main file never had it). Returns data
// unchanged when no overlay exists.
func maskLocalOverlay(configPath string, data []byte) ([]byte, error) {
	overData, err := os.ReadFile(localOverlayPath(configPath))
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, fmt.Errorf("read %s: %w", LocalOverlayName, err)
	}

	var over map[string]any
	if err := yaml.Unmarshal(overData, &over); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", LocalOverlayName, err)
	}
	if len(over) == 0 {
		return data, nil
	}

	var next map[string]any
	if err := yaml.Unmarshal(data, &next); err != nil {
		return nil, fmt.Errorf("mask overlay: parse new config: %w", err)
	}
	if next == nil {
		next = map[string]any{}
	}

	// Best-effort: the main file may not exist yet (first save).
	var base map[string]any
	if baseData, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(baseData, &base)
	}

	restoreOverlayLeaves(next, base, over)

	out, err := yaml.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("mask overlay: %w", err)
	}
	return out, nil
}

// restoreOverlayLeaves walks the overlay tree; for every leaf key it resets
// dst to the value from base (the previous on-disk main config), or deletes
// the key when base never defined it. Nested maps recurse so that e.g.
// providers.openrouter.api_key masks only the api_key leaf.
func restoreOverlayLeaves(dst, base, over map[string]any) {
	for k, ov := range over {
		if om, isMap := ov.(map[string]any); isMap {
			dm, ok := dst[k].(map[string]any)
			if !ok {
				continue
			}
			var bm map[string]any
			if base != nil {
				bm, _ = base[k].(map[string]any)
			}
			restoreOverlayLeaves(dm, bm, om)
			if len(dm) == 0 {
				delete(dst, k)
			}
			continue
		}
		if base != nil {
			if bv, ok := base[k]; ok {
				dst[k] = bv
				continue
			}
		}
		delete(dst, k)
	}
}
