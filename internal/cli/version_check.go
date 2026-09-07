package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// errNoRelease separates "this repository has published no release" from every
// other way the check can fail. Both are "cannot tell", but only one of them is
// something the user can reason about — and reporting a rate limit or an outage
// as "no releases yet" would be a lie.
var errNoRelease = errors.New("no published release")

// latestReleaseURL is the GitHub API endpoint for the newest published
// release. The repository is the one README's install one-liners point at.
const latestReleaseURL = "https://api.github.com/repos/GreyKxtx/Orchestra/releases/latest"

// versionCheckTimeout bounds the whole check. `orchestra version --check` is
// something a user runs while wondering whether to upgrade; it must answer or
// give up quickly, never hang on a network that silently drops packets.
const versionCheckTimeout = 8 * time.Second

// compareReleaseVersions orders two release tags, returning -1/0/1 and whether
// the comparison was possible at all.
//
// ok is false whenever either side is not a release number — most importantly
// for "vnext", the CoreVersion every unstamped build reports. Ordering that
// against a real tag would be a guess, and both guesses are harmful: "older"
// nags every developer on every run, "newer" hides a real update.
//
// Comparison is numeric per segment, not lexicographic: v0.10.0 is newer than
// v0.9.0, which string ordering gets backwards. A prerelease suffix is dropped
// — v0.3.0-rc1 and v0.3.0 carry the same three numbers, and ordering by string
// would sort the rc ahead of the release.
func compareReleaseVersions(a, b string) (int, bool) {
	av, aok := parseReleaseVersion(a)
	bv, bok := parseReleaseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		switch {
		case av[i] < bv[i]:
			return -1, true
		case av[i] > bv[i]:
			return 1, true
		}
	}
	return 0, true
}

// parseReleaseVersion turns "v0.4.1", "0.4.1" or "v0.4" into [3]int. A missing
// patch (or minor) is zero — a tag of "v1" means 1.0.0, not "unparseable".
func parseReleaseVersion(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return out, false
	}
	// Drop a prerelease/build suffix: 0.3.0-rc1, 0.3.0+meta.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// fetchLatestReleaseTag returns the tag_name of the newest published release.
//
// A repository with no release answers 404, which must surface as an error:
// silently treating "no release found" as "you are up to date" is the failure
// mode an update check exists to avoid.
func fetchLatestReleaseTag(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: versionCheckTimeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", errNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", err
	}
	tag := strings.TrimSpace(rel.TagName)
	if tag == "" {
		return "", fmt.Errorf("release has no tag_name")
	}
	return tag, nil
}

// versionCheckMessage phrases the comparison for a terminal.
func versionCheckMessage(current, latest string) string {
	cmp, ok := compareReleaseVersions(current, latest)
	if !ok {
		return fmt.Sprintf(
			"Последний релиз: %s\nТекущая сборка %q — не релизная версия, сравнить нельзя "+
				"(так собирается всё, кроме релизов: `go build` без -ldflags).",
			latest, current)
	}
	switch {
	case cmp < 0:
		return fmt.Sprintf("Доступна новая версия: %s (сейчас %s)\n%s", latest, current, upgradeHint())
	case cmp > 0:
		return fmt.Sprintf("Сборка %s новее последнего релиза %s — локальная сборка впереди тега.",
			current, latest)
	default:
		return fmt.Sprintf("Версия актуальная: %s", current)
	}
}

func upgradeHint() string {
	return "Обновиться: см. scripts/install.sh / scripts/install.ps1 в README, " +
		"или brew/scoop-ассеты релиза."
}

// runVersionCheck performs the network check and writes the outcome.
func runVersionCheck(ctx context.Context, w io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, versionCheckTimeout)
	defer cancel()

	latest, err := fetchLatestReleaseTag(ctx, latestReleaseURL)
	if errors.Is(err, errNoRelease) {
		// Not a failure of the check — the check worked and the answer is that
		// there is nothing to compare against yet.
		fmt.Fprintln(w, "Опубликованных релизов пока нет — сравнивать не с чем.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("не удалось узнать последний релиз: %w", err)
	}
	fmt.Fprintln(w, versionCheckMessage(currentVersion(), latest))
	return nil
}
