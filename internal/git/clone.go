package git

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CloneURL is a remote this package is willing to clone from. The set is
// deliberately small: the URL arrives from a page, and git reads a leading
// dash as an option, so anything that is not plainly one of these forms is
// refused rather than handed to the command line and hoped about.
//
//	https://host/owner/repo[.git]
//	http://host/owner/repo[.git]   (a local mirror; loopback only)
//	ssh://git@host/owner/repo.git
//	git@host:owner/repo.git        (scp-like, the form GitHub shows)
type CloneURL struct {
	// Raw is the string to hand to git, unchanged.
	Raw string
	// Name is the directory the clone should land in: the last path segment
	// with any .git suffix removed.
	Name string
}

// ParseCloneURL validates a remote and works out the directory name for it.
// The error is safe to show a user: it names what was wrong, not what was
// entered, beyond what they typed themselves.
func ParseCloneURL(raw string) (CloneURL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return CloneURL{}, fmt.Errorf("no repository URL")
	}
	// git treats a leading dash as an option no matter where it appears in the
	// argument list, and "--upload-pack=..." is remote code execution. There is
	// no legitimate remote that starts with one.
	if strings.HasPrefix(s, "-") {
		return CloneURL{}, fmt.Errorf("repository URL must not start with a dash")
	}
	if strings.ContainsAny(s, "\x00\n\r") {
		return CloneURL{}, fmt.Errorf("repository URL contains a control character")
	}

	var path string
	switch {
	case strings.HasPrefix(s, "https://"), strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "ssh://"):
		u, err := url.Parse(s)
		if err != nil {
			return CloneURL{}, fmt.Errorf("repository URL is not a URL: %w", err)
		}
		if u.Host == "" {
			return CloneURL{}, fmt.Errorf("repository URL has no host")
		}
		path = u.Path
	case scpLike(s):
		// git@github.com:owner/repo.git
		path = s[strings.Index(s, ":")+1:]
	default:
		return CloneURL{}, fmt.Errorf("only https, ssh and git@host:owner/repo URLs can be cloned")
	}

	name := repoName(path)
	if name == "" {
		return CloneURL{}, fmt.Errorf("repository URL does not end in a repository name")
	}
	return CloneURL{Raw: s, Name: name}, nil
}

// scpLike reports whether s is the user@host:path form, which has no scheme.
// A Windows drive letter ("C:\src") also has a colon, so a host is required.
func scpLike(s string) bool {
	at := strings.Index(s, "@")
	colon := strings.Index(s, ":")
	if at <= 0 || colon <= at+1 {
		return false
	}
	// Anything after the colon must not be a path separator: "git@host:/x" is
	// still fine, but "C:/src" has no @ and never reaches here.
	return !strings.Contains(s[:at], "/")
}

// repoName is the last path segment with .git removed, rejected if it is not a
// plain directory name. Traversal ("..") and separators cannot survive this.
func repoName(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return ""
	}
	last := trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		last = trimmed[i+1:]
	}
	last = strings.TrimSuffix(last, ".git")
	if last == "" || last == "." || last == ".." {
		return ""
	}
	if strings.ContainsAny(last, `/\:*?"<>|`) {
		return ""
	}
	return last
}

// Clone clones remote into a new directory under parent and returns the
// directory it created. parent must exist; the destination must not.
//
// The command is run without a shell and with the URL after "--", so no part
// of it can be read as an option. Credential prompts are turned off: a private
// repository fails with git's own message instead of blocking the server on a
// terminal that nobody can see.
func Clone(ctx context.Context, remote, parent string) (string, error) {
	u, err := ParseCloneURL(remote)
	if err != nil {
		return "", err
	}
	absParent, err := filepath.Abs(strings.TrimSpace(parent))
	if err != nil {
		return "", fmt.Errorf("destination folder: %w", err)
	}
	info, err := os.Stat(absParent)
	if err != nil {
		return "", fmt.Errorf("destination folder: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("destination is not a folder: %s", absParent)
	}
	dest := filepath.Join(absParent, u.Name)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("%s already exists", dest)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--progress", "--", u.Raw, dest)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_ASKPASS=echo",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		// A half-written directory is worse than none: the next attempt would
		// fail on "already exists" and the user could not tell why.
		_ = os.RemoveAll(dest)
		return "", fmt.Errorf("git clone failed: %s", lastLine(out.String()))
	}
	return dest, nil
}

// lastLine is git's most useful line — the reason — without the progress that
// precedes it.
func lastLine(s string) string {
	lines := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return "no output"
}
