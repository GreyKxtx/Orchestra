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

// GoPackageFQN returns just the package importpath for `relation='imports'` edges.
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

// IsLikelyFQN returns true if q looks like a fully-qualified name
// (contains a slash, a Rust-style `::`, or more than one dot).
func IsLikelyFQN(q string) bool {
	return strings.Contains(q, "/") || strings.Contains(q, "::") || strings.Count(q, ".") > 1
}

// ---- TypeScript / JavaScript ----

// tsModuleKey returns the file-relative path used as the TS/JS module FQN prefix,
// e.g. "src/session/processor.ts".
func tsModuleKey(rootDir, filePath string) string {
	rel, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		return filepath.Base(filePath)
	}
	return filepath.ToSlash(rel)
}

// TsPackageFQN returns the file-level FQN for TypeScript/JavaScript (the module key).
func TsPackageFQN(rootDir, filePath string) string {
	return tsModuleKey(rootDir, filePath)
}

// TsFQN returns the FQN for a TypeScript/JavaScript symbol.
// Format: "rel/path/file.ts::Container.symbol" or "rel/path/file.ts::symbol"
func TsFQN(rootDir, filePath, container, symbol string) string {
	mod := tsModuleKey(rootDir, filePath)
	if container != "" {
		return mod + "::" + container + "." + symbol
	}
	return mod + "::" + symbol
}

// ---- Python ----

// pyModuleKey converts a Python file path to a dotted module path,
// e.g. "src/agent/runner.py" → "src.agent.runner".
func pyModuleKey(rootDir, filePath string) string {
	rel, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		return strings.TrimSuffix(filepath.Base(filePath), ".py")
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".py")
	return strings.ReplaceAll(rel, "/", ".")
}

// PyPackageFQN returns the module-level FQN for Python.
func PyPackageFQN(rootDir, filePath string) string {
	return pyModuleKey(rootDir, filePath)
}

// PyFQN returns the FQN for a Python symbol.
// Format: "module.path::Container.symbol" or "module.path::symbol"
func PyFQN(rootDir, filePath, container, symbol string) string {
	mod := pyModuleKey(rootDir, filePath)
	if container != "" {
		return mod + "::" + container + "." + symbol
	}
	return mod + "::" + symbol
}

// ---- Rust ----

// rustModuleKey converts a Rust file path to a crate::module path.
// src/lib.rs and src/main.rs map to the crate root; src/foo/mod.rs maps to crate::foo.
func rustModuleKey(crateName, rootDir, filePath string) string {
	rel, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		if crateName != "" {
			return crateName
		}
		return filepath.Base(filePath)
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "src/")
	rel = strings.TrimSuffix(rel, ".rs")
	if rel == "lib" || rel == "main" {
		if crateName != "" {
			return crateName
		}
		return ""
	}
	rel = strings.TrimSuffix(rel, "/mod")
	rel = strings.ReplaceAll(rel, "/", "::")
	if crateName != "" {
		return crateName + "::" + rel
	}
	return rel
}

// RustPackageFQN returns the module-level FQN for Rust.
func RustPackageFQN(crateName, rootDir, filePath string) string {
	return rustModuleKey(crateName, rootDir, filePath)
}

// RustFQN returns the FQN for a Rust symbol.
// Format: "crate::module::Container::symbol" or "crate::module::symbol"
func RustFQN(crateName, rootDir, filePath, container, symbol string) string {
	mod := rustModuleKey(crateName, rootDir, filePath)
	if container != "" {
		if mod != "" {
			return mod + "::" + container + "::" + symbol
		}
		return container + "::" + symbol
	}
	if mod != "" {
		return mod + "::" + symbol
	}
	return symbol
}

// ---- Java ----

// JavaPackageFQN returns the package-level FQN for Java (the dotted package declaration).
func JavaPackageFQN(pkgDecl string) string {
	return pkgDecl
}

// JavaFQN returns the FQN for a Java symbol.
// Format: "com.example.pkg.ClassName.method"
func JavaFQN(pkgDecl, container, symbol string) string {
	var sb strings.Builder
	if pkgDecl != "" {
		sb.WriteString(pkgDecl)
	}
	if container != "" {
		if sb.Len() > 0 {
			sb.WriteByte('.')
		}
		sb.WriteString(container)
	}
	if symbol != "" {
		if sb.Len() > 0 {
			sb.WriteByte('.')
		}
		sb.WriteString(symbol)
	}
	return sb.String()
}
