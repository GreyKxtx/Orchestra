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
		"frame-ancestors 'none'",
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
