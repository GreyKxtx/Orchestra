package ckg

import (
	"os"
	"path/filepath"
	"testing"
)

// ---- TypeScript FQN ----

func TestTsFQN_WithContainer(t *testing.T) {
	root := "/repo"
	got := TsFQN(root, "/repo/src/app.ts", "MyClass", "method")
	want := "src/app.ts::MyClass.method"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTsFQN_NoContainer(t *testing.T) {
	root := "/repo"
	got := TsFQN(root, "/repo/src/app.ts", "", "topLevel")
	want := "src/app.ts::topLevel"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTsPackageFQN(t *testing.T) {
	root := "/repo"
	got := TsPackageFQN(root, "/repo/src/utils.js")
	want := "src/utils.js"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---- Python FQN ----

func TestPyFQN_WithContainer(t *testing.T) {
	root := "/repo"
	got := PyFQN(root, "/repo/src/agent/runner.py", "Runner", "run")
	want := "src.agent.runner::Runner.run"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPyFQN_NoContainer(t *testing.T) {
	root := "/repo"
	got := PyFQN(root, "/repo/src/agent/runner.py", "", "main")
	want := "src.agent.runner::main"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPyPackageFQN(t *testing.T) {
	root := "/repo"
	got := PyPackageFQN(root, "/repo/src/utils.py")
	want := "src.utils"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---- Rust FQN ----

func TestRustFQN_WithCrateAndContainer(t *testing.T) {
	root := "/repo"
	got := RustFQN("myapp", root, "/repo/src/parser.rs", "Parser", "parse")
	want := "myapp::parser::Parser::parse"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRustFQN_LibFile(t *testing.T) {
	root := "/repo"
	got := RustFQN("myapp", root, "/repo/src/lib.rs", "", "init")
	want := "myapp::init"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRustFQN_NoCrate(t *testing.T) {
	root := "/repo"
	got := RustFQN("", root, "/repo/src/parser.rs", "", "func")
	want := "parser::func"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRustPackageFQN(t *testing.T) {
	root := "/repo"
	got := RustPackageFQN("myapp", root, "/repo/src/foo/bar.rs")
	want := "myapp::foo::bar"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---- Java FQN ----

func TestJavaFQN_Full(t *testing.T) {
	got := JavaFQN("com.example.pkg", "MyClass", "myMethod")
	want := "com.example.pkg.MyClass.myMethod"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJavaFQN_NoPkg(t *testing.T) {
	got := JavaFQN("", "MyClass", "myMethod")
	want := "MyClass.myMethod"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJavaFQN_NoContainer(t *testing.T) {
	got := JavaFQN("com.example", "", "MyClass")
	want := "com.example.MyClass"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJavaPackageFQN(t *testing.T) {
	got := JavaPackageFQN("com.example.pkg")
	if got != "com.example.pkg" {
		t.Errorf("got %q, want %q", got, "com.example.pkg")
	}
}

func TestParseModulePath(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("// header\n\nmodule example.com/foo/bar\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ParseModulePath(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.com/foo/bar" {
		t.Fatalf("got %q, want example.com/foo/bar", got)
	}
}

func TestParseModulePathMissing(t *testing.T) {
	tmp := t.TempDir()
	got, err := ParseModulePath(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestGoFQN(t *testing.T) {
	root := filepath.FromSlash("/repo")
	tests := []struct {
		name, modulePath, file, recvType, symbol, want string
	}{
		{"top-level func", "github.com/x/y", "/repo/internal/agent/agent.go", "", "Run",
			"github.com/x/y/internal/agent.Run"},
		{"method", "github.com/x/y", "/repo/internal/agent/agent.go", "Agent", "Run",
			"github.com/x/y/internal/agent.Agent.Run"},
		{"root pkg", "github.com/x/y", "/repo/main.go", "", "main",
			"github.com/x/y.main"},
		{"no module path", "", "/repo/internal/agent/agent.go", "", "Run",
			"internal/agent.Run"},
		{"no module path root file", "", "/repo/main.go", "", "Run",
			"Run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GoFQN(tt.modulePath, root, filepath.FromSlash(tt.file), tt.recvType, tt.symbol)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsLikelyFQN(t *testing.T) {
	cases := map[string]bool{
		"Run":                      false,
		"Agent.Run":                false,
		"github.com/x/y.Agent.Run": true,
		"internal/agent.Run":       true,
		"fmt.Println":              false,
	}
	for in, want := range cases {
		if got := IsLikelyFQN(in); got != want {
			t.Errorf("IsLikelyFQN(%q): got %v, want %v", in, got, want)
		}
	}
}
