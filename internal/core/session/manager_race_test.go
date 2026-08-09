package session

import (
	"sync"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestManager_LoadOrCreate_ConcurrentSameID(t *testing.T) {
	dir := t.TempDir()
	m := NewManager()
	const id = "race-test-id"
	var wg sync.WaitGroup
	sessions := make([]*Session, 8)
	for i := range sessions {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s, err := m.LoadOrCreate(dir, id)
			if err != nil {
				t.Errorf("LoadOrCreate: %v", err)
				return
			}
			sessions[idx] = s
		}(i)
	}
	wg.Wait()
	first := sessions[0]
	if first == nil {
		t.Fatal("no session returned")
	}
	for i, s := range sessions {
		if s != first {
			t.Fatalf("goroutine %d got different pointer", i)
		}
	}
}

func TestManager_GetOrLoad_ConcurrentAfterSnapshot(t *testing.T) {
	dir := t.TempDir()
	seed := NewWithID("persist-id")
	seed.AppendHistory([]llm.Message{{Role: llm.RoleUser, Content: "hi"}})
	if err := seed.Snapshot(dir); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	var wg sync.WaitGroup
	out := make([]*Session, 6)
	for i := range out {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s, err := m.GetOrLoad(dir, seed.ID)
			if err != nil {
				t.Errorf("GetOrLoad: %v", err)
				return
			}
			out[idx] = s
		}(i)
	}
	wg.Wait()
	if out[0] == nil {
		t.Fatal("no session loaded")
	}
	for i, s := range out {
		if s != out[0] {
			t.Fatalf("goroutine %d got different pointer", i)
		}
	}
}
