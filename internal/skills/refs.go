package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RefsDir holds reusable text blocks (`@refs/<name>` macros) that skills
// can embed verbatim to avoid duplicating shared philosophy/conventions.
const RefsDir = ".orchestra/refs"

// refMacro matches `@refs/<name>` where name is a sequence of ASCII
// word chars / dashes. The macro must be standalone (preceded by start
// of line or whitespace) so that `email@refs/x` in regular prose is
// not accidentally expanded.
var refMacro = regexp.MustCompile(`(^|[\s])@refs/([A-Za-z0-9_\-]+)`)

const maxRefDepth = 8

// DiscoverRefs returns a name→body map for all reusable reference blocks
// found under packsRoot subdirs, the user-global refs dir, and the
// project refs dir. Project overrides user, user overrides packs.
// Missing dirs are not an error. The reference body is the raw file
// content with no frontmatter parsing (refs are plain text fragments).
func DiscoverRefs(projectRoot string) (map[string]string, error) {
	var userDir, packsRoot string
	if home, err := os.UserHomeDir(); err == nil {
		userDir = filepath.Join(home, RefsDir)
		packsRoot = filepath.Join(home, PacksDir)
	}
	return DiscoverRefsFromAll(packsRoot, userDir, filepath.Join(projectRoot, RefsDir))
}

// DiscoverRefsFromAll is the testable form: any path may be "" to skip.
// Each subdir of packsRoot is treated as one pack source; refs inside
// `<pack>/refs/*.md` (if present) are loaded with pack precedence.
func DiscoverRefsFromAll(packsRoot, userDir, projectDir string) (map[string]string, error) {
	out := make(map[string]string)
	if packsRoot != "" {
		entries, err := os.ReadDir(packsRoot)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read packs root %s: %w", packsRoot, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			refsSub := filepath.Join(packsRoot, e.Name(), "refs")
			if err := mergeRefsDir(out, refsSub); err != nil {
				return nil, err
			}
		}
	}
	if err := mergeRefsDir(out, userDir); err != nil {
		return nil, err
	}
	if err := mergeRefsDir(out, projectDir); err != nil {
		return nil, err
	}
	return out, nil
}

func mergeRefsDir(dst map[string]string, dir string) error {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read refs dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read ref %s: %w", e.Name(), err)
		}
		dst[name] = strings.TrimRight(string(data), "\r\n")
	}
	return nil
}

// ExpandRefs replaces `@refs/<name>` macros in body with the matching
// entry from refs. Expansion is recursive (refs may embed other refs)
// up to maxRefDepth, after which an error is returned. Unknown refs
// produce an error so typos surface loudly.
func ExpandRefs(body string, refs map[string]string) (string, error) {
	return expandRefsDepth(body, refs, 0, nil)
}

func expandRefsDepth(body string, refs map[string]string, depth int, stack []string) (string, error) {
	if depth > maxRefDepth {
		return "", fmt.Errorf("refs: max expansion depth %d exceeded (cycle?) stack=%v", maxRefDepth, stack)
	}
	var firstErr error
	out := refMacro.ReplaceAllStringFunc(body, func(match string) string {
		sm := refMacro.FindStringSubmatch(match)
		prefix, name := sm[1], sm[2]
		val, ok := refs[name]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("refs: unknown reference %q (known: %s)", name, strings.Join(sortedKeys(refs), ", "))
			}
			return match
		}
		for _, prev := range stack {
			if prev == name {
				if firstErr == nil {
					firstErr = fmt.Errorf("refs: cycle detected expanding %q (stack=%v)", name, stack)
				}
				return match
			}
		}
		expanded, err := expandRefsDepth(val, refs, depth+1, append(stack, name))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return match
		}
		return prefix + expanded
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// PrepareBody is the canonical pipeline applied to a skill body before
// it becomes the agent's system prompt: refs expansion first (so a ref
// containing $ARGUMENTS still gets substituted), then $ARGUMENTS
// replacement. Pass refs=nil to skip ref expansion (and fail closed if
// a @refs/ macro is present in the body).
func PrepareBody(body, arguments string, refs map[string]string) (string, error) {
	expanded, err := ExpandRefs(body, refs)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(expanded, "$ARGUMENTS", arguments), nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
