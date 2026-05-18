package skills

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	frontmatterDelim = "---"
	SkillsDir        = ".orchestra/skills"
	// PacksDir is the user-global root where installed third-party packs
	// are materialised. Each pack lives in its own subdirectory; *.md
	// files anywhere under that subdir are loaded as skills.
	PacksDir = ".orchestra/packs"
)

// Origin tags for Skill.Origin.
const (
	OriginProject = "project"
	OriginUser    = "user"
)

// Parse reads a single skill from r. The source argument is recorded on
// the returned Skill (for diagnostics) and is not otherwise validated.
func Parse(source string, r io.Reader) (*Skill, error) {
	br := bufio.NewReader(r)
	first, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read frontmatter open: %w", err)
	}
	if strings.TrimRight(first, "\r\n") != frontmatterDelim {
		return nil, fmt.Errorf("skill %s: missing %q frontmatter open on line 1", source, frontmatterDelim)
	}

	var fmBuf strings.Builder
	closed := false
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == frontmatterDelim {
				closed = true
				break
			}
			fmBuf.WriteString(line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read frontmatter: %w", err)
		}
	}
	if !closed {
		return nil, fmt.Errorf("skill %s: unterminated frontmatter", source)
	}

	var s Skill
	if err := yaml.Unmarshal([]byte(fmBuf.String()), &s); err != nil {
		return nil, fmt.Errorf("skill %s: parse frontmatter: %w", source, err)
	}

	bodyBytes, err := io.ReadAll(br)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	s.Body = strings.TrimLeft(string(bodyBytes), "\r\n")
	s.Source = source
	// A skill whose body is empty is almost always a copy-paste mistake — the
	// agent would receive an empty system prompt and produce gibberish. Catch
	// it at load time so workflow.run / skill.invoke fail with a clear error
	// before any LLM tokens are spent.
	if strings.TrimSpace(s.Body) == "" {
		return nil, fmt.Errorf("skill %s: body is empty (no system prompt content after frontmatter)", source)
	}
	return &s, nil
}

// Load reads a single skill from path.
func Load(path string) (*Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(path, f)
}

// Discover scans installed pack subdirs (<userHome>/.orchestra/packs/*/),
// the user-global skills dir (<userHome>/.orchestra/skills/), and the
// project-local dir (<projectRoot>/.orchestra/skills/). Returns all parsed
// skills sorted by Name. Missing directories are not an error.
//
// Override precedence (highest wins on Name collision):
//
//	project > user > pack
//
// Duplicate Name within the SAME root is still an error. Files with
// extensions other than .md are ignored. The user dir is skipped when
// os.UserHomeDir returns an error (no panic).
func Discover(projectRoot string) ([]*Skill, error) {
	var userDir, packsRoot string
	if home, err := os.UserHomeDir(); err == nil {
		userDir = filepath.Join(home, SkillsDir)
		packsRoot = filepath.Join(home, PacksDir)
	}
	return DiscoverFromAll(packsRoot, userDir, filepath.Join(projectRoot, SkillsDir))
}

// DiscoverFrom is the legacy two-tier entry point (user + project).
// Kept for back-compat with tests written before pack support.
func DiscoverFrom(userDir, projectDir string) ([]*Skill, error) {
	return DiscoverFromAll("", userDir, projectDir)
}

// DiscoverFromAll is the testable form. Any dir may be "" to skip it.
// packsRoot is the parent dir; each subdir of packsRoot is treated as
// one pack source.
func DiscoverFromAll(packsRoot, userDir, projectDir string) ([]*Skill, error) {
	packSkills, err := scanPacks(packsRoot)
	if err != nil {
		return nil, err
	}
	userSkills, err := scanDir(userDir)
	if err != nil {
		return nil, err
	}
	for _, s := range userSkills {
		s.Origin = OriginUser
	}
	projectSkills, err := scanDir(projectDir)
	if err != nil {
		return nil, err
	}
	for _, s := range projectSkills {
		s.Origin = OriginProject
	}

	merged := make(map[string]*Skill, len(packSkills)+len(userSkills)+len(projectSkills))
	for _, s := range packSkills {
		merged[s.Name] = s
	}
	for _, s := range userSkills {
		merged[s.Name] = s
	}
	for _, s := range projectSkills {
		merged[s.Name] = s // project wins
	}
	out := make([]*Skill, 0, len(merged))
	for _, s := range merged {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// scanPacks walks <packsRoot>/<pack-id>/ subdirs and scans each one
// recursively for .md skill files. Each skill's Origin is set to
// "pack:<pack-id>". A missing packsRoot is not an error.
func scanPacks(packsRoot string) ([]*Skill, error) {
	if packsRoot == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(packsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read packs root %s: %w", packsRoot, err)
	}
	var out []*Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		packID := e.Name()
		packDir := filepath.Join(packsRoot, packID)
		seen := make(map[string]string)
		walkErr := filepath.Walk(packDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || filepath.Ext(info.Name()) != ".md" {
				return nil
			}
			s, lerr := Load(path)
			if lerr != nil {
				return lerr
			}
			if s.Name == "" {
				return fmt.Errorf("skill %s: name is required", path)
			}
			if prev, ok := seen[s.Name]; ok {
				return fmt.Errorf("duplicate skill name %q in pack %s: %s and %s", s.Name, packID, prev, path)
			}
			seen[s.Name] = path
			s.Origin = "pack:" + packID
			out = append(out, s)
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return out, nil
}

// scanDir reads .md skill files from a single directory and validates
// uniqueness within that directory.
func scanDir(dir string) ([]*Skill, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills dir %s: %w", dir, err)
	}

	var out []*Skill
	seen := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		s, err := Load(p)
		if err != nil {
			return nil, err
		}
		if s.Name == "" {
			return nil, fmt.Errorf("skill %s: name is required", p)
		}
		if prev, ok := seen[s.Name]; ok {
			return nil, fmt.Errorf("duplicate skill name %q in %s and %s", s.Name, prev, p)
		}
		seen[s.Name] = p
		out = append(out, s)
	}
	return out, nil
}

// Find returns the skill with the given name from skills, or nil.
func Find(skills []*Skill, name string) *Skill {
	for _, s := range skills {
		if s.Name == name {
			return s
		}
	}
	return nil
}
