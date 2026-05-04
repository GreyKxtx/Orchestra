package testpkg

import "testing"

func TestAdd(t *testing.T) {
    if got := Sum(1, 2); got != 3 {
        t.Errorf("got %d, want 3", got)
    }
}
