package tools

import (
	"context"
	"sync"

	"github.com/orchestra/orchestra/llm"
)

// instructionSeen remembers which directories' ORCHESTRA.md each agent has
// already been given, so a directory's rules reach every agent once.
//
// It was one set for the whole process: the first agent to read a file in a
// directory got its rules and every later one — the workers a Lead spawned
// to edit exactly that directory, the next turn after a compaction — got
// nothing (audit §3.4). The agent is the run and task its ctx carries
// (llm.Trace); a caller without a trace shares the one set it always had.
// The sets of the oldest agents are dropped past maxInstructionAgents, so the
// memory stays bounded however long the core runs.
type instructionSeen struct {
	mu    sync.Mutex
	sets  map[string]map[string]struct{}
	order []string
}

const maxInstructionAgents = 256

func instructionAgentKey(ctx context.Context) string {
	t := llm.TraceFrom(ctx)
	return t.RunID + "/" + t.TaskID
}

// firstTime records that agent has been given dir's rules and reports
// whether this is the first time.
func (s *instructionSeen) firstTime(agent, dir string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sets == nil {
		s.sets = map[string]map[string]struct{}{}
	}
	set := s.sets[agent]
	if set == nil {
		set = map[string]struct{}{}
		s.sets[agent] = set
		s.order = append(s.order, agent)
		for len(s.order) > maxInstructionAgents {
			delete(s.sets, s.order[0])
			s.order = s.order[1:]
		}
	}
	if _, seen := set[dir]; seen {
		return false
	}
	set[dir] = struct{}{}
	return true
}
