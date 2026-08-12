package view

import tea "github.com/charmbracelet/bubbletea"

// Typed dialog results. Each dialog emits its own message type with typed
// payload fields, so App routes them through a compile-time-checked type
// switch instead of matching "source"/"action" strings and asserting a
// Data any payload.

// resultCmd wraps a ready message into a tea.Cmd.
func resultCmd(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// ProviderDialogMsg — provider picker: cancel or pick an entry.
type ProviderDialogMsg struct {
	Cancel   bool
	Provider ProviderEntry
}

// EndpointDialogMsg — credentials step: cancel or save endpoint/API key.
type EndpointDialogMsg struct {
	Cancel bool
	Result EndpointDialogResult
}

// ModelDialogMsg — model picker: cancel or pick a model.
type ModelDialogMsg struct {
	Cancel bool
	Model  ModelEntry
}

// SettingsDialogMsg — tuning step: cancel or save settings.
type SettingsDialogMsg struct {
	Cancel bool
	Result SettingsDialogResult
}

// OrchestraDialogAction enumerates orchestra-dialog outcomes.
type OrchestraDialogAction int

const (
	OrchestraCancel OrchestraDialogAction = iota
	OrchestraSave
	OrchestraPickProvider
	OrchestraPickModel
)

// OrchestraDialogMsg — roles editor: save all, or drill into one role.
type OrchestraDialogMsg struct {
	Action  OrchestraDialogAction
	Result  OrchestraDialogResult // set for OrchestraSave
	RoleIdx int                   // set for OrchestraPickProvider / OrchestraPickModel
}

// OrchestraSourceDialogMsg — provider source for a role: cancel or pick.
type OrchestraSourceDialogMsg struct {
	Cancel bool
	Pick   OrchestraSourcePick
}

// SessionsDialogAction enumerates sessions-dialog outcomes.
type SessionsDialogAction int

const (
	SessionsCancel SessionsDialogAction = iota
	SessionsSelect
	SessionsDelete
)

// SessionsDialogMsg — session list: open or delete a session by id.
type SessionsDialogMsg struct {
	Action SessionsDialogAction
	ID     string
}

// RewindDialogMsg — checkpoint picker: cancel or rewind to a checkpoint.
type RewindDialogMsg struct {
	Cancel     bool
	Checkpoint RewindCheckpoint
}

// MessageActionKind enumerates message-context-menu outcomes.
type MessageActionKind int

const (
	MessageActionCancel MessageActionKind = iota
	MessageActionCopy
	MessageActionEdit
)

// MessageActionDialogMsg — chat message context menu.
type MessageActionDialogMsg struct {
	Action MessageActionKind
	Text   string
}

// MCPListDialogAction enumerates MCP list outcomes.
type MCPListDialogAction int

const (
	MCPListCancel MCPListDialogAction = iota
	MCPListAdd
	MCPListEdit
	MCPListDelete
	MCPListToggle
	MCPListTest // ServerName empty ⇒ test all servers
)

// MCPListDialogMsg — MCP server list actions.
type MCPListDialogMsg struct {
	Action     MCPListDialogAction
	ServerName string        // delete / toggle / test target
	Server     MCPServerView // set for MCPListEdit
}

// MCPPresetDialogMsg — preset catalog: cancel or pick a preset.
type MCPPresetDialogMsg struct {
	Cancel bool
	Preset MCPPreset
}

// MCPEditDialogMsg — server editor: cancel or save.
type MCPEditDialogMsg struct {
	Cancel bool
	Result MCPEditDialogResult
}
