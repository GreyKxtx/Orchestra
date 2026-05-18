package tui

import "testing"

func TestSplitNameAndArgs(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantArgs string
		wantOK   bool
	}{
		{"feature add cache", "feature", "add cache", true},
		{"debugger why does foo return nil", "debugger", "why does foo return nil", true},
		{"  feature   trim me  ", "feature", "trim me", true},
		{"feature", "feature", "", false},
		{"feature  ", "feature", "", false},
		{"", "", "", false},
		{"   ", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			name, args, ok := splitNameAndArgs(c.in)
			if ok != c.wantOK {
				t.Errorf("ok=%v, want %v", ok, c.wantOK)
			}
			if name != c.wantName {
				t.Errorf("name=%q, want %q", name, c.wantName)
			}
			if args != c.wantArgs {
				t.Errorf("args=%q, want %q", args, c.wantArgs)
			}
		})
	}
}
