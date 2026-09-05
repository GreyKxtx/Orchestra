package cli

import "testing"

func TestFormatCached(t *testing.T) {
	cases := []struct {
		cached, prompt int
		want           string
	}{
		{0, 20_000, "—"},              // local model: no cache reported
		{18_000, 20_000, "18000 (90%)"}, // the number the field run could not see
		{500, 0, "500"},                 // defensive: never divide by zero
	}
	for _, c := range cases {
		if got := formatCached(c.cached, c.prompt); got != c.want {
			t.Errorf("formatCached(%d, %d) = %q, want %q", c.cached, c.prompt, got, c.want)
		}
	}
}
