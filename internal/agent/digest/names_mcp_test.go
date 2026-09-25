package digest

import "testing"

func TestNormalizeToolNameKeepsMCPCase(t *testing.T) {
	if got := NormalizeToolName("mcp:GitHub:getIssue"); got != "mcp:GitHub:getIssue" {
		t.Fatalf("MCP names keep their case, got %q", got)
	}
	if got := NormalizeToolName("MCP:fs:read_file"); got != "mcp:fs:read_file" {
		t.Fatalf("the prefix is canonical, the rest as sent: %q", got)
	}
	if got := NormalizeToolName("Read"); got != "read" {
		t.Fatalf("built-in names still fold: %q", got)
	}
}
