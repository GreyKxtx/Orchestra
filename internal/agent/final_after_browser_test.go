package agent

import (
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// A turn whose job was on a web page could not say it had finished.
//
// queryRequiresCodeChanges reads action words in the query, and a page task is
// full of them: "click Create account", "add to cart", "update the profile".
// The turn filled the form and clicked the button, but none of that is a
// workspace mutation, so its answer was refused with advice to edit a file:
//
//	browser.fill, browser.select, browser.click, browser.snapshot -> the page says "Created Ada Lovelace …"
//	(validation_error: Task requires code changes but no edit/write was performed)
//	(validation_error)    -> "model repeatedly produced invalid output"
//
// Acting on the page is the turn's work, the way memory_write is.
func TestPrematureFinal_ATurnThatActedOnAPageMayFinish(t *testing.T) {
	for _, tool := range []string{"browser.click", "browser.type", "browser.fill", "browser.select"} {
		a := &Agent{}
		a.countMutatingTool(tool)

		hint, reject := a.rejectPrematureFinal(
			"Fill the form, click Create account, then tell me the confirmation text.",
			&Step{Type: StepFinal, Final: &Final{}},
			`The confirmation text is "Created Ada Lovelace <ada@example.com> on plan pro".`,
			7,
		)
		if reject {
			t.Errorf("after %s the turn was refused its final:\n%s", tool, hint)
		}
	}
}

// Looking at a page is not work, or a turn asked to fix code could open a page
// and claim it was done.
func TestPrematureFinal_ReadingAPageIsNotAMutation(t *testing.T) {
	a := &Agent{}
	for _, tool := range []string{"browser.navigate", "browser.snapshot", "browser.screenshot", "browser.wait", "browser.eval", "browser.close"} {
		a.countMutatingTool(tool)
	}
	if a.turnMutatingTools != 0 {
		t.Errorf("page reads counted as mutations: %d", a.turnMutatingTools)
	}
}

// The query-word heuristic behind "Task requires code changes" refused five
// correct answers in one day of live runs ("Create account", "Do not change",
// "until it finishes", "in their comment"). A turn dies on the second refusal
// when max_invalid_retries is 1. The reminder now comes once per turn: a model
// that answers again without edits is taken at its word. Open todos are a
// different, exact signal and still block every time.
func TestPrematureFinal_TheCodeChangeReminderComesOncePerTurn(t *testing.T) {
	a := &Agent{}
	final := &Step{Type: StepFinal, Final: &Final{}}
	const query = "list the courierStep functions that mention refund in their comment"
	const answer = "courierStep18, courierStep22 and courierStep23 mention refund."

	if _, reject := a.rejectPrematureFinal(query, final, answer, 4); !reject {
		t.Fatal("the first prose final on a query that reads as a change request should get the reminder")
	}
	if hint, reject := a.rejectPrematureFinal(query, final, answer, 5); reject {
		t.Errorf("the same answer after the reminder was refused again:\n%s", hint)
	}

	withTodos := &Agent{todos: []tools.TodoItem{{ID: "1", Content: "edit", Status: tools.TodoPending}}}
	for step := 4; step < 7; step++ {
		if _, reject := withTodos.rejectPrematureFinal(query, final, answer, step); !reject {
			t.Fatalf("an open todo stopped blocking the final at step %d", step)
		}
	}
}
