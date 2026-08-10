package tui

import "time"

const (
	// chatRenderMinInterval throttles full chat rebuilds while tokens stream in.
	chatRenderMinInterval = 200 * time.Millisecond
	// chatSpinnerInterval refreshes the in-turn spinner without new content.
	chatSpinnerInterval = 500 * time.Millisecond
)

// flushChat rebuilds the scrollback viewport from session state.
// force=true bypasses throttling (tool completion, turn end, window resize).
func (a *App) flushChat(force bool) {
	if !force {
		if !a.turn.ShowBusySpinner() && !a.chatDirty {
			return
		}
		now := time.Now()
		if a.chatDirty {
			if now.Sub(a.lastChatRender) < chatRenderMinInterval {
				return
			}
		} else if now.Sub(a.lastChatRender) < chatSpinnerInterval {
			return
		}
	}
	a.session.SyncActiveAssistantProjections()
	a.chat.SetMessages(a.session.Messages)
	a.chatDirty = false
	a.lastChatRender = time.Now()
}
