package tools

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"unicode"
)

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// TestToolDefinitions_AreEnglish keeps the tool schemas in the same language as
// the prompts.
//
// Every prompt file is English and a test enforces it, but tool descriptions
// stayed Russian — so one turn changed language between the system prompt and
// the schema sitting right next to it. The schemas are also the larger half of
// the wire (~32 KB against a ~2 KB system prompt for a cloud family), and
// Cyrillic tokenizes worse on exactly the local models this project targets,
// so the cost lands on every request of every run.
func TestToolDefinitions_AreEnglish(t *testing.T) {
	var offenders []string

	for name, def := range allToolDefsMap() {
		if hasCyrillic(def.Function.Name) {
			offenders = append(offenders, name+": tool name")
		}
		if hasCyrillic(def.Function.Description) {
			offenders = append(offenders, name+": description")
		}
		// Parameter descriptions ship inside the same schema.
		if raw, err := json.Marshal(def.Function.Parameters); err == nil && hasCyrillic(string(raw)) {
			offenders = append(offenders, name+": parameters")
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("%d tool definition(s) are not English:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
