// Package packs installs third-party skill packs into the user-global
// packs directory. Three source kinds are supported: git repositories,
// HTTP(S) zip/tar archives, and local filesystem paths.
//
// Security model: a skill body becomes SystemPromptOverride for a child
// agent with full tool access. Install therefore *must* be interactive —
// the caller (CLI) shows each skill to the user and asks Y/N. This
// package only does the network/disk work; trust gating lives in the
// caller.
package packs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SourceKind enumerates how a pack source is interpreted.
type SourceKind string

const (
	SourceGit   SourceKind = "git"
	SourceHTTP  SourceKind = "http"
	SourceLocal SourceKind = "local"
)

// Source is a parsed install request.
type Source struct {
	Kind     SourceKind
	Original string // exactly what the user typed
	URL      string // for git/http (may equal Original)
	Path     string // for local sources (absolute)
}

// safeIDRe sanitises a source string into something filesystem-safe.
var safeIDRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ParseSource classifies the input. Heuristics:
//   - Starts with file:// → local
//   - Existing filesystem path → local
//   - git@host:..., or *.git, or starts with git+ → git
//   - http://… / https://… → git when path ends in .git else http
//   - Otherwise → error
func ParseSource(s string) (*Source, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("source is empty")
	}
	if strings.HasPrefix(s, "file://") {
		return &Source{Kind: SourceLocal, Original: s, Path: filepath.FromSlash(strings.TrimPrefix(s, "file://"))}, nil
	}
	if strings.HasPrefix(s, "git@") || strings.HasPrefix(s, "git+") {
		u := strings.TrimPrefix(s, "git+")
		return &Source{Kind: SourceGit, Original: s, URL: u}, nil
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		// Treat .git suffix or any URL whose path ends in .git as a git clone target.
		if strings.HasSuffix(strings.ToLower(strings.TrimRight(s, "/")), ".git") {
			return &Source{Kind: SourceGit, Original: s, URL: s}, nil
		}
		return &Source{Kind: SourceHTTP, Original: s, URL: s}, nil
	}
	// Filesystem path.
	if st, err := os.Stat(s); err == nil && st.IsDir() {
		abs, _ := filepath.Abs(s)
		return &Source{Kind: SourceLocal, Original: s, Path: abs}, nil
	}
	return nil, fmt.Errorf("source %q: not a git URL, http(s) URL, or existing local directory", s)
}

// ID returns the directory name under <packsRoot> where this source
// will be materialised. Deterministic (same source → same dir) and
// filesystem-safe across OSes.
func (s *Source) ID() string {
	base := s.Original
	if s.Kind == SourceLocal {
		base = "local-" + filepath.Base(s.Path)
	}
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		base = u.Host + u.Path
	}
	cleaned := safeIDRe.ReplaceAllString(base, "_")
	cleaned = strings.Trim(cleaned, "_")
	if cleaned == "" {
		cleaned = "pack"
	}
	// Append a short hash so distinct URLs that collapse to the same
	// cleaned form still land in distinct dirs.
	h := sha1.Sum([]byte(s.Original))
	return fmt.Sprintf("%s_%s", cleaned, hex.EncodeToString(h[:4]))
}

// FetchOptions tweaks the materialisation behaviour. All fields optional.
type FetchOptions struct {
	HTTPClient  *http.Client
	GitTimeoutS int
}

// Fetch materialises the source into <destDir>. destDir must be empty
// (or non-existent) — Fetch creates it. Returns the absolute path on
// success. The caller is responsible for cleaning up on failure.
func Fetch(ctx context.Context, src *Source, destDir string, opts FetchOptions) (string, error) {
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return "", fmt.Errorf("mkdir parent: %w", err)
	}
	if _, err := os.Stat(destDir); err == nil {
		return "", fmt.Errorf("destination %s already exists; uninstall first", destDir)
	}
	switch src.Kind {
	case SourceGit:
		return destDir, fetchGit(ctx, src.URL, destDir, opts)
	case SourceHTTP:
		return destDir, fetchHTTP(ctx, src.URL, destDir, opts)
	case SourceLocal:
		return destDir, fetchLocal(src.Path, destDir)
	default:
		return "", fmt.Errorf("unknown source kind: %s", src.Kind)
	}
}

func fetchGit(ctx context.Context, gitURL, dest string, opts FetchOptions) error {
	timeout := time.Duration(opts.GitTimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", gitURL, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s: %w (output: %s)", gitURL, err, strings.TrimSpace(string(out)))
	}
	// Strip .git to keep the tree read-only as far as orchestra is concerned.
	_ = os.RemoveAll(filepath.Join(dest, ".git"))
	return nil
}

func fetchHTTP(ctx context.Context, urlStr, dest string, opts FetchOptions) error {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http get %s: %w", urlStr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %s: status %d", urlStr, resp.StatusCode)
	}
	// Buffer the body to disk so we can re-read for both archive formats.
	tmp, err := os.CreateTemp("", "orchestra-pack-*.bin")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	lower := strings.ToLower(urlStr)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(tmp.Name(), dest)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(tmp.Name(), dest)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(tmp.Name(), dest)
	default:
		os.RemoveAll(dest)
		return fmt.Errorf("http source %s: unsupported archive format (need .zip / .tar / .tar.gz / .tgz)", urlStr)
	}
}

func fetchLocal(srcPath, dest string) error {
	st, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("local source %s: not a directory", srcPath)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return copyDir(srcPath, dest)
}

// copyDir is a tiny recursive directory copy that skips .git and symlinks.
func copyDir(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		// Skip .git to keep packs read-only.
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(dstDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// safeExtractPath joins dest+name and refuses to escape dest (zip-slip guard).
// Rejects POSIX-absolute (leading "/"), OS-absolute, and any path containing ".."
// segments. Also re-checks the resolved path stays under dest.
func safeExtractPath(dest, name string) (string, error) {
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return "", fmt.Errorf("archive entry escapes destination: %q", name)
	}
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("archive entry escapes destination: %q", name)
	}
	// Look for any ".." segment.
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if seg == ".." {
			return "", fmt.Errorf("archive entry escapes destination: %q", name)
		}
	}
	target := filepath.Join(dest, clean)
	cleanDest := filepath.Clean(dest)
	if target != cleanDest && !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry escapes destination: %q", name)
	}
	return target, nil
}

func extractZip(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		target, err := safeExtractPath(dest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func extractTarGz(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTarReader(tar.NewReader(gz), dest)
}

func extractTar(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTarReader(tar.NewReader(f), dest)
}

func extractTarReader(tr *tar.Reader, dest string) error {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeExtractPath(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		default:
			// skip symlinks, devices, etc.
		}
	}
}
