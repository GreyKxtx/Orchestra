package agent

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// looksLikeWorkerResult decides whether a child's answer is recorded in the
// Lead's scratchpad and compacted with the rest of the run. Its switch is a
// hand-written list of statuses, sitting in this package, next to a set of
// wrap* functions that live in internal/tasks and keep growing.
//
// It has drifted three times. needs_review was missing, then no_changes, then
// llm_verification_failed — each one an outcome the Lead most needed to see
// and the one it silently dropped. A fourth hand edit is not a plan, so this
// reads the statuses out of the source that produces them.
//
// internal/tasks imports this package, so a test there cannot reach an
// unexported function here, and this one cannot import internal/tasks without
// a cycle. Reading the file is the way across.

const workerStatusSource = "../tasks/worker_verify.go"

var workerStatusLiteral = regexp.MustCompile(`"status":\s*"([a-z_]+)"`)

func TestLooksLikeWorkerResult_KnowsEveryStatusTheWrappersProduce(t *testing.T) {
	src, err := os.ReadFile(workerStatusSource)
	if err != nil {
		t.Fatalf("cannot read %s, so the drift guard is not guarding anything: %v",
			workerStatusSource, err)
	}
	matches := workerStatusLiteral.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatalf("no status literals found in %s — the payloads moved or changed shape, "+
			"and this guard now passes by finding nothing", workerStatusSource)
	}

	seen := map[string]bool{}
	var statuses []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			statuses = append(statuses, m[1])
		}
	}
	sort.Strings(statuses)
	t.Logf("statuses produced by %s: %s", workerStatusSource, strings.Join(statuses, " "))

	for _, st := range statuses {
		t.Run(st, func(t *testing.T) {
			// The richest shape any wrapper emits: if the switch does not accept
			// this, it accepts nothing for that status.
			payload := fmt.Sprintf(`{"status":%q,"worker_result":"something the child said"}`, st)
			if !looksLikeWorkerResult(payload) {
				t.Errorf("internal/tasks answers the Lead with status %q and "+
					"looksLikeWorkerResult does not recognise it, so that answer never "+
					"reaches the Lead's scratchpad. Add %q to the switch in "+
					"orchestra_scratchpad.go.", st, st)
			}
		})
	}
}
