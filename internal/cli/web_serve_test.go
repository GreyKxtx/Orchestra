package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/protocol"
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

// readAnnounce blocks until one line arrives on the announce pipe.
func readAnnounce(t *testing.T, r *io.PipeReader) webDiscovery {
	t.Helper()
	sc := bufio.NewScanner(r)
	lineCh := make(chan string, 1)
	go func() {
		if sc.Scan() {
			lineCh <- sc.Text()
		}
		close(lineCh)
	}()
	select {
	case line, ok := <-lineCh:
		if !ok {
			t.Fatal("announce pipe closed without a line")
		}
		var d webDiscovery
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatalf("announce line is not the discovery object: %v\n%s", err, line)
		}
		return d
	case <-time.After(20 * time.Second):
		t.Fatal("no announce line within 20s")
	}
	return webDiscovery{}
}

// The announce line is the discovery object, and the token in it works.
func TestServeWeb_AnnounceLineIsTheDiscoveryObject(t *testing.T) {
	root := initialisedDir(t)
	annR, annW := io.Pipe()
	stdinR, stdinW := io.Pipe()

	_, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true},
		webIO{Announce: annW, Stdin: stdinR})
	d := readAnnounce(t, annR)

	if d.URL == "" || d.Token == "" || d.PID != os.Getpid() || d.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("announce = %+v, want url, token, our pid and protocol version", d)
	}
	req, _ := http.NewRequest(http.MethodGet, d.URL+"/health", nil)
	req.Header.Set("Authorization", "Bearer "+d.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health with the announced token = %d, want 200", resp.StatusCode)
	}

	_ = stdinW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveWeb after stdin EOF: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("stdin EOF did not shut the server down")
	}
}

// EOF on stdin is a *clean* shutdown: the list is saved, the discovery file gone.
func TestServeWeb_StdinEOFIsACleanShutdown(t *testing.T) {
	root := initialisedDir(t)
	store := filepath.Join(t.TempDir(), "projects.json")
	annR, annW := io.Pipe()
	stdinR, stdinW := io.Pipe()

	_, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true, StorePath: store},
		webIO{Announce: annW, Stdin: stdinR})
	_ = readAnnounce(t, annR)
	_ = stdinW.Close()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("no shutdown on stdin EOF")
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("open list not saved on EOF shutdown: %v", err)
	}
	if _, err := os.Stat(webDiscoveryPath(root)); err == nil {
		t.Fatal("discovery file survived EOF shutdown")
	}
}

// Without announce, stdin is nobody's business: a never-closed stdin must not
// keep the server from stopping on ctx cancel, and nothing is read from it.
func TestServeWeb_WithoutAnnounceStdinIsIgnored(t *testing.T) {
	root := initialisedDir(t)
	stdinR, stdinW := io.Pipe()
	defer func() { _ = stdinW.Close() }()

	cancel, done := startServeWeb(t, webRunConfig{Workspace: root, NoOpen: true},
		webIO{Announce: nil, Stdin: stdinR})
	waitFor(t, "discovery file", func() bool {
		_, err := os.Stat(webDiscoveryPath(root))
		return err == nil
	})
	// Writing must not block: nobody is reading stdin in this mode. A reader
	// would make this Write return; we assert it does NOT complete.
	wrote := make(chan struct{})
	go func() { _, _ = stdinW.Write([]byte("x")); close(wrote) }()
	select {
	case <-wrote:
		t.Fatal("stdin was read without --announce")
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serveWeb: %v", err)
	}
}
