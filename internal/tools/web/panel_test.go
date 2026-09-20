package web

import (
	"context"
	"encoding/json"
	"errors"
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
	ctx := WithPanel(context.Background(), panel)

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
	ctx := WithPanel(context.Background(), panel)

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
	if PanelFrom(WithPanel(ctx, nil)) != nil {
		t.Error("a nil panel is not carried")
	}
}
