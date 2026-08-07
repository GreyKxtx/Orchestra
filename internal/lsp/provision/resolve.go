// Package provision resolves and (later) installs language-server binaries.
// Phase A: PATH + ~/.orchestra/lsp cache layout + doctor — no download yet.
package provision

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/orchestra/orchestra/internal/lsp/registry"
)

// CacheRoot returns ~/.orchestra/lsp (or ORCHESTRA_LSP_CACHE override).
func CacheRoot() (string, error) {
	if v := strings.TrimSpace(os.Getenv("ORCHESTRA_LSP_CACHE")); v != "" {
		return filepath.Clean(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("provision: home dir: %w", err)
	}
	return filepath.Join(home, ".orchestra", "lsp"), nil
}

// CacheBinaryPath is ~/.orchestra/lsp/<id>/<version>/<binary>[.exe].
func CacheBinaryPath(id, version, binaryName string) (string, error) {
	cands, err := CacheBinaryCandidates(id, version, binaryName)
	if err != nil {
		return "", err
	}
	return cands[0], nil
}

// CacheBinaryCandidates lists possible cache paths (.exe, .cmd, bare on Windows).
func CacheBinaryCandidates(id, version, binaryName string) ([]string, error) {
	root, err := CacheRoot()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, id, version, binaryName)
	if runtime.GOOS != "windows" {
		return []string{base}, nil
	}
	return []string{base + ".exe", base + ".cmd", base}, nil
}

func findCacheBinary(id, version, binaryName string) (string, bool) {
	cands, err := CacheBinaryCandidates(id, version, binaryName)
	if err != nil {
		return "", false
	}
	for _, c := range cands {
		if fileExists(c) {
			return c, true
		}
	}
	return "", false
}

// Source describes where Resolve found the binary.
type Source string

const (
	SourceAbsolute Source = "absolute"
	SourcePATH     Source = "path"
	SourceCache    Source = "cache"
)

// Result is a resolved argv ready for exec.
type Result struct {
	Command []string
	Source  Source
	Entry   *registry.Entry // non-nil when matched to catalog
}

// Resolve maps configured command to an absolute binary path.
// Order: absolute path → PATH → ~/.orchestra/lsp/<id>/<ver>/.
// Does not download (phase B).
func Resolve(command []string) (Result, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return Result{}, fmt.Errorf("provision: empty command")
	}
	bin := command[0]
	args := append([]string(nil), command[1:]...)

	if entry, ok := registry.ByBinaryName(filepath.Base(bin)); ok {
		e := entry
		if filepath.IsAbs(bin) {
			if fileExists(bin) {
				return Result{Command: append([]string{bin}, args...), Source: SourceAbsolute, Entry: &e}, nil
			}
			return Result{}, missingErr(bin, &e)
		}
		if path, err := exec.LookPath(bin); err == nil {
			return Result{Command: append([]string{path}, args...), Source: SourcePATH, Entry: &e}, nil
		}
		if cachePath, ok := findCacheBinary(e.ID, e.Version, e.BinaryName); ok {
			return Result{Command: append([]string{cachePath}, args...), Source: SourceCache, Entry: &e}, nil
		}
		return Result{}, missingErr(bin, &e)
	}

	// Unknown binary — still try absolute / PATH (custom servers).
	if filepath.IsAbs(bin) {
		if fileExists(bin) {
			return Result{Command: append([]string{bin}, args...), Source: SourceAbsolute}, nil
		}
		return Result{}, fmt.Errorf("provision: binary not found: %s", bin)
	}
	if path, err := exec.LookPath(bin); err == nil {
		return Result{Command: append([]string{path}, args...), Source: SourcePATH}, nil
	}
	return Result{}, fmt.Errorf("provision: binary %q not found in PATH (unknown to registry; add a full path or install it)", bin)
}

func missingErr(bin string, e *registry.Entry) error {
	hint := ""
	if e != nil {
		hint = fmt.Sprintf("; install: %s (or: orchestra lsp ensure %s)", e.InstallHint, e.Language)
	}
	return fmt.Errorf("provision: binary %q not found in PATH or ~/.orchestra/lsp%s", bin, hint)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// ServerStatus is one configured (or catalog) server for doctor/status.
type ServerStatus struct {
	Language   string
	Extensions []string
	Command    []string
	Resolved   string // absolute path if found
	Source     Source
	OK         bool
	Error      string
	Hint       string
	RuntimeOK  bool
	RuntimeMsg string
}

// InspectConfigured resolves each configured server command.
func InspectConfigured(servers []ConfiguredServer) []ServerStatus {
	out := make([]ServerStatus, 0, len(servers))
	for _, s := range servers {
		st := ServerStatus{
			Language:   s.Language,
			Extensions: s.Extensions,
			Command:    s.Command,
			RuntimeOK:  true,
		}
		if s.Disabled || len(s.Command) == 0 {
			st.OK = false
			st.Error = "disabled or empty command"
			out = append(out, st)
			continue
		}
		res, err := Resolve(s.Command)
		if err != nil {
			st.OK = false
			st.Error = err.Error()
			if e, ok := registry.ByBinaryName(filepath.Base(s.Command[0])); ok {
				st.Hint = e.InstallHint
				st.RuntimeOK, st.RuntimeMsg = checkRuntime(e)
			} else if e, ok := registry.ByLanguage(s.Language); ok {
				st.Hint = e.InstallHint
				st.RuntimeOK, st.RuntimeMsg = checkRuntime(e)
			}
		} else {
			st.OK = true
			st.Resolved = res.Command[0]
			st.Source = res.Source
			if res.Entry != nil {
				st.RuntimeOK, st.RuntimeMsg = checkRuntime(*res.Entry)
			}
		}
		out = append(out, st)
	}
	return out
}

// ConfiguredServer is a minimal view of yaml lsp.servers[] (avoids config import).
type ConfiguredServer struct {
	Language   string
	Extensions []string
	Command    []string
	Disabled   bool
}

func checkRuntime(e registry.Entry) (ok bool, msg string) {
	switch e.ID {
	case "gopls":
		if _, err := exec.LookPath("go"); err != nil {
			return false, "go not found on PATH (needed to install/run gopls analysis)"
		}
		return true, "go ok"
	case "typescript-language-server", "basedpyright", "intelephense", "yaml-language-server":
		if _, err := exec.LookPath("node"); err != nil {
			return false, "node not found on PATH"
		}
		if _, err := exec.LookPath("npm"); err != nil {
			return true, "node ok (npm missing — ensure/install may fail)"
		}
		return true, "node+npm ok"
	case "rust-analyzer":
		if _, err := exec.LookPath("rustc"); err != nil {
			return false, "rustc not found (analyzer may still run; analysis limited)"
		}
		return true, "rustc ok"
	case "csharp-ls":
		if _, err := exec.LookPath("dotnet"); err != nil {
			return false, "dotnet SDK not found on PATH"
		}
		return true, "dotnet ok"
	case "jdtls", "kotlin-language-server":
		if _, err := exec.LookPath("java"); err != nil {
			return false, "java/JDK not found on PATH"
		}
		return true, "java ok"
	case "ruby-lsp":
		if _, err := exec.LookPath("ruby"); err != nil {
			return false, "ruby not found on PATH"
		}
		return true, "ruby ok"
	case "clangd", "lua-language-server":
		return true, ""
	default:
		return true, ""
	}
}
