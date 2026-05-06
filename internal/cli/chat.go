package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/cache"
	"github.com/spf13/cobra"
)

var (
	chatWorkspace string
	chatAllowExec bool
	chatApply     bool
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive multi-turn chat with the LLM",
	Long:  "Starts an interactive REPL that maintains a session across turns. Commands: /exit /clear /diff /apply /cancel",
	Args:  cobra.NoArgs,
	RunE:  runChat,
}

func init() {
	chatCmd.Flags().StringVar(&chatWorkspace, "workspace", "", "Workspace root (default: current directory)")
	chatCmd.Flags().BoolVar(&chatAllowExec, "allow-exec", false, "Allow exec.run tool")
	chatCmd.Flags().BoolVar(&chatApply, "apply", false, "Auto-apply changes after each turn")
	rootCmd.AddCommand(chatCmd)
}

func runChat(cmd *cobra.Command, args []string) error {
	workspace := chatWorkspace
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		workspace = cwd
	}

	// Use a long-lived context for the subprocess; per-turn cancellation uses sub-contexts.
	chatCtx, chatCancel := context.WithCancel(context.Background())
	defer chatCancel()

	child, err := spawnCoreChild(chatCtx, workspace)
	if err != nil {
		return fmt.Errorf("start core subprocess: %w", err)
	}
	defer child.Close()

	// textBuf accumulates streamed text content so we can strip the trailing
	// {"patches":[...]} JSON before showing the response to the user.
	var textBuf strings.Builder

	// Stream agent events and exec output to stderr while a turn is running.
	child.Client.SetNotificationHandler(func(method string, params json.RawMessage) {
		switch method {
		case "agent/event":
			printChatEventBuffered(params, &textBuf)
		case "exec/output_chunk":
			var ev struct{ Chunk string `json:"chunk"` }
			if err := json.Unmarshal(params, &ev); err == nil && ev.Chunk != "" {
				fmt.Fprint(os.Stderr, ev.Chunk)
			}
		}
	})

	// Initialize the core subprocess.
	projectID, err := cache.ComputeProjectID(workspace)
	if err != nil {
		return err
	}
	var initRes core.InitializeResult
	if err := child.Client.Call(chatCtx, "initialize", core.InitializeParams{
		ProjectRoot:     workspace,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
	}, &initRes); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Start a session.
	var startRes core.SessionStartResult
	if err := child.Client.Call(chatCtx, "session.start", core.SessionStartParams{}, &startRes); err != nil {
		return fmt.Errorf("session.start: %w", err)
	}
	sessionID := startRes.SessionID

	fmt.Fprintf(os.Stderr, "Orchestra chat  session=%s\n", sessionID[:8])
	fmt.Fprintf(os.Stderr, "Commands: /exit /clear /diff /apply /cancel\n\n")

	var lastResult *core.SessionMessageResult
	sigCh := make(chan os.Signal, 1)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "you> ")
		if !scanner.Scan() {
			break // EOF or Ctrl-C during input
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch line {
		case "/exit":
			_ = child.Client.Call(context.Background(), "session.close",
				core.SessionCloseParams{SessionID: sessionID}, nil)
			return nil

		case "/clear":
			_ = child.Client.Call(context.Background(), "session.close",
				core.SessionCloseParams{SessionID: sessionID}, nil)
			var sr core.SessionStartResult
			if err := child.Client.Call(context.Background(), "session.start",
				core.SessionStartParams{}, &sr); err != nil {
				fmt.Fprintf(os.Stderr, "error restarting session: %v\n", err)
				continue
			}
			sessionID = sr.SessionID
			lastResult = nil
			fmt.Fprintln(os.Stderr, "[session cleared]")

		case "/diff":
			if lastResult == nil || len(lastResult.Patches) == 0 {
				fmt.Fprintln(os.Stderr, "[no pending patches]")
				continue
			}
			printChatPatches(lastResult.Patches)

		case "/apply":
			var applyRes core.SessionApplyPendingResult
			if err := child.Client.Call(context.Background(), "session.apply_pending",
				core.SessionApplyPendingParams{SessionID: sessionID}, &applyRes); err != nil {
				fmt.Fprintf(os.Stderr, "apply error: %v\n", err)
				continue
			}
			if applyRes.Applied {
				fmt.Fprintln(os.Stderr, "[changes applied]")
			} else {
				fmt.Fprintln(os.Stderr, "[no pending changes]")
			}

		case "/cancel":
			if err := child.Client.Call(context.Background(), "session.cancel",
				core.SessionCancelParams{SessionID: sessionID}, nil); err != nil {
				fmt.Fprintf(os.Stderr, "cancel error: %v\n", err)
			}

		default:
			textBuf.Reset() // reset buffer for each new turn
			lastResult = runChatTurn(child, sigCh, sessionID, line, chatApply, chatAllowExec)
		}
	}

	_ = child.Client.Call(context.Background(), "session.close",
		core.SessionCloseParams{SessionID: sessionID}, nil)
	return nil
}

// runChatTurn sends one message to the session with Ctrl-C cancel support.
// Returns the result on success, nil on error or cancellation.
func runChatTurn(child *CoreChild, sigCh chan os.Signal, sessionID, content string, apply, allowExec bool) *core.SessionMessageResult {
	turnCtx, cancelTurn := context.WithCancel(context.Background())
	turnDone := make(chan struct{})

	signal.Notify(sigCh, os.Interrupt)
	go func() {
		select {
		case <-sigCh:
			cancelTurn()
			// Ask the core to cancel the running turn.
			_ = child.Client.Call(context.Background(), "session.cancel",
				core.SessionCancelParams{SessionID: sessionID}, nil)
		case <-turnDone:
		}
	}()

	var msgRes core.SessionMessageResult
	err := child.Client.Call(turnCtx, "session.message", core.SessionMessageParams{
		SessionID: sessionID,
		Content:   content,
		Apply:     apply,
		AllowExec: allowExec,
	}, &msgRes)
	close(turnDone)
	cancelTurn()
	signal.Stop(sigCh)
	// Drain any signal that arrived during cleanup.
	select {
	case <-sigCh:
	default:
	}

	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "\n[turn cancelled]")
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return nil
	}

	if len(msgRes.Patches) > 0 && !msgRes.Applied {
		fmt.Fprintf(os.Stderr, "\n%d patch(es) ready — /diff to preview, /apply to apply\n", len(msgRes.Patches))
	} else if msgRes.Applied && msgRes.ApplyResponse != nil {
		fmt.Fprintf(os.Stderr, "\nApplied %d file(s)\n", len(msgRes.ApplyResponse.ChangedFiles))
	}
	return &msgRes
}

// printChatEventBuffered accumulates message_delta chunks and flushes clean text
// on tool_call_start and done events, stripping the trailing {"patches":...} JSON.
func printChatEventBuffered(params json.RawMessage, buf *strings.Builder) {
	var ev struct {
		Type         string `json:"type"`
		Content      string `json:"content"`
		ToolCallName string `json:"tool_call_name"`
	}
	if err := json.Unmarshal(params, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "message_delta":
		buf.WriteString(ev.Content)
	case "tool_call_start":
		// Flush any buffered text before showing the tool call line.
		flushTextBuf(buf)
		fmt.Fprintf(os.Stderr, "\n← %s ", ev.ToolCallName)
	case "done":
		// Flush buffered text, stripping the trailing patches JSON.
		flushTextBuf(buf)
		fmt.Fprintln(os.Stderr)
	}
}

// flushTextBuf prints the buffered content minus any trailing {"patches"...} block.
func flushTextBuf(buf *strings.Builder) {
	text := buf.String()
	buf.Reset()
	if text == "" {
		return
	}
	// Remove trailing patches JSON (the model puts it at the end).
	if idx := strings.LastIndex(text, `{"patches"`); idx != -1 {
		text = strings.TrimRight(text[:idx], " \t\n\r")
	}
	text = strings.TrimRight(text, " \t\n\r")
	if text != "" {
		fmt.Fprintln(os.Stderr, text)
	}
}

// printChatEvent is kept for callers that don't need buffering.
func printChatEvent(params json.RawMessage) {
	var buf strings.Builder
	printChatEventBuffered(params, &buf)
}

// printChatPatches shows each patch's type and path.
func printChatPatches(patchList []patches.Patch) {
	for i, p := range patchList {
		fmt.Fprintf(os.Stderr, "[%d] %s: %s\n", i+1, p.Type, p.Path)
		switch p.Type {
		case patches.TypeFileSearchReplace:
			fmt.Fprintf(os.Stderr, "  - %s\n", firstLine(p.Search))
			fmt.Fprintf(os.Stderr, "  + %s\n", firstLine(p.Replace))
		case patches.TypeFileUnifiedDiff:
			fmt.Fprintln(os.Stderr, p.Diff)
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + "…"
	}
	return s
}
