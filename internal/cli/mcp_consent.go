package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/mcp"
	"github.com/orchestra/orchestra/internal/permission"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

// terminalConsent is the permission.Requester for `orchestra apply` on a
// terminal: the prompt goes to stderr (stdout may be the patch), the answer
// comes from stdin. y approves once, a approves and stops asking for this
// server, anything else — including just Enter or EOF — refuses.
type terminalConsent struct {
	in  io.Reader
	out io.Writer
}

func (c *terminalConsent) RequestPermission(_ context.Context, req permission.Request) (permission.Response, error) {
	server := strings.TrimPrefix(req.Tool, "mcp:")
	fmt.Fprintf(c.out, "\n[MCP] сервер %s %s\n  %s\nРазрешить? [y/n/a] (a = всегда для этого сервера): ",
		server, req.Reason, req.Description)
	line, err := bufio.NewReader(c.in).ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(c.out)
		return permission.Response{}, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "д", "да":
		return permission.Response{Approved: true}, nil
	case "a", "always":
		return permission.Response{Approved: true, Always: true}, nil
	}
	return permission.Response{}, nil
}

// applyMCPHooks wires an apply run's MCP servers to the terminal. Without a
// TTY there is nobody to ask, so consent and questions are nil and the
// core's hooks fail closed: sampling refused, elicitation declined. That is
// the intended non-interactive behaviour, not a gap — a config flag alone is
// permission to ask, never permission to proceed.
func applyMCPHooks(client llm.Client, model string) mcp.Hooks {
	var consent permission.Requester
	var ask tools.QuestionAsker
	if isTTY() {
		consent = &terminalConsent{in: os.Stdin, out: os.Stderr}
		ask = &tools.StdinQuestionAsker{}
	}
	return core.NewMCPHooks(func() (llm.Client, string) { return client, model }, consent, ask)
}
