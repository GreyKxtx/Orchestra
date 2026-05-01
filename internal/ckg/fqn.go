package ckg

import (
	"path/filepath"
	"strings"
)

// GoFQN returns the importpath-qualified name for a Go symbol, following
// the convention used by `go doc`:
//
//	<importpath>.<Type>.<Method>   for methods
//	<importpath>.<Func>            for top-level functions
//	<importpath>.<Type>            for top-level types
//
// where importpath = modulePath + "/" + relative-dir-of-file (slash-separated).
//
// modulePath: result of ParseModulePath; may be empty for non-module workspaces.
// rootDir:    workspace root (absolute).
// filePath:   absolute path to the source file.
// recvType:   empty for funcs/types; type name (without pointer) for methods.
// symbol:     the func/method/type name.
func GoFQN(modulePath, rootDir, filePath, recvType, symbol string) string {
	pkgPath := goPackagePath(modulePath, rootDir, filePath)
	if pkgPath == "" {
		if recvType != "" {
			return recvType + "." + symbol
		}
		return symbol
	}
	if recvType != "" {
		return pkgPath + "." + recvType + "." + symbol
	}
	return pkgPath + "." + symbol
}

// GoPackageFQN returns just the package importpath for `relation='imports'` edges
// (e.g. "github.com/orchestra/orchestra/internal/agent").
func GoPackageFQN(modulePath, rootDir, filePath string) string {
	return goPackagePath(modulePath, rootDir, filePath)
}

func goPackagePath(modulePath, rootDir, filePath string) string {
	relDir, err := filepath.Rel(rootDir, filepath.Dir(filePath))
	if err != nil {
		relDir = ""
	}
	relDir = filepath.ToSlash(relDir)
	if relDir == "." {
		relDir = ""
	}

	switch {
	case modulePath != "" && relDir != "":
		return modulePath + "/" + relDir
	case modulePath != "":
		return modulePath
	case relDir != "":
		return relDir
	default:
		return ""
	}
}

// IsLikelyFQN returns true if `q` looks like a fully-qualified name
// (contains a slash or more than one dot) — used by Provider.ExploreSymbol
// to distinguish FQN lookups from short-name fuzzy lookups.
func IsLikelyFQN(q string) bool {
	return strings.Contains(q, "/") || strings.Count(q, ".") > 1
}
