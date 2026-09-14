package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// lesson_promote and playbook_promote are the last two tools with no coverage
// of their substance. gated_tool_exposure_test.go pins WHO may call them; this
// pins what happens when someone does.
//
// They are a two-step protocol, and the interesting part is the seam. The
// answer to lesson_promote is the model's only instruction about what comes
// next: "Draft local overlay written. Ask the User via open_questions; the
// runtime auto-seals decision_ref. Then call playbook_promote (same approval
// covers merge)." A model reads that and calls playbook_promote — so what
// happens on that call, with the draft still unapproved, is a contract and not
// an implementation detail. The duplicate-edit blocker is the warning here: it
// told the model to do the very thing it had just refused.

func lessonAgent(t *testing.T) *Agent {
	t.Helper()
	return agentInMode(t, ModeArchitecture)
}

func TestLessonPromote_WritesADraftAndSaysWhereItWent(t *testing.T) {
	ag := lessonAgent(t)

	raw, err := ag.handleLessonPromote(context.Background(),
		json.RawMessage(`{"dept":"frontend","note":"prefer composition over inheritance for widgets"}`))
	if err != nil {
		t.Fatalf("lesson_promote: %v", err)
	}
	out := string(raw)

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("the answer is not JSON, so the model cannot act on it: %s", out)
	}
	if p, _ := payload["path"].(string); strings.TrimSpace(p) == "" {
		t.Errorf("the answer does not name the file it wrote, so neither the model nor the "+
			"user can look at what is being proposed:\n%s", out)
	}
	if s, _ := payload["status"].(string); s != "draft" {
		t.Errorf("status is %q; a draft that does not say it is a draft reads as a finished "+
			"promotion:\n%s", s, out)
	}
	if ref, _ := payload["decision_ref"].(string); !strings.Contains(ref, "PENDING") {
		t.Errorf("decision_ref does not mark the overlay as unapproved:\n%s", out)
	}
}

// The seam. The answer to lesson_promote tells the model to call
// playbook_promote next, so calling it next must not leave the model stuck
// without knowing why.
func TestLessonPromote_TheNextStepItNamesIsUsableOrRefusesClearly(t *testing.T) {
	ag := lessonAgent(t)

	first, err := ag.handleLessonPromote(context.Background(),
		json.RawMessage(`{"dept":"frontend","note":"prefer composition over inheritance for widgets"}`))
	if err != nil {
		t.Fatalf("lesson_promote: %v", err)
	}
	var draft map[string]any
	_ = json.Unmarshal(first, &draft)
	msg, _ := draft["message"].(string)
	if !strings.Contains(msg, "playbook_promote") {
		t.Fatalf("lesson_promote no longer points at playbook_promote; this test is about "+
			"that instruction:\n%s", first)
	}

	second, promoteErr := ag.handlePlaybookPromote(context.Background(),
		json.RawMessage(`{"dept":"frontend"}`))

	if promoteErr == nil {
		// Merging an unapproved draft would make the approval step decorative.
		var merged map[string]any
		if err := json.Unmarshal(second, &merged); err != nil {
			t.Fatalf("playbook_promote answered with non-JSON: %s", second)
		}
		ref, _ := merged["promotion_ref"].(string)
		if strings.Contains(ref, "PENDING") {
			t.Errorf("playbook_promote merged a draft whose decision_ref is still PENDING, so "+
				"the user approval the first step asks for changes nothing:\n%s", second)
		}
		return
	}

	// A refusal is the right answer for an unapproved draft — but it has to say
	// what is missing. "Ask the user, then call me" followed by a refusal that
	// does not mention approval is how a model ends up calling the same tool
	// until the breaker stops it.
	low := strings.ToLower(promoteErr.Error())
	mentionsWhy := strings.Contains(low, "approv") || strings.Contains(low, "pending") ||
		strings.Contains(low, "decision_ref") || strings.Contains(low, "overlay")
	if !mentionsWhy {
		t.Errorf("playbook_promote refused the step lesson_promote told the model to take, "+
			"without naming approval, the decision_ref or the overlay as the reason:\n%v",
			promoteErr)
	}
}

// dept is not required, and finding that out is the point of this test.
// lessons.NormalizeDept turns both an empty dept AND any name that fails its
// pattern into "engineering", so a lesson filed under a misspelt department
// lands in engineering with nothing said about it. Both handlers still carry a
// `dept == ""` guard, and neither can ever fire.
//
// That is defensible — filing a lesson somewhere beats dropping it — but it is
// silent, and silent routing is the kind of thing that changes by accident.
// Pinned here so it changes on purpose.
func TestLearningTools_AnEmptyOrMisspeltDeptQuietlyBecomesEngineering(t *testing.T) {
	ag := lessonAgent(t)

	raw, err := ag.handleLessonPromote(context.Background(), json.RawMessage(`{"note":"x"}`))
	if err != nil {
		t.Fatalf("a note with no dept was refused; if that is the new rule, the guard in the "+
			"handler is finally reachable and this test should assert the refusal: %v", err)
	}
	if !strings.Contains(string(raw), "engineering") {
		t.Errorf("an empty dept no longer routes to engineering; where did the lesson go?\n%s", raw)
	}

	raw, err = ag.handleLessonPromote(context.Background(),
		json.RawMessage(`{"dept":"front end!!","note":"x"}`))
	if err != nil {
		t.Fatalf("a misspelt dept was refused: %v", err)
	}
	if !strings.Contains(string(raw), "engineering") {
		t.Errorf("a dept that fails the name pattern no longer falls back to engineering:\n%s", raw)
	}
}

// Promotion without an approved overlay is the common mistake, and the refusal
// has to say that approval is what is missing — not merely that something is
// empty.
func TestPlaybookPromote_WithoutAnApprovedOverlaySaysApprovalIsWhatIsMissing(t *testing.T) {
	ag := lessonAgent(t)

	_, err := ag.handlePlaybookPromote(context.Background(), json.RawMessage(`{"dept":"frontend"}`))
	if err == nil {
		t.Fatal("a promotion with no approved overlay was accepted, so the approval step " +
			"decides nothing")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "approve") && !strings.Contains(low, "overlay") {
		t.Errorf("the refusal does not point at the approval that is missing, leaving the "+
			"model to retry the same call: %v", err)
	}
}
