package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// NoteStats summarizes memory.note events from .orchestra/llm_log.jsonl
// (llm.Logger.LogMemoryNote): how many turns wrote, skipped, or failed to
// write a project-memory note, and where the written ones came from.
//
// This is the answer to the question the field run left unanswerable: one
// durable fact across 52 sessions, and no artifact said whether memory had
// tried and failed, or never tried at all.
type NoteStats struct {
	Written int
	Skipped int
	Failed  int
	// FromModel / FromDigest split the Written count by source — a written
	// note always has a source, so FromModel + FromDigest == Written.
	FromModel  int
	FromDigest int
}

// Total is every memory.note event seen, regardless of outcome.
func (s NoteStats) Total() int { return s.Written + s.Skipped + s.Failed }

type noteLogEntry struct {
	Event  string `json:"event"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

// ParseNoteStats reads memory.note events from path. A missing file counts
// as zero events, not an error — a project that has not run a turn yet (or
// predates cc41475) is a legitimate state. Malformed lines are skipped, not
// fatal, matching usage.Load's tolerance for a partially-written file.
func ParseNoteStats(path string) (NoteStats, error) {
	var s NoteStats
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, fmt.Errorf("memory: open %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e noteLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.Event != "memory.note" {
			continue
		}
		switch e.Kind {
		case "written":
			s.Written++
			switch e.Source {
			case "model":
				s.FromModel++
			case "digest":
				s.FromDigest++
			}
		case "skipped":
			s.Skipped++
		case "failed":
			s.Failed++
		}
	}
	if err := sc.Err(); err != nil {
		return s, fmt.Errorf("memory: read %s: %w", path, err)
	}
	return s, nil
}
