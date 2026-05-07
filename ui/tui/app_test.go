package tui_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/orchestra/orchestra/ui/tui"
)

func TestApp_EchoesUserInput(t *testing.T) {
	app := tui.NewApp(tui.Config{Model: "test-model", Mode: "code", CWD: "test"})
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Type "hello" and submit.
	tm.Type("hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for the echo to appear in output.
	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool {
			return bytes.Contains(b, []byte("echo: hello"))
		},
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_CtrlCQuits(t *testing.T) {
	app := tui.NewApp(tui.Config{})
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_EscResetsInput(t *testing.T) {
	app := tui.NewApp(tui.Config{})
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Type something then Esc to reset.
	tm.Type("some text")
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// Quit cleanly.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_EnterEmptyInputDoesNothing(t *testing.T) {
	app := tui.NewApp(tui.Config{})
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Press enter without typing anything.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))

	out := readAll(tm.FinalOutput(t, teatest.WithFinalTimeout(time.Second)))
	if strings.Contains(out, "echo: ") {
		t.Errorf("empty Enter should not produce an echo, got output:\n%s", out)
	}
}

// readAll drains an io.Reader to a string, used for final output.
func readAll(r io.Reader) string {
	var b bytes.Buffer
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return b.String()
}
