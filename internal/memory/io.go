package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/lessons"
)

// FallbackNames lists the project-instruction filenames Orchestra reads, in
// priority order (ORCHESTRA.md first). Exported so `orchestra init` can
// check whether one already exists before writing a stub — ORCHESTRA.md
// wins this fallback at runtime, so an empty stub over a real AGENTS.md
// would shadow it instead of adding to it.
var FallbackNames = append([]string(nil), orchestraFallbackNames...)

// FindProjectInstructions returns the content and filename of the first
// existing, non-empty candidate among FallbackNames in dir, or ("", "")
// when none exist.
func FindProjectInstructions(dir string) (content, name string) {
	return readOrchestraFile(dir)
}

// orchestraFallbackNames is the candidate chain for project instructions,
// checked in order. Most repos already carry one of these for another
// agent by the time Orchestra shows up; ignoring it means starting blind
// on a repo that already has guidance — the field-run demo project had
// none of ORCHESTRA.md and got no project memory at all as a result.
var orchestraFallbackNames = []string{"ORCHESTRA.md", "AGENTS.md", "CLAUDE.md", ".cursorrules"}

// localOrchestraFile is a personal, gitignored overlay (`orchestra init`
// adds it to .gitignore) — "my" rules layered onto the team's project
// instructions, the same relationship CLAUDE.local.md has to CLAUDE.md.
const localOrchestraFile = "ORCHESTRA.local.md"

// readOrchestraFile returns the content and filename of the first existing,
// non-empty candidate in dir, with ORCHESTRA.local.md (if present) appended,
// or ("", "") when nothing at all exists.
func readOrchestraFile(dir string) (content, name string) {
	for _, n := range orchestraFallbackNames {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		raw := strings.TrimSpace(string(data))
		if raw == "" {
			continue
		}
		return appendLocalOrchestra(dir, expandImports(raw, dir)), n
	}
	// No team file at all — a personal ORCHESTRA.local.md still counts as
	// project instructions instead of being silently dropped on a repo that
	// doesn't (yet) have a shared one.
	if local := readLocalOrchestra(dir); local != "" {
		return local, localOrchestraFile
	}
	return "", ""
}

// readLocalOrchestra returns ORCHESTRA.local.md's trimmed content (with its
// own @import lines expanded), or "".
func readLocalOrchestra(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, localOrchestraFile))
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return ""
	}
	return expandImports(raw, dir)
}

// appendLocalOrchestra layers ORCHESTRA.local.md onto raw, when present.
func appendLocalOrchestra(dir, raw string) string {
	local := readLocalOrchestra(dir)
	if local == "" {
		return raw
	}
	return raw + "\n\n" + local
}

// orchestraFileName reports which file backs the orchestra layer in the
// workspace root — "ORCHESTRA.md" unless a fallback is what's actually
// there, so List()/Read() can tell the operator which file is really
// feeding the agent instead of always naming the one that may not exist.
func (s *Store) orchestraFileName() string {
	if s.workspaceRoot == "" {
		return "ORCHESTRA.md"
	}
	if _, name := readOrchestraFile(s.workspaceRoot); name != "" {
		return name
	}
	return "ORCHESTRA.md"
}

func (s *Store) readLayerRaw(layer string) string {
	switch layer {
	case layerOrchestra:
		if s.workspaceRoot == "" {
			return ""
		}
		content, _ := readOrchestraFile(s.workspaceRoot)
		return content
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
			Layer: layerOrchestra, Path: s.orchestraFileName(), Bytes: len(raw),
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
	if entries, err := os.ReadDir(filepath.Join(s.workspaceRoot, filepath.FromSlash(lessons.RelDir))); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			f := filepath.Join(s.workspaceRoot, filepath.FromSlash(lessons.RelDir), e.Name())
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			raw := strings.TrimSpace(string(data))
			if raw == "" {
				continue
			}
			out = append(out, LayerSummary{
				Layer: layerLessons, Path: relPath(f, s.workspaceRoot),
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
		return ReadResult{Layer: layerOrchestra, Path: s.orchestraFileName(), Content: raw, Truncated: len(raw) >= maxBytes}
	case layerSession:
		if s.sessionID == "" {
			return ReadResult{Content: "no active session_id"}
		}
		raw := s.readSessionFile(maxBytes)
		return ReadResult{Layer: layerSession, Path: relPath(s.sessionFilePath(), s.workspaceRoot), Content: raw}
	case layerRepo:
		raw := s.sliceRepoMemory(maxBytes, true)
		return ReadResult{Layer: layerRepo, Path: ".orchestra/memory/", Content: raw, Truncated: len(raw) >= maxBytes}
	case layerLessons:
		raw := s.sliceLessonsMemory(maxBytes)
		rel := filepath.ToSlash(lessons.RelDir) + "/"
		return ReadResult{Layer: layerLessons, Path: rel, Content: raw, Truncated: len(raw) >= maxBytes}
	case layerGlobal:
		raw := s.sliceLayer(layerGlobal, maxBytes)
		return ReadResult{Layer: layerGlobal, Path: "~/.orchestra/memory.md", Content: raw}
	case "all":
		// The escape hatch hybrid points the model at: always every layer.
		return ReadResult{Content: s.tieredInject(maxBytes, fullScope()), Truncated: true}
	default:
		return ReadResult{Content: fmt.Sprintf("unknown layer %q (want orchestra|session|repo|lessons|global|all)", layer)}
	}
}

func (s *Store) sliceLessonsMemory(maxBytes int) string {
	if maxBytes <= 0 || s.workspaceRoot == "" {
		return ""
	}
	dir := filepath.Join(s.workspaceRoot, filepath.FromSlash(lessons.RelDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var parts []string
	remaining := maxBytes
	for _, name := range names {
		dept := strings.TrimSuffix(name, ".md")
		chunk := lessons.Tail(s.workspaceRoot, dept, remaining)
		if chunk == "" {
			continue
		}
		parts = append(parts, chunk)
		remaining -= len(chunk)
		if remaining <= 0 {
			break
		}
	}
	return strings.Join(parts, "\n\n")
}

func (s *Store) readByPath(path string, maxBytes int) (content, layer string, err error) {
	path = filepath.ToSlash(path)
	switch {
	case path == "ORCHESTRA.md" || path == "AGENTS.md" || path == "CLAUDE.md" || path == ".cursorrules" || path == localOrchestraFile:
		layer = layerOrchestra
		content = s.sliceLayer(layerOrchestra, maxBytes)
	case strings.HasPrefix(path, ".orchestra/memory/"):
		if strings.HasPrefix(path, lessons.RelDir+"/") {
			abs := filepath.Join(s.workspaceRoot, filepath.FromSlash(path))
			data, readErr := os.ReadFile(abs)
			if readErr != nil {
				return "", layerLessons, readErr
			}
			layer = layerLessons
			content = strings.TrimSpace(string(data))
			if len(content) > maxBytes {
				content = tailBytes(content, maxBytes)
			}
			break
		}
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
