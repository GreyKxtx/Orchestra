package web

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakePanel records what it was asked and answers with canned JSON.
type fakePanel struct {
	ops    []string
	params []map[string]any
	answer string
	err    error
}

func (f *fakePanel) Call(ctx context.Context, op string, params map[string]any) (json.RawMessage, error) {
	f.ops = append(f.ops, op)
	f.params = append(f.params, params)
	if f.err != nil {
		return nil, f.err
	}
	return json.RawMessage(f.answer), nil
}

// The client's own browser is what the tools act on when the turn has one —
// without a Playwright server configured at all, which is the desktop case.
func TestPanelAnswersSnapshotAndScreenshot(t *testing.T) {
	panel := &fakePanel{answer: `{"snapshot":"page https://example.com/\n[a1] link \"Learn more\""}`}
	ctx := WithPanel(context.Background(), panel, PanelPermits{})

	snap, err := BrowserSnapshot(ctx, Config{}, BrowserSnapshotRequest{})
	if err != nil {
		t.Fatalf("snapshot through the panel: %v", err)
	}
	if snap.Snapshot == "" || snap.Snapshot[:4] != "page" {
		t.Errorf("the panel's text is the answer, got %q", snap.Snapshot)
	}
	if len(panel.ops) != 1 || panel.ops[0] != "snapshot" {
		t.Errorf("asked %v, want one snapshot", panel.ops)
	}

	panel.answer = `{"image":"iVBORw0KGgo="}`
	shot, err := BrowserScreenshot(ctx, Config{}, BrowserScreenshotRequest{FullPage: true})
	if err != nil {
		t.Fatalf("screenshot through the panel: %v", err)
	}
	if shot.Image != "iVBORw0KGgo=" {
		t.Errorf("image = %q", shot.Image)
	}
	if got := panel.params[1]["full_page"]; got != true {
		t.Errorf("full_page did not reach the client: %v", got)
	}
}

// A refusal — the view is closed, consent was taken back mid-turn — is the
// tool's answer, not a crash.
func TestPanelRefusalIsTheToolsAnswer(t *testing.T) {
	panel := &fakePanel{err: errors.New("the browser panel is not open")}
	ctx := WithPanel(context.Background(), panel, PanelPermits{})

	if _, err := BrowserSnapshot(ctx, Config{}, BrowserSnapshotRequest{}); err == nil {
		t.Fatal("a refused op must not answer with a snapshot")
	} else if err.Error() == "" {
		t.Error("the refusal's reason is what the model is told")
	}
}

// Neither browser: the tools say what is missing rather than panicking on a
// nil client. This is every CLI and CI run.
func TestWithNeitherBrowserTheToolsRefuse(t *testing.T) {
	ctx := context.Background()
	if _, err := BrowserSnapshot(ctx, Config{}, BrowserSnapshotRequest{}); err == nil {
		t.Fatal("snapshot without a browser must refuse")
	}
	if _, err := BrowserScreenshot(ctx, Config{}, BrowserScreenshotRequest{}); err == nil {
		t.Fatal("screenshot without a browser must refuse")
	}
	if PanelFrom(ctx) != nil {
		t.Error("a bare context carries no panel")
	}
	if PanelFrom(WithPanel(ctx, nil, PanelPermits{Drive: true})) != nil {
		t.Error("a nil panel is not carried")
	}
}

// Looking at the page is one permission; pressing things on it is another,
// and running script in it a third. A turn given the first gets the first
// only — and is told which switch it is missing, because the person can turn
// it on and the model cannot.
func TestEachLevelIsAskedForSeparately(t *testing.T) {
	panel := &fakePanel{answer: `{"result":"done","filled":1}`}
	look := WithPanel(context.Background(), panel, PanelPermits{})
	drive := WithPanel(context.Background(), panel, PanelPermits{Drive: true})
	script := WithPanel(context.Background(), panel, PanelPermits{Drive: true, Eval: true})

	if _, err := BrowserSnapshot(look, Config{}, BrowserSnapshotRequest{}); err != nil {
		t.Errorf("looking was refused: %v", err)
	}

	acts := []struct {
		what string
		call func(context.Context) error
	}{
		{"click", func(c context.Context) error {
			_, err := BrowserClick(c, Config{}, BrowserClickRequest{Ref: "a1"})
			return err
		}},
		{"type", func(c context.Context) error {
			_, err := BrowserType(c, Config{}, BrowserTypeRequest{Ref: "a1", Text: "hello"})
			return err
		}},
		{"navigate", func(c context.Context) error {
			_, err := BrowserNavigate(c, Config{}, BrowserNavigateRequest{URL: "https://example.com/"})
			return err
		}},
		{"fill", func(c context.Context) error {
			_, err := BrowserFill(c, Config{}, BrowserFillRequest{
				Fields: []BrowserFillField{{Ref: "a1", Value: "x"}},
			})
			return err
		}},
		{"select", func(c context.Context) error {
			_, err := BrowserSelect(c, Config{}, BrowserSelectRequest{Ref: "a1", Value: "x"})
			return err
		}},
	}
	for _, act := range acts {
		if err := act.call(look); err == nil {
			t.Errorf("%s went through on a turn that may only look", act.what)
		} else if !strings.Contains(err.Error(), "access menu") {
			t.Errorf("%s: the refusal must say where the switch is, got %v", act.what, err)
		}
		if err := act.call(drive); err != nil {
			t.Errorf("%s was refused on a turn that may drive: %v", act.what, err)
		}
	}

	// Script has its own switch, which driving does not carry.
	if _, err := BrowserEval(drive, Config{}, BrowserEvalRequest{Expression: "1+1"}); err == nil {
		t.Error("browser.eval ran on a turn that was only allowed to drive")
	}
	if _, err := BrowserEval(script, Config{}, BrowserEvalRequest{Expression: "1+1"}); err != nil {
		t.Errorf("browser.eval was refused on a turn that may run script: %v", err)
	}

	// The panel is the person's window; a tool does not close it.
	if _, err := BrowserClose(script, Config{}, BrowserCloseRequest{}); err == nil {
		t.Error("browser.close closed the person's own browser view")
	}
}
