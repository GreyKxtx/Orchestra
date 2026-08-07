package view

import (
	"reflect"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestSplitJoinCommand(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"npx -y @pkg", []string{"npx", "-y", "@pkg"}},
		{`npx -y "@modelcontextprotocol/server-filesystem" "C:\path with spaces"`, []string{"npx", "-y", "@modelcontextprotocol/server-filesystem", `C:\path with spaces`}},
		{`echo 'hello world'`, []string{"echo", "hello world"}},
		{"", nil},
	}
	for _, tc := range cases {
		got := splitCommand(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("splitCommand(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
		if len(got) == 0 {
			continue
		}
		round := splitCommand(joinCommand(got))
		if !reflect.DeepEqual(round, got) {
			t.Fatalf("round-trip %v → %q → %#v", got, joinCommand(got), round)
		}
	}
}

func TestParseEnvAndCSV(t *testing.T) {
	env := parseEnv("A=1; B=two=still\nC=3")
	if env["A"] != "1" || env["B"] != "two=still" || env["C"] != "3" {
		t.Fatalf("parseEnv: %#v", env)
	}
	tools := parseCSV(" read , write, ")
	if !reflect.DeepEqual(tools, []string{"read", "write"}) {
		t.Fatalf("parseCSV: %#v", tools)
	}
}

func TestMCPServerViewConfigRoundTrip(t *testing.T) {
	src := config.MCPServerConfig{
		Name:         "fs",
		Command:      []string{"npx", "-y", "pkg", "."},
		Env:          map[string]string{"K": "V"},
		Disabled:     true,
		CallTimeoutS: 12,
		AllowedTools: []string{"read_file"},
	}
	v := MCPServerViewFromConfig(src)
	got := v.ToConfig()
	if !reflect.DeepEqual(got, src) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, src)
	}
}

func TestMCPPresetsIncludeFilesystem(t *testing.T) {
	ps := MCPPresets(`/tmp/proj`)
	if len(ps) < 2 {
		t.Fatalf("expected presets, got %d", len(ps))
	}
	found := false
	for _, p := range ps {
		if p.Key == "filesystem" {
			found = true
			if len(p.Command) < 4 || p.Command[len(p.Command)-1] != `/tmp/proj` {
				t.Fatalf("filesystem command: %#v", p.Command)
			}
		}
	}
	if !found {
		t.Fatal("filesystem preset missing")
	}
}
