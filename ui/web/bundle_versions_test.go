package webui

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/protocol"
)

// The web bundle embeds the protocol versions (ui/web/src/01-wire.generated.js);
// a version bump that regenerated the sources but not static/ shipped a page
// that refused the core it came with, and only check-web.mjs in CI caught
// it. The Go tests catch it now, node or no node.
func TestWebBundleCarriesTheProtocolVersions(t *testing.T) {
	b, err := fs.ReadFile(Assets(), "web.bundle.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		fmt.Sprintf("PROTOCOL_VERSION: %d,", protocol.ProtocolVersion),
		fmt.Sprintf("MIN_PROTOCOL_VERSION: %d,", protocol.MinProtocolVersion),
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("static/web.bundle.js is stale (%s not in it): run go generate ./protocol/wire and node ui/web/scripts/bundle-web.mjs", want)
		}
	}
}
