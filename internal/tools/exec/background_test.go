package exec

import (
	"strings"
	"testing"
)

func TestBgRegistry_NextIDMonotonic(t *testing.T) {
	reg := NewBackgroundRegistry()
	a := reg.nextID()
	b := reg.nextID()
	c := reg.nextID()
	if a == b || b == c || a == c {
		t.Errorf("ids not distinct: %s %s %s", a, b, c)
	}
	if !strings.HasPrefix(a, "bg_") {
		t.Errorf("prefix: %q", a)
	}
}
