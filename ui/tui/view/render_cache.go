package view

// renderCache memoizes rendered strings for completed (non-streaming) assistant
// messages keyed by (msgKey, width). On long sessions this turns the per-tick
// re-render from O(N) markdown reflows into O(1) string lookups — only the
// streaming message has to be rebuilt.
//
// Eviction strategy: when width changes (terminal resize) we drop the entire
// cache rather than try per-width retention — resize is rare and the cache
// repopulates within a few ticks.
type renderCache struct {
	width   int
	entries map[int64]string
}

func (rc *renderCache) get(key int64, width int) (string, bool) {
	if key == 0 || rc.entries == nil {
		return "", false
	}
	if rc.width != width {
		return "", false
	}
	s, ok := rc.entries[key]
	return s, ok
}

func (rc *renderCache) put(key int64, width int, rendered string) {
	if key == 0 {
		return
	}
	if rc.entries == nil || rc.width != width {
		rc.entries = map[int64]string{}
		rc.width = width
	}
	rc.entries[key] = rendered
}

func (rc *renderCache) delete(key int64) {
	if rc.entries != nil {
		delete(rc.entries, key)
	}
}

func (rc *renderCache) purge() {
	rc.entries = nil
	rc.width = 0
}
