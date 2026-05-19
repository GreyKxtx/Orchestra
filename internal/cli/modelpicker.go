package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modelInfo is one model returned by the LM Studio /v1/models endpoint.
type modelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// fetchModels calls GET <apiBase>/v1/models and returns the list.
func fetchModels(apiBase, apiKey string) ([]modelInfo, error) {
	url := strings.TrimRight(apiBase, "/") + "/v1/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach LM Studio at %s: %w", apiBase, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Data []modelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unexpected response: %w", err)
	}
	return payload.Data, nil
}

// pickerResult is what the model picker returns on completion.
type pickerResult struct {
	Model     string
	NumCtx    int
	Cancelled bool
}

type pickerState int

const (
	stateList pickerState = iota
	stateCtx
	stateDone
)

type pickerModel struct {
	models     []modelInfo
	cursor     int
	current    string
	currentCtx int
	ctxInput   string
	state      pickerState
	result     pickerResult

	styleTitle    lipgloss.Style
	styleSelected lipgloss.Style
	styleNormal   lipgloss.Style
	styleCurrent  lipgloss.Style
	styleDim      lipgloss.Style
	stylePrompt   lipgloss.Style
}

func newPickerModel(models []modelInfo, currentModel string, currentCtx int) pickerModel {
	cursor := 0
	for i, m := range models {
		if m.ID == currentModel {
			cursor = i
			break
		}
	}

	return pickerModel{
		models:     models,
		cursor:     cursor,
		current:    currentModel,
		currentCtx: currentCtx,
		ctxInput:   strconv.Itoa(currentCtx),
		state:      stateList,

		styleTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).MarginBottom(1),
		styleSelected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Background(lipgloss.Color("236")).PaddingLeft(2),
		styleNormal:   lipgloss.NewStyle().PaddingLeft(4),
		styleCurrent:  lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("82")),
		styleDim:      lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		stylePrompt:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")),
	}
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case stateList:
			return m.updateList(msg)
		case stateCtx:
			return m.updateCtx(msg)
		}
	}
	return m, nil
}

func (m pickerModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		m.result = pickerResult{Cancelled: true}
		m.state = stateDone
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.models)-1 {
			m.cursor++
		}
	case "enter":
		m.state = stateCtx
	}
	return m, nil
}

func (m pickerModel) updateCtx(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.result = pickerResult{Cancelled: true}
		m.state = stateDone
		return m, tea.Quit
	case "esc":
		m.state = stateList
	case "enter":
		ctx, err := strconv.Atoi(strings.TrimSpace(m.ctxInput))
		if err != nil || ctx <= 0 {
			m.ctxInput = strconv.Itoa(m.currentCtx)
			return m, nil
		}
		m.result = pickerResult{
			Model:  m.models[m.cursor].ID,
			NumCtx: ctx,
		}
		m.state = stateDone
		return m, tea.Quit
	case "backspace":
		if len(m.ctxInput) > 0 {
			m.ctxInput = m.ctxInput[:len(m.ctxInput)-1]
		}
	default:
		if len(msg.String()) == 1 && msg.String() >= "0" && msg.String() <= "9" {
			m.ctxInput += msg.String()
		}
	}
	return m, nil
}

func (m pickerModel) View() string {
	if m.state == stateDone {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.styleTitle.Render("Orchestra — model picker") + "\n")

	if m.state == stateList {
		b.WriteString(m.styleDim.Render("↑↓ select   Enter confirm   q quit") + "\n\n")
		for i, model := range m.models {
			label := model.ID
			if model.ID == m.current {
				label += m.styleDim.Render("  ← current")
			}
			if i == m.cursor {
				b.WriteString(m.styleSelected.Render("▶ "+label) + "\n")
			} else if model.ID == m.current {
				b.WriteString(m.styleCurrent.Render("✓ "+label) + "\n")
			} else {
				b.WriteString(m.styleNormal.Render(label) + "\n")
			}
		}
	} else if m.state == stateCtx {
		selected := m.models[m.cursor].ID
		b.WriteString(fmt.Sprintf("Model: %s\n\n", m.stylePrompt.Render(selected)))
		b.WriteString(m.stylePrompt.Render("Context (tokens): "))
		b.WriteString(m.ctxInput + "█\n")
		b.WriteString(m.styleDim.Render("\nEnter confirm   Esc back") + "\n")
	}

	return b.String()
}

// runModelPicker displays the interactive model picker and returns the result.
func runModelPicker(models []modelInfo, currentModel string, currentCtx int) (pickerResult, error) {
	m := newPickerModel(models, currentModel, currentCtx)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return pickerResult{}, err
	}
	return final.(pickerModel).result, nil
}
