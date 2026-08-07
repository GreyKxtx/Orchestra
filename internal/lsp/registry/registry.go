// Package registry is the built-in catalog of language-server recipes.
// Phase A of docs/architecture/lsp-auto-provision.md: metadata only (no download).
package registry

import "strings"

// Entry describes one language server Orchestra knows how to resolve / ensure.
type Entry struct {
	// ID is the stable cache key (e.g. "gopls").
	ID string
	// Language matches lsp.servers[].language in .orchestra.yml when possible.
	Language string
	// Extensions routed to this server (lowercase, with dot).
	Extensions []string
	// BinaryName is the executable basename expected on PATH / in cache.
	BinaryName string
	// DefaultArgs are typical argv after the binary (informational / init templates).
	DefaultArgs []string
	// Version pins the cache subdirectory; bump when Ensure installs a new pin.
	Version string
	// InstallHint is a one-line manual install instruction (doctor / errors).
	InstallHint string
	// RuntimeHint notes required toolchains (go, node, …); empty if none.
	RuntimeHint string
	// RootMarkers help workspace detect (phase C); optional in phase A.
	RootMarkers []string
}

// All returns the built-in catalog (immutable copy of pointers into the package table).
func All() []Entry {
	out := make([]Entry, len(catalog))
	copy(out, catalog)
	return out
}

// ByID looks up a catalog entry by ID (case-insensitive).
func ByID(id string) (Entry, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, e := range catalog {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// ByBinaryName finds an entry whose BinaryName matches (case-insensitive, no .exe).
func ByBinaryName(name string) (Entry, bool) {
	name = normalizeBin(name)
	for _, e := range catalog {
		if normalizeBin(e.BinaryName) == name {
			return e, true
		}
	}
	return Entry{}, false
}

// ByLanguage finds the preferred entry for a language id (go, typescript, csharp, …).
// Accepts common aliases (dotnet → csharp, js → typescript, …).
func ByLanguage(lang string) (Entry, bool) {
	lang = normalizeLanguage(lang)
	for _, e := range catalog {
		if e.Language == lang {
			return e, true
		}
	}
	return Entry{}, false
}

// ByExtension returns the first catalog entry that claims ext (e.g. ".go").
func ByExtension(ext string) (Entry, bool) {
	ext = strings.ToLower(ext)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	for _, e := range catalog {
		for _, x := range e.Extensions {
			if x == ext {
				return e, true
			}
		}
	}
	return Entry{}, false
}

func normalizeBin(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".exe")
	return name
}

func normalizeLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "dotnet", "c#", "cs", "c-sharp":
		return "csharp"
	case "ts", "js", "javascript", "tsx", "jsx":
		return "typescript"
	case "py":
		return "python"
	case "c++", "cpp", "cxx", "cc", "c":
		return "cpp"
	case "yml":
		return "yaml"
	case "kt", "kts":
		return "kotlin"
	case "rb":
		return "ruby"
	case "sh", "bash", "zsh":
		return "bash"
	default:
		return lang
	}
}

// catalog: ~12 popular language servers. Ensure (phase B+) installs from these recipes.
// Custom servers still work via yaml command + PATH without a registry entry.
var catalog = []Entry{
	{
		ID:          "gopls",
		Language:    "go",
		Extensions:  []string{".go"},
		BinaryName:  "gopls",
		DefaultArgs: []string{"serve"},
		// "latest" tracks the Go toolchain; pinned old tags break on new Go
		// (e.g. v0.16.2 fails to build on Go 1.25+/1.26 with tokeninternal).
		Version:     "latest",
		InstallHint: "go install golang.org/x/tools/gopls@latest",
		RuntimeHint: "requires Go toolchain (go) on PATH",
		RootMarkers: []string{"go.mod", "go.work"},
	},
	{
		ID:          "typescript-language-server",
		Language:    "typescript",
		Extensions:  []string{".ts", ".tsx", ".js", ".jsx"},
		BinaryName:  "typescript-language-server",
		DefaultArgs: []string{"--stdio"},
		Version:     "4.3.3",
		InstallHint: "npm install -g typescript-language-server typescript",
		RuntimeHint: "requires Node.js + npm on PATH",
		RootMarkers: []string{"package.json", "tsconfig.json", "jsconfig.json"},
	},
	{
		ID:          "basedpyright",
		Language:    "python",
		Extensions:  []string{".py"},
		BinaryName:  "basedpyright-langserver",
		DefaultArgs: []string{"--stdio"},
		Version:     "1.21.0",
		InstallHint: "npm install -g basedpyright",
		RuntimeHint: "requires Node.js + npm on PATH (or pip install basedpyright)",
		RootMarkers: []string{"pyproject.toml", "setup.py", "requirements.txt"},
	},
	{
		ID:          "rust-analyzer",
		Language:    "rust",
		Extensions:  []string{".rs"},
		BinaryName:  "rust-analyzer",
		DefaultArgs: nil,
		Version:     "2024-12-23",
		InstallHint: "rustup component add rust-analyzer  OR download from GitHub releases",
		RuntimeHint: "requires Rust toolchain for full analysis",
		RootMarkers: []string{"Cargo.toml"},
	},
	{
		ID:          "csharp-ls",
		Language:    "csharp",
		Extensions:  []string{".cs"},
		BinaryName:  "csharp-ls",
		DefaultArgs: nil,
		Version:     "0.16.0",
		InstallHint: "dotnet tool install -g csharp-ls",
		RuntimeHint: "requires .NET SDK (dotnet) on PATH",
		RootMarkers: []string{"*.sln", "*.csproj", "global.json"},
	},
	{
		ID:          "clangd",
		Language:    "cpp",
		Extensions:  []string{".c", ".h", ".cpp", ".cc", ".cxx", ".hpp", ".hxx"},
		BinaryName:  "clangd",
		DefaultArgs: nil,
		Version:     "19.1.0",
		InstallHint: "install LLVM/clangd (https://clangd.llvm.org/installation)",
		RuntimeHint: "clangd binary on PATH; compile_commands.json recommended",
		RootMarkers: []string{"compile_commands.json", "CMakeLists.txt", "Makefile"},
	},
	{
		ID:          "jdtls",
		Language:    "java",
		Extensions:  []string{".java"},
		BinaryName:  "jdtls",
		DefaultArgs: nil,
		Version:     "1.40.0",
		InstallHint: "install Eclipse JDT LS wrapper (e.g. jdtls script / vscode-java)",
		RuntimeHint: "requires JDK on PATH",
		RootMarkers: []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle"},
	},
	{
		ID:          "lua-language-server",
		Language:    "lua",
		Extensions:  []string{".lua"},
		BinaryName:  "lua-language-server",
		DefaultArgs: nil,
		Version:     "3.13.0",
		InstallHint: "download from https://github.com/LuaLS/lua-language-server/releases",
		RuntimeHint: "",
		RootMarkers: []string{".luarc.json", ".luacheckrc"},
	},
	{
		ID:          "intelephense",
		Language:    "php",
		Extensions:  []string{".php"},
		BinaryName:  "intelephense",
		DefaultArgs: []string{"--stdio"},
		Version:     "1.12.0",
		InstallHint: "npm install -g intelephense",
		RuntimeHint: "requires Node.js + npm on PATH",
		RootMarkers: []string{"composer.json"},
	},
	{
		ID:          "kotlin-language-server",
		Language:    "kotlin",
		Extensions:  []string{".kt", ".kts"},
		BinaryName:  "kotlin-language-server",
		DefaultArgs: nil,
		Version:     "1.3.13",
		InstallHint: "download from https://github.com/fwcd/kotlin-language-server/releases",
		RuntimeHint: "JDK recommended on PATH",
		RootMarkers: []string{"build.gradle.kts", "settings.gradle.kts", "pom.xml"},
	},
	{
		ID:          "ruby-lsp",
		Language:    "ruby",
		Extensions:  []string{".rb", ".rake", ".gemspec"},
		BinaryName:  "ruby-lsp",
		DefaultArgs: nil,
		Version:     "0.22.0",
		InstallHint: "gem install ruby-lsp",
		RuntimeHint: "requires Ruby + gem on PATH",
		RootMarkers: []string{"Gemfile", ".ruby-version"},
	},
	{
		ID:          "yaml-language-server",
		Language:    "yaml",
		Extensions:  []string{".yaml", ".yml"},
		BinaryName:  "yaml-language-server",
		DefaultArgs: []string{"--stdio"},
		Version:     "1.15.0",
		InstallHint: "npm install -g yaml-language-server",
		RuntimeHint: "requires Node.js + npm on PATH",
		RootMarkers: []string{},
	},
}
