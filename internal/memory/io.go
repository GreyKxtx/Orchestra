package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) readLayerRaw(layer string) string {
	switch layer {
	case layerOrchestra:
		if s.workspaceRoot == "" {
			return ""
		}
		data, err := os.ReadFile(filepath.Join(s.workspaceRoot, "ORCHESTRA.md"))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	case layerGlobal:
		if !s.cfg.GlobalEnabled {
			return ""
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		data, err := os.ReadFile(filepath.Join(home, ".orchestra", "memory.md"))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	default:
		return ""
	}
}

func (s *Store) List() []LayerSummary {
	var out []LayerSummary
	if raw := s.readLayerRaw(layerOrchestra); raw != "" {
		out = append(out, LayerSummary{
			Layer: layerOrchestra, Path: "ORCHESTRA.md", Bytes: len(raw),
			Preview: preview(raw, 120),
		})
	}
	if s.cfg.SessionEnabled && s.sessionID != "" {
		if raw := s.readSessionFile(0); raw != "" {
			out = append(out, LayerSummary{
				Layer: layerSession, Path: relPath(s.sessionFilePath(), s.workspaceRoot),
				Bytes: len(raw), Preview: preview(raw, 120),
			})
		}
	}
	memDir := filepath.Join(s.workspaceRoot, ".orchestra", "memory")
	if entries, err := os.ReadDir(memDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			f := filepath.Join(memDir, e.Name())
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			raw := strings.TrimSpace(string(data))
			if raw == "" {
				continue
			}
			out = append(out, LayerSummary{
				Layer: layerRepo, Path: relPath(f, s.workspaceRoot),
				Bytes: len(raw), Preview: preview(raw, 120),
			})
		}
	}
	if s.cfg.GlobalEnabled {
		if raw := s.readLayerRaw(layerGlobal); raw != "" {
			out = append(out, LayerSummary{
				Layer: layerGlobal, Path: "~/.orchestra/memory.md",
				Bytes: len(raw), Preview: preview(raw, 120),
			})
		}
	}
	return out
}

// Read loads memory by layer or path. Empty layer+path lists all layers (metadata only).
func (s *Store) Read(layer, path string, maxBytes int) ReadResult {
	layer = strings.ToLower(strings.TrimSpace(layer))
	path = strings.TrimSpace(path)
	if maxBytes <= 0 {
		maxBytes = s.cfg.InjectBytes()
	}

	if layer == "" && path == "" {
		return ReadResult{Entries: s.List()}
	}

	if path != "" {
		content, resolvedLayer, err := s.readByPath(path, maxBytes)
		if err != nil {
			return ReadResult{Content: "error: " + err.Error()}
		}
		truncated := len(content) >= maxBytes
		return ReadResult{Layer: resolvedLayer, Path: path, Content: content, Truncated: truncated}
	}

	switch layer {
	case layerOrchestra, "project":
		raw := s.sliceLayer(layerOrchestra, maxBytes)
		return ReadResult{Layer: layerOrchestra, Path: "ORCHESTRA.md", Content: raw, Truncated: len(raw) >= maxBytes}
	case layerSession:
		if s.sessionID == "" {
			return ReadResult{Content: "no active session_id"}
		}
		raw := s.readSessionFile(maxBytes)
		return ReadResult{Layer: layerSession, Path: relPath(s.sessionFilePath(), s.workspaceRoot), Content: raw}
	case layerRepo:
		raw := s.sliceRepoMemory(maxBytes)
		return ReadResult{Layer: layerRepo, Path: ".orchestra/memory/", Content: raw, Truncated: len(raw) >= maxBytes}
	case layerGlobal:
		raw := s.sliceLayer(layerGlobal, maxBytes)
		return ReadResult{Layer: layerGlobal, Path: "~/.orchestra/memory.md", Content: raw}
	case "all":
		return ReadResult{Content: s.tieredInject(maxBytes), Truncated: true}
	default:
		return ReadResult{Content: fmt.Sprintf("unknown layer %q (want orchestra|session|repo|global|all)", layer)}
	}
}

func (s *Store) readByPath(path string, maxBytes int) (content, layer string, err error) {
	path = filepath.ToSlash(path)
	switch {
	case path == "ORCHESTRA.md":
		layer = layerOrchestra
		content = s.sliceLayer(layerOrchestra, maxBytes)
	case strings.HasPrefix(path, ".orchestra/memory/"):
		abs := filepath.Join(s.workspaceRoot, filepath.FromSlash(path))
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			return "", layerRepo, readErr
		}
		layer = layerRepo
		content = strings.TrimSpace(string(data))
		if len(content) > maxBytes {
			content = tailBytes(content, maxBytes)
		}
	case path == "~/.orchestra/memory.md":
		layer = layerGlobal
		content = s.sliceLayer(layerGlobal, maxBytes)
	default:
		return "", "", fmt.Errorf("path not in memory store: %s", path)
	}
	return content, layer, nil
}

