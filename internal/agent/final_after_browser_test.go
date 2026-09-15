package agent

import "testing"

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
