package daemon

import (
	"os"
	"testing"
)

func TestDiscoverDaemonURL_Priority(t *testing.T) {
	root := t.TempDir()
	// 1) discovery file wins
	if err := WriteDiscovery(root, DiscoveryInfo{URL: "http://127.0.0.1:1111"}); err != nil {
		t.Fatalf("WriteDiscovery failed: %v", err)
	}
	os.Setenv("ORCHESTRA_DAEMON_URL", "http://127.0.0.1:2222")
	defer os.Unsetenv("ORCHESTRA_DAEMON_URL")

	url, ok, err := DiscoverDaemonURL(root, "127.0.0.1", 3333)
	if err != nil {
		t.Fatalf("DiscoverDaemonURL failed: %v", err)
	}
	if !ok || url != "http://127.0.0.1:1111" {
		t.Fatalf("expected discovery URL, got ok=%v url=%q", ok, url)
	}

	// 2) env wins if discovery missing
	if err := RemoveDiscovery(root); err != nil {
		t.Fatalf("RemoveDiscovery failed: %v", err)
	}
	url, ok, err = DiscoverDaemonURL(root, "127.0.0.1", 3333)
	if err != nil {
		t.Fatalf("DiscoverDaemonURL failed: %v", err)
	}
	if !ok || url != "http://127.0.0.1:2222" {
		t.Fatalf("expected env URL, got ok=%v url=%q", ok, url)
	}

	// 3) config wins if discovery+env missing
	os.Unsetenv("ORCHESTRA_DAEMON_URL")
	url, ok, err = DiscoverDaemonURL(root, "127.0.0.1", 3333)
	if err != nil {
		t.Fatalf("DiscoverDaemonURL failed: %v", err)
	}
	if !ok || url != "http://127.0.0.1:3333" {
		t.Fatalf("expected config URL, got ok=%v url=%q", ok, url)
	}
}
