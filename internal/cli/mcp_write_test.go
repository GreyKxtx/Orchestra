package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestSplitMCPAddArgs_StdioCommandAfterDash(t *testing.T) {
	name, command, err := splitMCPAddArgs([]string{"fs", "npx", "-y", "server-filesystem"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if name != "fs" {
		t.Errorf("name = %q, want fs", name)
	}
	want := []string{"npx", "-y", "server-filesystem"}
	if len(command) != len(want) {
		t.Fatalf("command = %v, want %v", command, want)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %v, want %v", command, want)
		}
	}
}

func TestSplitMCPAddArgs_NoDashIsRemoteOnly(t *testing.T) {
	name, command, err := splitMCPAddArgs([]string{"linear"}, -1)
	if err != nil {
		t.Fatal(err)
	}
	if name != "linear" || command != nil {
		t.Errorf("name=%q command=%v, want linear/nil", name, command)
	}
}

func TestSplitMCPAddArgs_ExtraArgsBeforeDashIsAnError(t *testing.T) {
	if _, _, err := splitMCPAddArgs([]string{"a", "b", "npx"}, 2); err == nil {
		t.Fatal("more than one arg before -- must be rejected")
	}
}

func TestParseKeyValuePairs(t *testing.T) {
	got, err := parseKeyValuePairs([]string{"A=1", "B=two=three"})
	if err != nil {
		t.Fatal(err)
	}
	if got["A"] != "1" || got["B"] != "two=three" {
		t.Errorf("got %v", got)
	}
	if got, err := parseKeyValuePairs(nil); err != nil || got != nil {
		t.Errorf("empty input: got=%v err=%v, want nil/nil", got, err)
	}
	if _, err := parseKeyValuePairs([]string{"no-equals-sign"}); err == nil {
		t.Fatal("malformed pair must be rejected")
	}
}

func TestMCPAddServerFromArgs_BuildsStdioServer(t *testing.T) {
	srv, err := mcpAddServerFromArgs("fs", []string{"npx", "-y", "x"}, "", "", nil, []string{"K=V"}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if srv.Name != "fs" || srv.Command[0] != "npx" || srv.Env["K"] != "V" {
		t.Errorf("srv = %+v", srv)
	}
}

func TestMCPAddServerFromArgs_BuildsRemoteServer(t *testing.T) {
	srv, err := mcpAddServerFromArgs("linear", nil, "https://mcp.linear.app/sse", "LINEAR_TOKEN", []string{"X=1"}, nil, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if srv.URL != "https://mcp.linear.app/sse" || srv.BearerTokenEnv != "LINEAR_TOKEN" || srv.Headers["X"] != "1" || srv.CallTimeoutS != 30 || !srv.Disabled {
		t.Errorf("srv = %+v", srv)
	}
}

func TestMCPAddServerFromArgs_EmptyNameIsAnError(t *testing.T) {
	if _, err := mcpAddServerFromArgs("  ", nil, "https://x", "", nil, nil, 0, false); err == nil {
		t.Fatal("blank name must be rejected")
	}
}

func TestFormatMCPServer_ShowsTransportAndFlags(t *testing.T) {
	out := formatMCPServer(config.MCPServerConfig{Name: "fs", Command: []string{"npx", "-y", "x"}, Env: map[string]string{"K": "V"}})
	for _, want := range []string{"name: fs", "command: npx -y x", "env: 1 var"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// --- end-to-end through cobra Execute, in a temp project ---

func newMCPCLIFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.WriteFile(filepath.Join(dir, ".orchestra.yml"),
		[]byte("project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func execMCP(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(append([]string{"mcp"}, args...))
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestMCPAddRemoveGet_EndToEnd(t *testing.T) {
	dir := newMCPCLIFixture(t)

	if out, err := execMCP(t, "add", "fs", "--", "npx", "-y", "server-filesystem"); err != nil {
		t.Fatalf("add: %v (%s)", err, out)
	}

	cfg, err := config.Load(filepath.Join(dir, ".orchestra.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Name != "fs" {
		t.Fatalf("MCP.Servers = %+v", cfg.MCP.Servers)
	}

	out, err := execMCP(t, "get", "fs")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, "command: npx -y server-filesystem") {
		t.Errorf("get output = %q", out)
	}

	if out, err := execMCP(t, "remove", "fs"); err != nil {
		t.Fatalf("remove: %v (%s)", err, out)
	}
	cfg, err = config.Load(filepath.Join(dir, ".orchestra.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCP.Servers) != 0 {
		t.Fatalf("MCP.Servers = %+v, want empty after remove", cfg.MCP.Servers)
	}
}

func TestMCPRemove_UnknownNameFails(t *testing.T) {
	newMCPCLIFixture(t)
	if _, err := execMCP(t, "remove", "nope"); err == nil {
		t.Fatal("removing an unknown server must fail")
	}
}
