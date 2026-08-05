package sessionfile

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/ops"
)

// ParseSnapshot reads raw JSON from disk, auto-migrating legacy v0 (TUI-only)
// and v1 (core-only) records into v2.
func ParseSnapshot(data []byte, fileID string) (*Snapshot, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("sessionfile: empty snapshot")
	}
	var probe struct {
		Version  int             `json:"version"`
		Messages json.RawMessage `json:"messages"`
		History  json.RawMessage `json:"history"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("sessionfile: parse probe: %w", err)
	}
	if probe.Version >= Version {
		var snap Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			return nil, fmt.Errorf("sessionfile: parse v2: %w", err)
		}
		normalizeSnapshot(&snap, fileID)
		return &snap, nil
	}
	if probe.Version == 1 || (probe.History != nil && probe.Messages == nil) {
		return migrateV1(data, fileID)
	}
	if probe.Messages != nil {
		return migrateV0(data, fileID)
	}
	// Unknown: try v1 then v0.
	if snap, err := migrateV1(data, fileID); err == nil {
		return snap, nil
	}
	return migrateV0(data, fileID)
}

func migrateV1(data []byte, fileID string) (*Snapshot, error) {
	var v1 struct {
		Version      int              `json:"version"`
		ID           string           `json:"id"`
		History      []llm.Message    `json:"history"`
		CreatedAt    time.Time        `json:"created_at"`
		LastActivity time.Time        `json:"last_activity"`
		PendingOps   []ops.AnyOp           `json:"pending_ops,omitempty"`
		Todos        []TodoItem            `json:"todos,omitempty"`
		PlanPath     string           `json:"plan_path,omitempty"`
	}
	if err := json.Unmarshal(data, &v1); err != nil {
		return nil, fmt.Errorf("sessionfile: migrate v1: %w", err)
	}
	id := v1.ID
	if id == "" {
		id = fileID
	}
	updated := v1.LastActivity
	if updated.IsZero() {
		updated = v1.CreatedAt
	}
	snap := &Snapshot{
		Version:    Version,
		ID:         id,
		CreatedAt:  v1.CreatedAt,
		UpdatedAt:  updated,
		History:    v1.History,
		PendingOps: v1.PendingOps,
		Todos:      v1.Todos,
		PlanPath:   v1.PlanPath,
		UIMessages: nil,
	}
	normalizeSnapshot(snap, fileID)
	return snap, nil
}

func migrateV0(data []byte, fileID string) (*Snapshot, error) {
	var v0 struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Model     string    `json:"model,omitempty"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		MsgCount  int       `json:"msg_count"`
		Messages  []v0Msg   `json:"messages"`
	}
	if err := json.Unmarshal(data, &v0); err != nil {
		return nil, fmt.Errorf("sessionfile: migrate v0: %w", err)
	}
	id := v0.ID
	if id == "" {
		id = fileID
	}
	ui := make([]UIMessage, 0, len(v0.Messages))
	for _, m := range v0.Messages {
		ui = append(ui, m.toUIMessage())
	}
	snap := &Snapshot{
		Version:    Version,
		ID:         id,
		Title:      v0.Title,
		Model:      v0.Model,
		CreatedAt:  v0.CreatedAt,
		UpdatedAt:  v0.UpdatedAt,
		History:    nil,
		UIMessages: ui,
		MsgCount:   len(ui),
	}
	normalizeSnapshot(snap, fileID)
	return snap, nil
}

// v0Msg accepts both legacy capitalized keys (state.Message without json tags)
// and lowercase variants.
type v0Msg struct {
	Role       string        `json:"Role"`
	RoleLo     string        `json:"role"`
	Text       string        `json:"Text"`
	TextLo     string        `json:"text"`
	ToolBlocks []v0ToolBlock `json:"ToolBlocks"`
	ToolLo     []v0ToolBlock `json:"tool_blocks"`
	Reasoning  string        `json:"Reasoning"`
	ReasonLo   string        `json:"reasoning"`
	StartedAt  time.Time     `json:"StartedAt"`
	StartedLo  time.Time     `json:"started_at"`
	DurationNS int64         `json:"Duration"`
	DurMS      int64         `json:"duration_ms"`
	TokensIn   int           `json:"TokensIn"`
	TokensInLo int           `json:"tokens_in"`
	TokensOut  int           `json:"TokensOut"`
	TokensOutLo int          `json:"tokens_out"`
	Mode       string        `json:"Mode"`
	ModeLo     string        `json:"mode"`
	Model      string        `json:"Model"`
	ModelLo    string        `json:"model"`
}

type v0ToolBlock struct {
	ID          string `json:"ID"`
	IDLo        string `json:"id"`
	Name        string `json:"Name"`
	NameLo      string `json:"name"`
	ArgsPreview string `json:"ArgsPreview"`
	ArgsLo      string `json:"args_preview"`
	ArgsRaw     string `json:"ArgsRaw"`
	ArgsRawLo   string `json:"args_raw"`
	Status      string `json:"Status"`
	StatusLo    string `json:"status"`
	Result      string `json:"Result"`
	ResultLo    string `json:"result"`
	DurationNS  int64  `json:"Duration"`
	DurMS       int64  `json:"duration_ms"`
}

func (m v0Msg) toUIMessage() UIMessage {
	role := firstNonEmpty(m.RoleLo, m.Role)
	text := firstNonEmpty(m.TextLo, m.Text)
	blocks := m.ToolLo
	if len(blocks) == 0 {
		blocks = m.ToolBlocks
	}
	tools := make([]UIToolBlock, 0, len(blocks))
	for _, b := range blocks {
		tools = append(tools, b.toUI())
	}
	durMS := m.DurMS
	if durMS == 0 && m.DurationNS > 0 {
		durMS = m.DurationNS / int64(time.Millisecond)
	}
	return UIMessage{
		Role:       role,
		Text:       text,
		ToolBlocks: tools,
		Reasoning:  firstNonEmpty(m.ReasonLo, m.Reasoning),
		StartedAt:  firstTime(m.StartedLo, m.StartedAt),
		DurationMS: durMS,
		TokensIn:   firstInt(m.TokensInLo, m.TokensIn),
		TokensOut:  firstInt(m.TokensOutLo, m.TokensOut),
		Mode:       firstNonEmpty(m.ModeLo, m.Mode),
		Model:      firstNonEmpty(m.ModelLo, m.Model),
	}
}

func (b v0ToolBlock) toUI() UIToolBlock {
	durMS := b.DurMS
	if durMS == 0 && b.DurationNS > 0 {
		durMS = b.DurationNS / int64(time.Millisecond)
	}
	return UIToolBlock{
		ID:          firstNonEmpty(b.IDLo, b.ID),
		Name:        firstNonEmpty(b.NameLo, b.Name),
		ArgsPreview: firstNonEmpty(b.ArgsLo, b.ArgsPreview),
		ArgsRaw:     firstNonEmpty(b.ArgsRawLo, b.ArgsRaw),
		Status:      firstNonEmpty(b.StatusLo, b.Status),
		Result:      firstNonEmpty(b.ResultLo, b.Result),
		DurationMS:  durMS,
	}
}

func normalizeSnapshot(s *Snapshot, fileID string) {
	if s == nil {
		return
	}
	s.Version = Version
	if s.ID == "" {
		s.ID = fileID
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = s.CreatedAt
	}
	if s.History == nil {
		s.History = []llm.Message{}
	}
	if s.UIMessages == nil {
		s.UIMessages = []UIMessage{}
	}
	if s.MsgCount == 0 {
		s.MsgCount = len(s.UIMessages)
	}
	if strings.TrimSpace(s.Title) == "" && len(s.UIMessages) > 0 {
		s.Title = TitleFromUIMessages(s.UIMessages)
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func firstTime(a, b time.Time) time.Time {
	if !a.IsZero() {
		return a
	}
	return b
}

func firstInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// TitleFromUIMessages derives a short title from the first user message.
func TitleFromUIMessages(msgs []UIMessage) string {
	for _, m := range msgs {
		if strings.ToLower(m.Role) != "user" {
			continue
		}
		t := strings.TrimSpace(m.Text)
		if t == "" {
			continue
		}
		if i := strings.IndexByte(t, '\n'); i >= 0 {
			t = t[:i]
		}
		const limit = 60
		r := []rune(t)
		if len(r) > limit {
			t = string(r[:limit-1]) + "…"
		}
		return t
	}
	return "(empty session)"
}
