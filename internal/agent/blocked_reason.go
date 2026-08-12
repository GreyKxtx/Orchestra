package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// blocked_reason taxonomy (spec §5.2): a closed list so the runtime and the
// orchestrator can route failures automatically. Free text does not route.
var validBlockedReasons = map[string]struct{}{
	"stale_contract":    {},
	"missing_answer":    {},
	"verify_failed":     {},
	"permission_denied": {},
	"tier_exhausted":    {},
	"dependency_unmet":  {},
}

// ValidBlockedReason reports whether s is in the closed taxonomy.
func ValidBlockedReason(s string) bool {
	_, ok := validBlockedReasons[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

// BlockedReasons returns the taxonomy, sorted, for error messages and docs.
func BlockedReasons() []string {
	out := make([]string, 0, len(validBlockedReasons))
	for k := range validBlockedReasons {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkBlockedReasonTaxonomy rejects a task_result whose blocked_reason is
// outside the closed list, so the child corrects it before finishing.
func checkBlockedReasonTaxonomy(content string) error {
	content = strings.TrimSpace(content)
	if content == "" || !strings.HasPrefix(content, "{") || !json.Valid([]byte(content)) {
		return nil
	}
	var payload struct {
		BlockedReason string `json:"blocked_reason"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil
	}
	if payload.BlockedReason == "" || ValidBlockedReason(payload.BlockedReason) {
		return nil
	}
	return fmt.Errorf("blocked_reason %q is not in the closed taxonomy [%s]; pick the closest value and call task_result again",
		payload.BlockedReason, strings.Join(BlockedReasons(), " | "))
}
