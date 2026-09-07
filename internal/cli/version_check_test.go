package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompareReleaseVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.3.0", "v0.3.0", 0, true},
		{"v0.3.0", "v0.4.0", -1, true},
		{"v0.4.0", "v0.3.9", 1, true},
		{"0.3.0", "v0.3.0", 0, true},   // the v prefix is decoration on both sides
		{"v0.3", "v0.3.0", 0, true},    // a missing patch is zero, not "unparseable"
		{"v0.10.0", "v0.9.0", 1, true}, // numeric, not lexicographic — the classic bug
		{"v1.0.0", "v0.99.99", 1, true},
		// Anything that is not a release number cannot be ordered. "vnext" is
		// the default CoreVersion of every unstamped build, so this is the
		// common case, not an exotic one.
		{"vnext", "v0.3.0", 0, false},
		{"v0.3.0", "", 0, false},
		{"", "v0.3.0", 0, false},
	}
	for _, c := range cases {
		got, ok := compareReleaseVersions(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("compareReleaseVersions(%q, %q) = %d/%v, want %d/%v",
				c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

// A prerelease suffix must not read as a different release number: v0.3.0-rc1
// and v0.3.0 are the same three numbers, and ordering them by string would put
// the rc ahead.
func TestCompareReleaseVersions_IgnoresPrereleaseSuffix(t *testing.T) {
	got, ok := compareReleaseVersions("v0.3.0-rc1", "v0.3.0")
	if !ok || got != 0 {
		t.Errorf("compareReleaseVersions(v0.3.0-rc1, v0.3.0) = %d/%v, want 0/true", got, ok)
	}
}

func TestFetchLatestReleaseTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); !strings.Contains(got, "json") {
			t.Errorf("Accept = %q, want a JSON accept header", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.4.1","name":"0.4.1"}`))
	}))
	defer srv.Close()

	got, err := fetchLatestReleaseTag(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.4.1" {
		t.Errorf("tag = %q, want v0.4.1", got)
	}
}

// A repo with no published release answers 404. That is not a crash and not
// "you are up to date" — it is "cannot tell", and it must surface as an error.
func TestFetchLatestReleaseTag_ErrorsOnNoRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := fetchLatestReleaseTag(context.Background(), srv.URL); err == nil {
		t.Fatal("no error for a 404 — a missing release would read as up to date")
	}
}

func TestVersionCheckMessage_NewerReleaseAvailable(t *testing.T) {
	msg := versionCheckMessage("v0.3.0", "v0.4.0")
	if !strings.Contains(msg, "v0.4.0") {
		t.Errorf("msg = %q, must name the available version", msg)
	}
	if !strings.Contains(msg, "v0.3.0") {
		t.Errorf("msg = %q, must name the running version so the gap is visible", msg)
	}
}

func TestVersionCheckMessage_UpToDate(t *testing.T) {
	msg := versionCheckMessage("v0.4.0", "v0.4.0")
	if strings.Contains(strings.ToLower(msg), "доступна") {
		t.Errorf("msg = %q, must not offer an update when there is none", msg)
	}
	if !strings.Contains(msg, "v0.4.0") {
		t.Errorf("msg = %q, must name the version it checked", msg)
	}
}

// An unstamped build reports CoreVersion ("vnext"), which is not a release
// number. Guessing it is older would nag every developer on every run;
// guessing it is newer would hide a real update. Say what is known.
func TestVersionCheckMessage_UncomparableBuildSaysSo(t *testing.T) {
	msg := versionCheckMessage("vnext", "v0.4.0")
	if !strings.Contains(msg, "v0.4.0") {
		t.Errorf("msg = %q, must still report the latest release", msg)
	}
	if strings.Contains(strings.ToLower(msg), "актуальн") {
		t.Errorf("msg = %q, claims up-to-date for a build it cannot order", msg)
	}
}

// A local build ahead of the last tag is normal for anyone working on the
// project; it must not be reported as an available "update" backwards.
func TestVersionCheckMessage_AheadOfLatest(t *testing.T) {
	msg := versionCheckMessage("v0.5.0", "v0.4.0")
	if strings.Contains(strings.ToLower(msg), "доступна") {
		t.Errorf("msg = %q, offers a downgrade as an update", msg)
	}
}
