package prompt

import _ "embed"

// PlanModeReminder is injected into every user message in plan mode.
//
//go:embed files/plan-reminder.txt
var PlanModeReminder string

// BuildSwitchReminder is injected once when switching from plan to build mode.
//
//go:embed files/plan-switch.txt
var BuildSwitchReminder string

// MaxStepsReminder is injected as a synthetic assistant message when approaching the step limit.
//
//go:embed files/max-steps.txt
var MaxStepsReminder string
