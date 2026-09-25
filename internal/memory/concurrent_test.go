package memory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func concurrentStore(root string) *Store {
	cfg := DefaultConfig()
	cfg.MaxAgentKB = 4096 // no compaction: every fact must survive on its own merits
	cfg.GlobalEnabled = false
	return NewStore(root, "", cfg)
}

func newFact(i int) string { return fmt.Sprintf("marker%02d zeta%02d omega%02d kappa%02d", i, i, i, i) }
func seedFact(i int) string {
	return fmt.Sprintf("seedfact%02d relates alpha%02d to beta%02d", i, i, i)
}

func agentMD(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "memory", "agent.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// 30 new facts and 30 restatements of existing ones, all at once: every new
// fact survives, and every restated one is still there once (DATA-1: 8 to 29
// of the 30 survived before the lock).
func TestAppend_ConcurrentWritersLoseNothing(t *testing.T) {
	root := t.TempDir()
	s := concurrentStore(root)
	for i := 0; i < 10; i++ {
		if _, err := s.AppendEntry("project", TypeProject, seedFact(i)); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			content := newFact(i)
			if i >= 30 {
				content = seedFact((i-30)%10) + " strongly"
			}
			if _, err := concurrentStore(root).AppendEntry("project", TypeProject, content); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	body := agentMD(t, root)
	for i := 0; i < 30; i++ {
		if n := strings.Count(body, fmt.Sprintf("marker%02d ", i)); n != 1 {
			t.Errorf("new fact %d appears %d times", i, n)
		}
	}
	for i := 0; i < 10; i++ {
		if n := strings.Count(body, fmt.Sprintf("seedfact%02d ", i)); n != 1 {
			t.Errorf("seed fact %d appears %d times", i, n)
		}
	}
	if n := len(splitEntries(body)); n != 40 {
		t.Errorf("want 40 entries, got %d", n)
	}
}

// Two cores on one project are two processes; the lock is the OS's, not a mutex.
func TestAppend_ConcurrentProcessesLoseNothing(t *testing.T) {
	if os.Getenv("ORCH_MEMORY_WRITER_ROOT") != "" {
		t.Skip("helper process")
	}
	root := t.TempDir()
	const procs, each = 3, 10
	var wg sync.WaitGroup
	errs := make(chan error, procs+1)
	for p := 0; p < procs; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestMemoryWriterProcess$", "-test.count=1")
			cmd.Env = append(os.Environ(),
				"ORCH_MEMORY_WRITER_ROOT="+root,
				"ORCH_MEMORY_WRITER_FROM="+strconv.Itoa(p*each),
				"ORCH_MEMORY_WRITER_N="+strconv.Itoa(each))
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("writer %d: %v\n%s", p, err, out)
			}
		}(p)
	}
	// And this process writes at the same time.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := procs * each; i < (procs+1)*each; i++ {
			if _, err := concurrentStore(root).AppendEntry("project", TypeProject, newFact(i)); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	body := agentMD(t, root)
	for i := 0; i < (procs+1)*each; i++ {
		if n := strings.Count(body, fmt.Sprintf("marker%02d ", i)); n != 1 {
			t.Errorf("fact %d appears %d times", i, n)
		}
	}
}

// TestMemoryWriterProcess is the child side of the test above.
func TestMemoryWriterProcess(t *testing.T) {
	root := os.Getenv("ORCH_MEMORY_WRITER_ROOT")
	if root == "" {
		t.Skip("run by TestAppend_ConcurrentProcessesLoseNothing")
	}
	from, _ := strconv.Atoi(os.Getenv("ORCH_MEMORY_WRITER_FROM"))
	n, _ := strconv.Atoi(os.Getenv("ORCH_MEMORY_WRITER_N"))
	for i := from; i < from+n; i++ {
		if _, err := concurrentStore(root).AppendEntry("project", TypeProject, newFact(i)); err != nil {
			t.Fatal(err)
		}
	}
}

// The header says who wrote an entry, without confusing the parsers that
// read its timestamp and type.
func TestAppend_ProvenanceOnTheHeaderLine(t *testing.T) {
	root := t.TempDir()
	s := concurrentStore(root)
	if _, err := s.AppendEntryFrom("project", TypeFeedback, "prefer table tests", "task_3 [*evil*], run r1"); err != nil {
		t.Fatal(err)
	}
	entries := splitEntries(agentMD(t, root))
	if len(entries) != 1 {
		t.Fatalf("want one entry, got %d", len(entries))
	}
	header, _, _ := strings.Cut(strings.TrimSpace(entries[0]), "\n")
	if !strings.Contains(header, "(by task_3 evil, run r1)") {
		t.Errorf("provenance missing or unsanitised: %q", header)
	}
	if EntryTypeOf(entries[0]) != TypeFeedback {
		t.Errorf("the type marker must still read: %q", header)
	}
	if _, ok := entryTimestamp(entries[0]); !ok {
		t.Errorf("the timestamp must still read: %q", header)
	}
}
