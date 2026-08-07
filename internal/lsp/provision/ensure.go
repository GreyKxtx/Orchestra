package provision

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/lsp/registry"
)

// Installer installs a language server into destDir (version directory).
type Installer interface {
	Install(ctx context.Context, e registry.Entry, destDir string) error
}

type defaultInstaller struct{}

var activeInstaller Installer = defaultInstaller{}

// SetInstallerForTest swaps the installer (tests only). Pass nil to restore.
func SetInstallerForTest(i Installer) {
	if i == nil {
		activeInstaller = defaultInstaller{}
		return
	}
	activeInstaller = i
}

// CanEnsure reports whether Ensure has an automated installer for this ID.
func CanEnsure(id string) bool {
	switch id {
	case "gopls", "typescript-language-server", "basedpyright", "yaml-language-server",
		"intelephense", "csharp-ls":
		return true
	default:
		return false
	}
}

// Ensure downloads/builds the language server into ~/.orchestra/lsp/<id>/<ver>/.
func Ensure(ctx context.Context, idOrLang string) error {
	e, ok := registry.ByID(idOrLang)
	if !ok {
		e, ok = registry.ByLanguage(idOrLang)
	}
	if !ok {
		return fmt.Errorf("provision: unknown language server %q (see orchestra lsp list)", idOrLang)
	}
	if !CanEnsure(e.ID) {
		return fmt.Errorf("provision: ensure for %q not automated yet — install manually: %s", e.ID, e.InstallHint)
	}

	if _, ok := findCacheBinary(e.ID, e.Version, e.BinaryName); ok {
		return nil
	}
	dir, err := cacheVersionDir(e.ID, e.Version)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("provision: mkdir %s: %w", dir, err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if _, has := ctx.Deadline(); !has {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
	}

	if err := activeInstaller.Install(ctx, e, dir); err != nil {
		return err
	}
	if _, ok := findCacheBinary(e.ID, e.Version, e.BinaryName); !ok {
		return fmt.Errorf("provision: ensure %s finished but binary missing in %s", e.ID, dir)
	}
	return nil
}

func cacheVersionDir(id, version string) (string, error) {
	root, err := CacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id, version), nil
}

func (defaultInstaller) Install(ctx context.Context, e registry.Entry, destDir string) error {
	switch e.ID {
	case "gopls":
		return installGopls(ctx, destDir)
	case "typescript-language-server":
		return installNPM(ctx, destDir, e.BinaryName, "typescript-language-server@latest", "typescript@latest")
	case "basedpyright":
		return installNPM(ctx, destDir, e.BinaryName, "basedpyright@latest")
	case "yaml-language-server":
		return installNPM(ctx, destDir, e.BinaryName, "yaml-language-server@latest")
	case "intelephense":
		return installNPM(ctx, destDir, e.BinaryName, "intelephense@latest")
	case "csharp-ls":
		return installDotnetTool(ctx, destDir, "csharp-ls")
	default:
		return fmt.Errorf("provision: no installer for %q", e.ID)
	}
}

func installGopls(ctx context.Context, destDir string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("provision: go not found on PATH (needed to install gopls)")
	}
	cmd := exec.CommandContext(ctx, "go", "install", "golang.org/x/tools/gopls@latest")
	cmd.Dir = destDir
	cmd.Env = append(os.Environ(), "GOBIN="+destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("provision: go install gopls@latest failed: %s", msg)
	}
	return nil
}

func installNPM(ctx context.Context, destDir, binaryName string, pkgs ...string) error {
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("provision: npm not found on PATH")
	}
	args := append([]string{"install", "--prefix", destDir}, pkgs...)
	cmd := exec.CommandContext(ctx, "npm", args...)
	cmd.Dir = destDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("provision: npm install: %s", msg)
	}
	return placeNPMBinary(destDir, binaryName)
}

func placeNPMBinary(destDir, binaryName string) error {
	binDir := filepath.Join(destDir, "node_modules", ".bin")
	candidates := []string{
		filepath.Join(binDir, binaryName),
		filepath.Join(binDir, binaryName+".cmd"),
		filepath.Join(binDir, binaryName+".exe"),
		filepath.Join(binDir, binaryName+".ps1"),
	}
	var src string
	for _, c := range candidates {
		if fileExists(c) {
			src = c
			break
		}
	}
	if src == "" {
		return fmt.Errorf("provision: npm binary %q not found under %s", binaryName, binDir)
	}
	ext := filepath.Ext(src)
	destName := binaryName
	if runtime.GOOS == "windows" {
		if ext == "" {
			ext = ".cmd"
		}
		destName = binaryName + ext
	}
	return copyFile(src, filepath.Join(destDir, destName))
}

func installDotnetTool(ctx context.Context, destDir, packageID string) error {
	if _, err := exec.LookPath("dotnet"); err != nil {
		return fmt.Errorf("provision: dotnet not found on PATH")
	}
	cmd := exec.CommandContext(ctx, "dotnet", "tool", "install", packageID, "--tool-path", destDir)
	cmd.Dir = destDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if !strings.Contains(strings.ToLower(msg), "already installed") {
			return fmt.Errorf("provision: dotnet tool install: %s", msg)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
