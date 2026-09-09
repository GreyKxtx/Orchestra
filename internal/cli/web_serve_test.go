package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

// startServeWeb runs serveWeb in the background against a temp store and
// returns a cancel and a done channel carrying its error.
func startServeWeb(t *testing.T, cfg webRunConfig, streams webIO) (cancel func(), done <-chan error) {
	t.Helper()
	if cfg.StorePath == "" {
		cfg.StorePath = filepath.Join(t.TempDir(), "projects.json")
	}
	ctx, c := context.WithCancel(context.Background())
	ch := make(chan error, 1)
	go func() {
		err := serveWeb(ctx, cfg, streams)
		ch <- err
		close(ch)
	}()
	t.Cleanup(func() {
		c()
		select {
		case <-ch:
		case <-time.After(15 * time.Second):
			t.Error("serveWeb did not return after cancel")
		}
	})
	return c, ch
}

func initialisedDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), config.DefaultConfig(root)); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	return root
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A bare directory is fatal without --init and initialised with it. This is
// what lets the desktop shell hand the server a folder the user just picked.
func TestServeWeb_InitCreatesTheConfigOnlyWhenAsked(t *testing.T) {
	bare := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := serveWeb(ctx, webRunConfig{Workspace: bare, NoOpen: true,
		StorePath: filepath.Join(t.TempDir(), "p.json")}, webIO{})
	if err == nil {
		t.Fatal("a bare workspace without --init must be an error, as today")
	}

	cancel2, done := startServeWeb(t, webRunConfig{Workspace: bare, NoOpen: true, Init: true}, webIO{})
	waitFor(t, ".orchestra.yml", func() bool {
		_, err := os.Stat(filepath.Join(bare, ".orchestra.yml"))
		return err == nil
	})
	cancel2()
	if err := <-done; err != nil {
		t.Fatalf("serveWeb with --init: %v", err)
	}
}

// --init must leave an existing config byte-for-byte alone.
func TestServeWeb_InitLeavesAnExistingConfigUntouched(t *testing.T) {
	root := initialisedDir(t)
	before, _ := os.ReadFile(filepath.Join(root, ".orchestra.yml"))

	cancel, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true, Init: true}, webIO{})
	waitFor(t, "discovery file", func() bool {
		_, err := os.Stat(webDiscoveryPath(root))
		return err == nil
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveWeb: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".orchestra.yml"))
	if string(before) != string(after) {
		t.Fatal("--init rewrote an existing .orchestra.yml")
	}
}

// The store path is honoured: the started workspace lands in it on shutdown.
func TestServeWeb_SavesTheOpenListToTheGivenStore(t *testing.T) {
	root := initialisedDir(t)
	store := filepath.Join(t.TempDir(), "projects.json")

	cancel, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true, StorePath: store}, webIO{})
	waitFor(t, "discovery file", func() bool {
		_, err := os.Stat(webDiscoveryPath(root))
		return err == nil
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveWeb: %v", err)
	}
	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatalf("store not written: %v", err)
	}
	if !strings.Contains(string(b), filepath.Base(root)) {
		t.Fatalf("store does not list the workspace: %s", b)
	}
	if _, err := os.Stat(webDiscoveryPath(root)); err == nil {
		t.Fatal("discovery file survived shutdown")
	}
}
