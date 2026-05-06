package prompt

// PlanModeReminder is injected into every user message in plan mode.
const PlanModeReminder = `РЕЖИМ ПЛАНИРОВАНИЯ АКТИВЕН. СТРОГО ЗАПРЕЩЕНО: write и edit (кроме .orchestra/plan.md), bash. Анализируй кодовую базу, задавай вопросы через question, запиши план в .orchestra/plan.md, затем вызови plan_exit.`

// BuildSwitchReminder is injected once when switching from plan to build mode.
const BuildSwitchReminder = `Режим изменён: ПЛАН → BUILD. Теперь разрешены все инструменты. Выполни согласованный план.`
