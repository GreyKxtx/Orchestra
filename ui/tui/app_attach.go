package tui

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/attachments"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
)

type attachResultMsg struct {
	att state.Attachment
	err error
}

func (a *App) cmdAttachFile(path string) tea.Cmd {
	path = strings.TrimSpace(path)
	if path == "" {
		a.showToast("/attach <path>")
		return nil
	}
	root := a.cfg.WorkspaceRoot
	return func() tea.Msg {
		wsPath, err := attachments.StageIntoWorkspace(root, path)
		if err != nil {
			return attachResultMsg{err: err}
		}
		meta := attachments.MessageAttachmentFromPath(wsPath)
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(meta.Name)), ".")
		return attachResultMsg{att: state.Attachment{
			Path: meta.Path,
			Name: meta.Name,
			Kind: meta.Kind,
			MIME: meta.MIME,
			Ext:  ext,
		}}
	}
}

func (a *App) handleAttachResult(m attachResultMsg) tea.Cmd {
	if m.err != nil {
		a.showToast("attach: " + m.err.Error())
		return nil
	}
	a.stagedAttachments = append(a.stagedAttachments, m.att)
	name := m.att.Name
	if name == "" {
		name = filepath.Base(m.att.Path)
	}
	a.showToast("📎 " + name)
	a.updateStatusHints()
	return nil
}

func rpcAttachmentsFromState(atts []state.Attachment) []rpcclient.RPCAttachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]rpcclient.RPCAttachment, 0, len(atts))
	for _, att := range atts {
		out = append(out, rpcclient.RPCAttachment{
			Path: att.Path,
			Kind: att.Kind,
			MIME: att.MIME,
			Name: att.Name,
		})
	}
	return out
}

func (a *App) takeStagedAttachments() []state.Attachment {
	if len(a.stagedAttachments) == 0 {
		return nil
	}
	out := append([]state.Attachment(nil), a.stagedAttachments...)
	a.stagedAttachments = nil
	return out
}