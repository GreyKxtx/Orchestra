package resolver

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/protocol"
)

const nearestSample = `package main

import "fmt"

func Greet(name string) string {
	return fmt.Sprintf("hello, %s", name)
}

func main() {
	fmt.Println(Greet("world"))
}
`

func TestNearestRegionHint_PointsAtTheRealText(t *testing.T) {
	// The model remembers the signature slightly wrong (an extra param).
	hint := NearestRegionHint([]byte(nearestSample), "func Greet(name string, loud bool) string {\n\treturn \"\"\n}")
	if hint == "" {
		t.Fatal("no hint for a near-miss search block")
	}
	if !strings.Contains(hint, "func Greet(name string) string {") {
		t.Fatalf("hint does not show the real line:\n%s", hint)
	}
	if !strings.Contains(hint, "file has 12 lines") {
		t.Fatalf("hint lacks the file size:\n%s", hint)
	}
}

func TestNearestRegionHint_SilentWhenNothingResembles(t *testing.T) {
	if hint := NearestRegionHint([]byte(nearestSample), "SELECT * FROM users WHERE id = ?"); hint != "" {
		t.Fatalf("expected no hint for unrelated search, got:\n%s", hint)
	}
}

func TestApplySearchReplace_StaleErrorCarriesNearestRegion(t *testing.T) {
	_, err := ApplySearchReplace([]byte(nearestSample), "func Greet(name string, loud bool) string {", "x")
	if err == nil {
		t.Fatal("expected StaleContent error")
	}
	pe, ok := protocol.AsError(err)
	if !ok || pe.Code != protocol.StaleContent {
		t.Fatalf("err=%v", err)
	}
	data, _ := pe.Data.(map[string]any)
	nearest, _ := data["nearest"].(string)
	if !strings.Contains(nearest, "func Greet(name string) string {") {
		t.Fatalf("StaleContent detail has no usable nearest region: %#v", pe.Data)
	}
}
