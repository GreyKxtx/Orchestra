// ui/tui/mention_test.go
package tui

import "testing"

func TestMentionQuery(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello @internal/resolver", "internal/resolver"},
		{"@src", "src"},
		{"no at sign", ""},
		{"text @", ""},
		{"@", ""},
		{"double @foo @bar", "bar"},
	}
	for _, tc := range cases {
		got := mentionQuery(tc.input)
		if got != tc.want {
			t.Errorf("mentionQuery(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestReplaceLastMention(t *testing.T) {
	cases := []struct {
		input       string
		replacement string
		want        string
	}{
		{"fix @int", "internal/resolver.go", "fix internal/resolver.go"},
		{"@src", "src/main.go", "src/main.go"},
		{"hello @foo bar", "baz.go", "hello baz.go bar"},
		{"no at", "x", "no at"},
	}
	for _, tc := range cases {
		got := replaceLastMention(tc.input, tc.replacement)
		if got != tc.want {
			t.Errorf("replaceLastMention(%q, %q) = %q, want %q", tc.input, tc.replacement, got, tc.want)
		}
	}
}
