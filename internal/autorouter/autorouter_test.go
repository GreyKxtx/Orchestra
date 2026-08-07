package autorouter

import "testing"

func TestHeuristicClassify(t *testing.T) {
	cases := []struct {
		q    string
		want string
	}{
		{"спланируй архитектуру модуля", "plan"},
		{"plan the auth flow", "plan"},
		{"найди где используется Foo", "explore"},
		{"where is the handler defined", "explore"},
		{"объясни что делает этот код", "ask"},
		{"explain how auth works", "ask"},
		{"добавь функцию Validate", "build"},
		{"fix the nil panic in agent.go", "build"},
	}
	for _, tc := range cases {
		got := HeuristicClassify(tc.q)
		if got.Mode != tc.want {
			t.Errorf("HeuristicClassify(%q)=%q want %q (%s)", tc.q, got.Mode, tc.want, got.Reason)
		}
	}
}

func TestParseDecision(t *testing.T) {
	dec, ok := parseDecision(`{"mode":"plan","confidence":0.9,"reason":"design"}`)
	if !ok || dec.Mode != "plan" {
		t.Fatalf("parseDecision plan: ok=%v dec=%+v", ok, dec)
	}
	_, ok = parseDecision(`{"mode":"orchestra","confidence":1}`)
	if ok {
		t.Fatal("orchestra must be rejected by router")
	}
}
