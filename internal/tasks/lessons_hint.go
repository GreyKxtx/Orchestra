package tasks

import (
	"encoding/json"
	"strings"
)

// annotateLessonPromoteSuggestion attaches lesson_promote_suggestion to JSON task results.
func annotateLessonPromoteSuggestion(taskResult, hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return taskResult
	}
	return attachBarrierPayload(taskResult, map[string]any{
		"lesson_promote_suggestion": hint,
	})
}

// annotatePlaybookPromoteSuggestion attaches playbook_promote_suggestion to JSON task results.
func annotatePlaybookPromoteSuggestion(taskResult, hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return taskResult
	}
	return attachBarrierPayload(taskResult, map[string]any{
		"playbook_promote_suggestion": hint,
	})
}

// extractPromoteHintForTest exposes JSON field for tests.
func extractPromoteHintForTest(taskResult string) string {
	raw := strings.TrimSpace(taskResult)
	if raw == "" || !json.Valid([]byte(raw)) {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	s, _ := m["lesson_promote_suggestion"].(string)
	return strings.TrimSpace(s)
}

func extractPlaybookPromoteHintForTest(taskResult string) string {
	raw := strings.TrimSpace(taskResult)
	if raw == "" || !json.Valid([]byte(raw)) {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	s, _ := m["playbook_promote_suggestion"].(string)
	return strings.TrimSpace(s)
}
