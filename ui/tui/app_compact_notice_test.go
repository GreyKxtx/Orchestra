package tui

import (
	"strings"
	"testing"
)

// /compact summarizes the older half and keeps the recent tail, so the message
// count often does not drop — a real run reported "Контекст сжат: 8 → 8 msgs",
// which reads as "nothing happened, and it went fine". The count is not what
// compaction is for; it is only worth printing when it actually fell.
//
// The status bar was also set to zero, claiming an empty context. The number
// belongs to the last turn's prompt; the next turn replaces it.
func TestCompactNotice_SaysWhatHappenedAndKeepsTheLastPromptSize(t *testing.T) {
	a := testChromeApp(t)
	a.chrome.promptTokensUsed = 10200

	a.handleSessionCompactDone(sessionCompactDoneMsg{before: 8, after: 8})
	if !hasSystemNotice(a, "сводк") {
		t.Errorf("a compaction that did not shorten the history must say what it did: %s", lastNoticeText(a))
	}
	if strings.Contains(lastNoticeText(a), "8 → 8") {
		t.Errorf("«8 → 8» reads as a no-op: %s", lastNoticeText(a))
	}
	if a.chrome.promptTokensUsed != 10200 {
		t.Errorf("the last turn's prompt size was thrown away: %d", a.chrome.promptTokensUsed)
	}

	b := testChromeApp(t)
	b.handleSessionCompactDone(sessionCompactDoneMsg{before: 24, after: 9})
	if !strings.Contains(lastNoticeText(b), "24") || !strings.Contains(lastNoticeText(b), "9") {
		t.Errorf("a compaction that shortened the history must show both counts: %s", lastNoticeText(b))
	}
}

func lastNoticeText(a *App) string {
	for i := len(a.session.Messages) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(a.session.Messages[i].Text); t != "" {
			return t
		}
	}
	return ""
}
