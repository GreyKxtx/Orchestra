package ckg

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ParseModulePath reads go.mod from rootDir and returns the module path.
// Returns ("", nil) if go.mod is missing (workspace is not a Go module).
// Comments and blank lines are skipped; only the first `module ...` directive is honoured.
func ParseModulePath(rootDir string) (string, error) {
	f, err := os.Open(filepath.Join(rootDir, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "module ") {
			mod := strings.TrimSpace(strings.TrimPrefix(line, "module"))
			mod = strings.Trim(mod, `"`)
			return mod, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

// ParseCrateName reads Cargo.toml from rootDir and returns the crate package name.
// Returns ("", nil) if Cargo.toml is missing.
func ParseCrateName(rootDir string) (string, error) {
	f, err := os.Open(filepath.Join(rootDir, "Cargo.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inPackage := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inPackage = line == "[package]"
			continue
		}
		if inPackage && strings.HasPrefix(line, "name") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				name = strings.Trim(name, `"'`)
				return name, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}
