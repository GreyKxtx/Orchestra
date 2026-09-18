package webtransport

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The chat renders markdown the model wrote about files it read, so a
// repository can put text on this page. Without a policy, text that arrives
// that way can execute and talk to the network.
func TestStatic_ServesAContentSecurityPolicy(t *testing.T) {
	base := startAssetServer(t)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(base + "/?token=secret")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy on the page response")
	}
	for _, want := range []string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self'",
		// 'self', not 'none': the chat page frames our own settings document.
		// What this is here to refuse is a page on another origin framing us,
		// and 'self' refuses exactly that.
		"frame-ancestors 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP is missing %q: %s", want, csp)
		}
	}
	// 'unsafe-inline' or 'unsafe-eval' anywhere gives back exactly what the
	// policy is here to take away.
	if strings.Contains(csp, "unsafe-") {
		t.Errorf("CSP allows unsafe sources: %s", csp)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// The settings panel is a second document of ours, shown in an iframe. Under
// default-src 'none' a frame needs frame-src to say so, and a blocked frame is
// a console line rather than an error the page can catch — the gear opened an
// empty dialog and nothing anywhere said why. The test is conditional on the
// markup so that removing the iframe does not leave a rule nobody needs.
func TestStatic_PolicyAllowsTheSettingsFrame(t *testing.T) {
	page := filepath.Join("..", "..", "ui", "web", "static", "index.html")
	b, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(b), "<iframe") {
		t.Skip("the page frames nothing, so frame-src is not needed")
	}
	// Both halves, because each one alone still blocks the frame — and the
	// second failure only appeared once the first was fixed.
	for _, want := range []string{"frame-src 'self'", "frame-ancestors 'self'"} {
		if !strings.Contains(staticCSP, want) {
			t.Errorf(
				"index.html has an <iframe> but the policy has no %s; the frame is blocked "+
					"with nothing on screen to say so: %s",
				want, staticCSP,
			)
		}
	}
}

// The policy above is only true if the pages can live under it. An inline
// <script> or style="" in the served markup is dropped by the browser with no
// error anyone sees — the theme flashes, or a swatch loses its colour.
func TestStatic_ServedPagesCarryNothingInline(t *testing.T) {
	repo := filepath.Join("..", "..")
	inlineScript := regexp.MustCompile(`<script(?:\s[^>]*)?>\s*[^<\s]`)
	styleAttr := regexp.MustCompile(`\sstyle\s*=\s*["']`)

	for _, name := range []string{"index.html", "settings.html"} {
		p := filepath.Join(repo, "ui", "web", "static", name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		html := string(b)
		if loc := inlineScript.FindString(html); loc != "" {
			t.Errorf("%s has an inline <script>; script-src 'self' drops it. Move it to a file.", name)
		}
		if styleAttr.MatchString(html) {
			t.Errorf("%s has a style=\"\" attribute; style-src 'self' drops it. Use a class or the CSSOM.", name)
		}
	}
}

// The bundles build markup as strings; a style="" written there is dropped the
// same way, in both hosts, and the VS Code webview has had that policy for
// longer than this one.
func TestStatic_BundlesBuildNoInlineStyleAttributes(t *testing.T) {
	repo := filepath.Join("..", "..")
	styleAttr := regexp.MustCompile(`\sstyle\s*=\s*\\?["']`)

	for _, rel := range []string{
		filepath.Join("ui", "web", "static", "web.bundle.js"),
		filepath.Join("ui", "web", "static", "settings.bundle.js"),
	} {
		b, err := os.ReadFile(filepath.Join(repo, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if styleAttr.Match(b) {
			t.Errorf("%s writes a style=\"\" into markup; set it through the CSSOM instead", rel)
		}
	}
}

// The page, the bundle and the stylesheet are embedded in the binary and
// change with it, under names that never change. With no caching header at
// all a browser invents its own freshness, and one talking to a fixed port
// does: after an upgrade it kept painting the previous build's stylesheet,
// which is invisible — the app looks like the change simply was not made.
func TestStatic_AssetsAreRevalidated(t *testing.T) {
	base := startAssetServer(t)

	// The token goes in a header, not the query: with ?token= the page answers
	// 302 (it sets the cookie and drops the token from the URL), and a test
	// that walks past anything other than 200 checks nothing at all — which is
	// exactly what the first version of this test did.
	req, err := http.NewRequest(http.MethodGet, base+"/", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("X-Orchestra-Token", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page status = %d, want 200 — the assertion below would be vacuous", resp.StatusCode)
	}
	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "no-cache") && !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want it to force revalidation", cc)
	}
}
