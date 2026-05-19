package skills

import (
	"sync"
)

// discoverCache memoises Discover results per project root for the
// lifetime of the process. M9 in architecture audit: Discover was
// called up to 8 times per request flow (cli/apply.go, cli/skills.go,
// cli/workflow.go, cli/skill_resolver.go, core/workflow.go,
// core/skill.go), each time re-reading every .md file under
// ~/.orchestra/skills, ~/.orchestra/packs and <project>/.orchestra/
// skills. The skill set is stable within a process; explicit reload
// via InvalidateCache is available for callers that watch the
// filesystem.
var (
	discoverCacheMu sync.Mutex
	discoverCache   = map[string]discoverResult{}
)

type discoverResult struct {
	skills []*Skill
	err    error
}

// DiscoverCached returns a cached Discover result for projectRoot,
// running the actual discovery on the first call. Safe for concurrent
// use; subsequent callers share the same slice (read-only).
//
// The error from the underlying Discover is cached too — a transient
// I/O hiccup will persist until InvalidateCache. Callers that need a
// fresh read should call Discover directly.
func DiscoverCached(projectRoot string) ([]*Skill, error) {
	discoverCacheMu.Lock()
	if v, ok := discoverCache[projectRoot]; ok {
		discoverCacheMu.Unlock()
		return v.skills, v.err
	}
	discoverCacheMu.Unlock()

	// Discovery itself happens outside the lock: it's I/O-bound (filesystem
	// walk) and we don't want to serialise concurrent first-callers for
	// different projects. A small duplicate-work window for callers racing
	// on the same root is acceptable — both arrive at the same answer.
	skills, err := Discover(projectRoot)

	discoverCacheMu.Lock()
	discoverCache[projectRoot] = discoverResult{skills: skills, err: err}
	discoverCacheMu.Unlock()
	return skills, err
}

// InvalidateCache forgets the cached result for projectRoot so the next
// DiscoverCached re-reads from disk. Pass "" to forget every project.
// Use after a skill file is added/removed/edited.
func InvalidateCache(projectRoot string) {
	discoverCacheMu.Lock()
	defer discoverCacheMu.Unlock()
	if projectRoot == "" {
		discoverCache = map[string]discoverResult{}
		return
	}
	delete(discoverCache, projectRoot)
}
