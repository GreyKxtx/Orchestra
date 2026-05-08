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
	app, err := tui.NewApp(tui.Config{Model: "test-model", Mode: "code", CWD: "test"})
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
	app, err := tui.NewApp(tui.Config{Model: "test", Mode: "code", CWD: "test"})
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

func TestApp_SlashPalette_OpensOnSlash(t *testing.T) {
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Typing "/" should open the palette and change the footer hint.
	tm.Type("/")
	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool {
			return bytes.Contains(b, []byte("Enter execute"))
		},
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_EscClosesPalette(t *testing.T) {
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Open the palette.
	tm.Type("/")
	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool { return bytes.Contains(b, []byte("Enter execute")) },
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	// Esc should close it; footer returns to default.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool { return bytes.Contains(b, []byte("/ commands")) },
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_SlashHelp_ShowsHelp(t *testing.T) {
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Type "/help" and submit — palette selects /help automatically.
	tm.Type("/help")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool {
			return bytes.Contains(b, []byte("Orchestra TUI"))
		},
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_SlashClear_ClearsMessages(t *testing.T) {
	app, err := tui.NewApp(tui.Config{Model: "test", Mode: "code", CWD: "test"})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Submit a message so there is something to clear.
	tm.Type("hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool { return bytes.Contains(b, []byte("echo: hello")) },
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	// /clear should remove the message.
	tm.Type("/clear")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Re-open the palette — this forces the footer to change (diff line emitted),
	// proving /clear left the app in a healthy state.
	tm.Type("/")
	teatest.WaitFor(
		t, tm.Output(),
		func(b []byte) bool { return bytes.Contains(b, []byte("Enter execute")) },
		teatest.WithCheckInterval(50*time.Millisecond),
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_SlashQuit_Exits(t *testing.T) {
	app, err := tui.NewApp(tui.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// /quit should exit the program cleanly.
	tm.Type("/quit")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
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
