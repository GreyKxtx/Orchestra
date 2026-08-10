package git

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const worktreeRegistryVersion = 1

// WorktreeEntry describes one git worktree (from porcelain list + orchestra metadata).
type WorktreeEntry struct {
	Path       string `json:"path"`
	HEAD       string `json:"head,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Detached   bool   `json:"detached,omitempty"`
	Locked     bool   `json:"locked,omitempty"`
	Prunable   bool   `json:"prunable,omitempty"`
	Bare       bool   `json:"bare,omitempty"`
	Managed    bool   `json:"managed,omitempty"`
	Name       string `json:"name,omitempty"`
	Main       bool   `json:"main,omitempty"`
}

type registryFile struct {
	Version int                 `json:"version"`
	Entries []registryEntry     `json:"entries"`
}

type registryEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"` // workspace-relative from main repo root
	Branch    string    `json:"branch"`
	CreatedAt time.Time `json:"created_at"`
}

// MainRepoRoot returns the primary worktree root for any checkout inside the repo.
func MainRepoRoot(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("empty directory")
	}
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	common := strings.TrimSpace(out.String())
	if common == "" {
		return "", fmt.Errorf("git-common-dir is empty")
	}
	if !filepath.IsAbs(common) {
		top, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return "", fmt.Errorf("rev-parse --show-toplevel: %w", err)
		}
		topDir := strings.TrimSpace(string(top))
		common = filepath.Join(topDir, common)
	}
	common = filepath.Clean(common)
	base := filepath.Dir(common)
	if strings.EqualFold(filepath.Base(common), ".git") {
		base = filepath.Clean(base)
		if resolved, err := filepath.EvalSymlinks(base); err == nil {
			base = resolved
		}
		return base, nil
	}
	return "", fmt.Errorf("unexpected git-common-dir: %s", common)
}

// IsSafeWorktreeName validates orchestra-managed worktree names.
func IsSafeWorktreeName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, c := range name {
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_'
		if !ok {
			return false
		}
		if i == 0 && c >= '0' && c <= '9' {
			return false
		}
	}
	return true
}

func registryPath(mainRoot string) string {
	return filepath.Join(mainRoot, ".orchestra", "worktrees.json")
}

func worktreesBase(mainRoot string) string {
	return filepath.Join(mainRoot, ".orchestra", "worktrees")
}

func loadRegistry(mainRoot string) (*registryFile, error) {
	path := registryPath(mainRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &registryFile{Version: worktreeRegistryVersion, Entries: nil}, nil
		}
		return nil, err
	}
	var reg registryFile
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if reg.Version == 0 {
		reg.Version = worktreeRegistryVersion
	}
	return &reg, nil
}

func saveRegistry(mainRoot string, reg *registryFile) error {
	if reg == nil {
		return fmt.Errorf("registry is nil")
	}
	reg.Version = worktreeRegistryVersion
	dir := filepath.Join(mainRoot, ".orchestra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := registryPath(mainRoot) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, registryPath(mainRoot))
}

func managedNameForPath(mainRoot, absPath string) string {
	reg, err := loadRegistry(mainRoot)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(mainRoot, absPath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	for _, e := range reg.Entries {
		if e.Path == rel {
			return e.Name
		}
	}
	return ""
}

// ListWorktrees returns all worktrees for the repository containing dir.
func ListWorktrees(dir string) ([]WorktreeEntry, error) {
	mainRoot, err := MainRepoRoot(dir)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "-C", mainRoot, "worktree", "list", "--porcelain")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	entries := parseWorktreePorcelain(out.String())
	for i := range entries {
		if entries[i].Path != "" {
			entries[i].Path = filepath.Clean(entries[i].Path)
		}
		if mainRoot != "" && entries[i].Path == filepath.Clean(mainRoot) {
			entries[i].Main = true
		}
		if name := managedNameForPath(mainRoot, entries[i].Path); name != "" {
			entries[i].Managed = true
			entries[i].Name = name
		}
	}
	return entries, nil
}

func parseWorktreePorcelain(raw string) []WorktreeEntry {
	var out []WorktreeEntry
	var cur *WorktreeEntry
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			flush()
			cur = &WorktreeEntry{Path: strings.TrimPrefix(line, "worktree ")}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "HEAD "):
			cur.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(line, "branch ")
		case line == "detached":
			cur.Detached = true
		case strings.HasPrefix(line, "locked"):
			cur.Locked = true
		case line == "prunable":
			cur.Prunable = true
		case line == "bare":
			cur.Bare = true
		}
	}
	flush()
	return out
}

// AddWorktree creates an orchestra-managed linked worktree under .orchestra/worktrees/<name>.
func AddWorktree(dir, name, branch, baseRef string, force bool) (*WorktreeEntry, error) {
	if !IsSafeWorktreeName(name) {
		return nil, fmt.Errorf("invalid worktree name %q: use letters, digits, - or _", name)
	}
	mainRoot, err := MainRepoRoot(dir)
	if err != nil {
		return nil, err
	}
	reg, err := loadRegistry(mainRoot)
	if err != nil {
		return nil, err
	}
	for _, e := range reg.Entries {
		if e.Name == name {
			return nil, fmt.Errorf("worktree %q already registered", name)
		}
	}
	relPath := filepath.ToSlash(filepath.Join(".orchestra", "worktrees", name))
	absPath := filepath.Join(mainRoot, filepath.FromSlash(relPath))
	if _, err := os.Stat(absPath); err == nil {
		return nil, fmt.Errorf("path already exists: %s", relPath)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(worktreesBase(mainRoot), 0o755); err != nil {
		return nil, err
	}
	if branch = strings.TrimSpace(branch); branch == "" {
		branch = "orchestra/" + name
	}
	if !IsGitSafeRef(branch) {
		return nil, fmt.Errorf("invalid branch name %q", branch)
	}
	if baseRef = strings.TrimSpace(baseRef); baseRef == "" {
		baseRef = "HEAD"
	} else if !IsGitSafeRef(baseRef) {
		return nil, fmt.Errorf("invalid base ref %q", baseRef)
	}

	args := []string{"worktree", "add"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "-B", branch, relPath, baseRef)
	cmd := exec.Command("git", append([]string{"-C", mainRoot}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	reg.Entries = append(reg.Entries, registryEntry{
		Name:      name,
		Path:      relPath,
		Branch:    branch,
		CreatedAt: time.Now().UTC(),
	})
	if err := saveRegistry(mainRoot, reg); err != nil {
		return nil, err
	}

	all, err := ListWorktrees(mainRoot)
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		if e.Name == name {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("worktree %q created but not found in list", name)
}

// RemoveWorktree removes a managed worktree by name.
func RemoveWorktree(dir, name string, force bool) error {
	if !IsSafeWorktreeName(name) {
		return fmt.Errorf("invalid worktree name %q", name)
	}
	mainRoot, err := MainRepoRoot(dir)
	if err != nil {
		return err
	}
	reg, err := loadRegistry(mainRoot)
	if err != nil {
		return err
	}
	var relPath string
	found := false
	var kept []registryEntry
	for _, e := range reg.Entries {
		if e.Name == name {
			relPath = e.Path
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return fmt.Errorf("unknown orchestra worktree %q (not in registry)", name)
	}
	absPath := filepath.Join(mainRoot, filepath.FromSlash(relPath))
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, absPath)
	cmd := exec.Command("git", append([]string{"-C", mainRoot}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree remove: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	reg.Entries = kept
	return saveRegistry(mainRoot, reg)
}

// PruneWorktrees runs git worktree prune and drops stale registry entries.
func PruneWorktrees(dir string) (int, error) {
	mainRoot, err := MainRepoRoot(dir)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command("git", "-C", mainRoot, "worktree", "prune")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("git worktree prune: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	reg, err := loadRegistry(mainRoot)
	if err != nil {
		return 0, err
	}
	var kept []registryEntry
	removed := 0
	for _, e := range reg.Entries {
		abs := filepath.Join(mainRoot, filepath.FromSlash(e.Path))
		if _, statErr := os.Stat(abs); statErr != nil {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	reg.Entries = kept
	if err := saveRegistry(mainRoot, reg); err != nil {
		return removed, err
	}
	return removed, nil
}

// ResolveManagedWorktree returns the absolute path for a registered worktree name.
func ResolveManagedWorktree(dir, name string) (string, error) {
	if !IsSafeWorktreeName(name) {
		return "", fmt.Errorf("invalid worktree name %q", name)
	}
	mainRoot, err := MainRepoRoot(dir)
	if err != nil {
		return "", err
	}
	reg, err := loadRegistry(mainRoot)
	if err != nil {
		return "", err
	}
	for _, e := range reg.Entries {
		if e.Name == name {
			abs := filepath.Join(mainRoot, filepath.FromSlash(e.Path))
			if _, statErr := os.Stat(abs); statErr != nil {
				return "", fmt.Errorf("worktree %q path missing: %s", name, e.Path)
			}
			return abs, nil
		}
	}
	return "", fmt.Errorf("unknown orchestra worktree %q", name)
}

// ListManaged returns orchestra registry entries (may include missing paths).
func ListManaged(dir string) ([]registryEntry, error) {
	mainRoot, err := MainRepoRoot(dir)
	if err != nil {
		return nil, err
	}
	reg, err := loadRegistry(mainRoot)
	if err != nil {
		return nil, err
	}
	return reg.Entries, nil
}
