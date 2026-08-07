package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrepareToolHistoryContent_ReadSkipsWriteTimeDigest(t *testing.T) {
	// JSON-encoded read of a ~20KB numbered file would exceed a 8KB budget.
	body := strings.Repeat("x", 20*1024)
	raw, _ := json.Marshal(map[string]any{
		"path":      "src/CityInfoPanel.jsx",
		"content":   body,
		"file_hash": "abc",
		"size":      len(body),
	})
	if len(raw) < 8*1024 {
		t.Fatalf("fixture too small: %d", len(raw))
	}

	a := &Agent{opts: Options{ToolDigestBytes: 8 * 1024}}
	got := a.prepareToolHistoryContent("read", json.RawMessage(`{"path":"src/CityInfoPanel.jsx"}`), raw)
	if strings.Contains(got, "[digest tool:read") {
		t.Fatalf("fresh read must not be write-time digested, got digest:\n%.200s", got)
	}
	if !strings.Contains(got, body[:64]) {
		t.Fatal("expected full read content in history")
	}
}

func TestPrepareToolHistoryContent_BashStillDigested(t *testing.T) {
	raw := []byte(`{"exit_code":0,"stdout":"` + strings.Repeat("log line\n", 2000) + `","stderr":""}`)
	a := &Agent{opts: Options{ToolDigestBytes: 1024}}
	got := a.prepareToolHistoryContent("bash", nil, raw)
	if !strings.Contains(got, "[digest tool:bash") {
		t.Fatalf("large bash output should still digest, got:\n%.200s", got)
	}
}

func TestSkipWriteTimeDigest(t *testing.T) {
	if !skipWriteTimeDigest("read") || !skipWriteTimeDigest("explore") {
		t.Fatal("read/explore should skip")
	}
	if skipWriteTimeDigest("bash") || skipWriteTimeDigest("grep") {
		t.Fatal("bash/grep should not skip")
	}
}
