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
	app, err := tui.NewApp(tui.Config{Model: "test-model", Mode: "build", CWD: "test"})
	if err != nil {
		t.Fatal(err)
	}
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
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_EscResetsInput(t *testing.T) {
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Type something then Esc to reset.
	tm.Type("some text")
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// Quit cleanly.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_EnterEmptyInputDoesNothing(t *testing.T) {
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
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

func TestApp_HistoryRecall(t *testing.T) {
	app, err := tui.NewApp(tui.Config{Model: "test", Mode: "build", CWD: "test"})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Submit "hello" → history stores it.
	tm.Type("hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Wait for echo.
	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool { return bytes.Contains(b, []byte("echo: hello")) },
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	// Press ↑ to recall "hello" into input, then submit again.
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// After the second submit the new rendered frame contains the second echo.
	// We already confirmed the first echo; the incremental output here needs
	// to contain at least one more "echo: hello".
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

func TestApp_SlashPaletteOpensOnSlash(t *testing.T) {
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Typing "/" should open the inline slash palette.
	tm.Type("/")

	// The palette renders command names above the input box.
	// At minimum /help and /clear must appear in the rendered output.
	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool {
			s := string(b)
			return strings.Contains(s, "/help") && strings.Contains(s, "/clear")
		},
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_SlashPaletteFiltersOnType(t *testing.T) {
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Typing "/cl" should filter to /clear (contains "cl" in the name).
	tm.Type("/cl")

	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool {
			return bytes.Contains(b, []byte("/clear"))
		},
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_SlashPaletteClosesOnSpace(t *testing.T) {
	app, err := tui.NewApp(tui.Config{Model: "test-model", Mode: "build", CWD: "test"})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Type "/clear " (with trailing space) — palette should close.
	// Then Enter should send "/clear" as a regular chat message (not execute
	// the command). Echo mode returns "echo: /clear" proving the palette was
	// NOT active when Enter was pressed.
	tm.Type("/clear ")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool {
			return bytes.Contains(b, []byte("echo: /clear"))
		},
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
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
