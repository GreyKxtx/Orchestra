package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/externalpatch"
	"github.com/orchestra/orchestra/internal/ops"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"

	"github.com/orchestra/orchestra/internal/llm"
)

type Options struct {
	MaxSteps int
	// MaxInvalidRetries is the number of extra attempts after an invalid JSON/schema output.
	MaxInvalidRetries int
	// MaxPromptBytes limits the total prompt size passed to the LLM.
	MaxPromptBytes int

	// Apply controls whether to write changes to disk.
	// If false, the agent will run fs.apply_ops in dry-run mode and return diffs.
	Apply  bool
	Backup bool

	// AllowExec controls whether exec.run is allowed without external consent.
	AllowExec bool

	// MaxDeniedToolRepeats is a hard stop to prevent infinite TOOL_DENIED loops
	// (e.g. model repeatedly calling exec.run when it's not allowed).
	MaxDeniedToolRepeats int
	// MaxToolErrorRepeats is a hard stop for repeated TOOL_ERR loops.
	MaxToolErrorRepeats int
	// MaxFinalFailures is a hard stop for repeated resolve/apply failures after "final".
	MaxFinalFailures int
	// LLMStepTimeout bounds time spent waiting for the model per attempt.
	LLMStepTimeout time.Duration

	Debug  bool
	Logger *log.Logger
}

type Result struct {
	Steps int

	Patches []externalpatch.Patch
	Ops     []ops.AnyOp

	Applied bool
	// ApplyResponse is returned from fs.apply_ops (dry-run or write).
	ApplyResponse *tools.FSApplyOpsResponse
}

type Agent struct {
	llm       llm.Client
	validator *schema.Validator
	tools     *tools.Runner
	opts      Options
}

func New(llmClient llm.Client, v *schema.Validator, toolRunner *tools.Runner, opts Options) (*Agent, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("llm client is nil")
	}
	if v == nil {
		return nil, fmt.Errorf("validator is nil")
	}
	if toolRunner == nil {
		return nil, fmt.Errorf("tools runner is nil")
	}
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 24
	}
	if opts.MaxInvalidRetries <= 0 {
		opts.MaxInvalidRetries = 3
	}
	if opts.MaxPromptBytes <= 0 {
		opts.MaxPromptBytes = 64 * 1024
	}
	if opts.MaxDeniedToolRepeats <= 0 {
		opts.MaxDeniedToolRepeats = 2
	}
	if opts.MaxToolErrorRepeats <= 0 {
		opts.MaxToolErrorRepeats = 6
	}
	if opts.MaxFinalFailures <= 0 {
		opts.MaxFinalFailures = 6
	}
	if opts.LLMStepTimeout <= 0 {
		opts.LLMStepTimeout = 25 * time.Second
	}
	return &Agent{
		llm:       llmClient,
		validator: v,
		tools:     toolRunner,
		opts:      opts,
	}, nil
}

func (a *Agent) Run(ctx context.Context, userQuery string) (*Result, error) {
	userQuery = strings.TrimSpace(userQuery)
	if userQuery == "" {
		return nil, fmt.Errorf("user query is empty")
	}

	// History as structured messages (system + user + assistant tool calls + tool results)
	history := make([]llm.Message, 0, 32)
	steps := 0
	deniedCounts := make(map[string]int, 4)
	consecutiveToolErrors := 0
	finalFailures := 0

	for steps < a.opts.MaxSteps {
		steps++

		step, raw, resp, err := a.nextStep(ctx, userQuery, history)
		if err != nil {
			return nil, err
		}
		if step == nil {
			return nil, fmt.Errorf("nextStep returned nil step without error")
		}
		// resp can be nil only if there was an error, which should have been returned above
		// But we handle it gracefully for tool calls that need tool_call_id

		switch step.Type {
		case StepToolCall:
			if step.Tool == nil {
				// Add validation error as user message for retry
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: tool is required", raw),
				})
				continue
			}
			name := strings.TrimSpace(step.Tool.Name)
			if name == "" {
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: tool.name is empty", raw),
				})
				continue
			}

			// Extract tool_call_id from response
			toolCallID := ""
			hasToolCalls := resp != nil && len(resp.Message.ToolCalls) > 0
			if hasToolCalls {
				toolCallID = resp.Message.ToolCalls[0].ID
			}
			if toolCallID == "" {
				// Fallback: generate a synthetic ID if not provided (for legacy JSON format)
				toolCallID = fmt.Sprintf("call_%d_%d", steps, time.Now().UnixNano())
			}

			// Add assistant message with tool_calls (required for proper tool calling loop)
			// Only add if we have actual tool_calls (not legacy JSON format in content)
			if hasToolCalls {
				// When tool_calls are present, content should be empty (some providers don't like content + tool_calls)
				assistantMsg := llm.Message{
					Role:      llm.RoleAssistant,
					Content:   "", // Clear content when tool_calls are present
					ToolCalls: resp.Message.ToolCalls,
				}
				history = append(history, assistantMsg)
				a.logf("agent.tool_call added assistant message to history, history_len=%d, tool_call_id=%s", len(history), toolCallID)
			} else {
				a.logf("agent.tool_call WARNING: no tool_calls in response, history_len=%d", len(history))
			}

			// Consent policy: block exec.run unless explicitly allowed.
			if name == "exec.run" && !a.opts.AllowExec {
				deniedCounts[name]++
				// Add tool message with denial
				toolResult := formatToolDeniedJSON(name, step.Tool.Input, "exec.run requires user consent")
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    toolResult,
				})
				if deniedCounts[name] > a.opts.MaxDeniedToolRepeats {
					return nil, protocol.NewError(protocol.InvalidLLMOutput, "model repeatedly requested denied tool", map[string]any{
						"tool":        name,
						"count":       deniedCounts[name],
						"max_repeats": a.opts.MaxDeniedToolRepeats,
					})
				}
				continue
			}

			start := time.Now()
			out, err := a.tools.Call(ctx, name, step.Tool.Input)
			dur := time.Since(start).Milliseconds()
			if err != nil {
				a.logf("tool_call name=%s status=error duration_ms=%d err=%v", name, dur, err)
				// Add tool message with error
				toolResult := formatToolErrorJSON(name, step.Tool.Input, err)
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    toolResult,
				})
				consecutiveToolErrors++
				if consecutiveToolErrors >= a.opts.MaxToolErrorRepeats {
					return nil, protocol.NewError(protocol.InvalidLLMOutput, "model repeatedly produced failing tool calls", map[string]any{
						"count":       consecutiveToolErrors,
						"max_repeats": a.opts.MaxToolErrorRepeats,
						"last_tool":   name,
					})
				}
				continue
			}
			a.logf("tool_call name=%s status=ok duration_ms=%d output_bytes=%d", name, dur, len(out))
			// Add tool message with success result
			history = append(history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    string(out),
			})
			a.logf("agent.tool_call added tool message to history, history_len=%d, tool_call_id=%s", len(history), toolCallID)
			consecutiveToolErrors = 0
			finalFailures = 0 // Reset final failures on successful tool call (model is making progress)
			continue

		case StepFinal:
			if step.Final == nil {
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: final is required", raw),
				})
				continue
			}
			// Empty patches is valid (no changes needed)
			if len(step.Final.Patches) == 0 {
				a.logf("final received empty patches (no changes needed)")
				// Return empty result - this is success, not an error
				return &Result{
					Patches: []externalpatch.Patch{},
					Applied: false,
				}, nil
			}

			patches := append([]externalpatch.Patch(nil), step.Final.Patches...)
			a.logf("final received patches=%d", len(patches))

			start := time.Now()
			internalOps, err := resolver.ResolveExternalPatches(a.tools.WorkspaceRoot(), patches)
			resolveMS := time.Since(start).Milliseconds()
			if err != nil {
				a.logf("resolve status=error duration_ms=%d err=%v", resolveMS, err)
				// Add user message with resolve error for retry
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatResolveErrorCompact(err),
				})
				finalFailures++
				if finalFailures >= a.opts.MaxFinalFailures {
					return nil, protocol.NewError(protocol.InvalidLLMOutput, "failed to resolve/apply patches repeatedly", map[string]any{
						"count":        finalFailures,
						"max_failures": a.opts.MaxFinalFailures,
						"last_error":   formatErr(err),
					})
				}
				continue
			}
			a.logf("resolve status=ok duration_ms=%d ops=%d", resolveMS, len(internalOps))

			applyReq := tools.FSApplyOpsRequest{
				Ops:    internalOps,
				DryRun: !a.opts.Apply,
				Backup: a.opts.Backup && a.opts.Apply,
			}

			start = time.Now()
			resp, err := a.tools.FSApplyOps(ctx, applyReq)
			applyMS := time.Since(start).Milliseconds()
			if err != nil {
				// StaleContent/AmbiguousMatch are recoverable: keep looping.
				if pe, ok := protocol.AsError(err); ok && (pe.Code == protocol.StaleContent || pe.Code == protocol.AmbiguousMatch) {
					a.logf("apply status=recoverable_error duration_ms=%d err=%v", applyMS, err)
					// Add user message with apply error for retry
					history = append(history, llm.Message{
						Role:    llm.RoleUser,
						Content: formatApplyErrorCompact(err, pe.Code),
					})
					finalFailures++
					if finalFailures >= a.opts.MaxFinalFailures {
						return nil, protocol.NewError(protocol.InvalidLLMOutput, "failed to resolve/apply patches repeatedly", map[string]any{
							"count":        finalFailures,
							"max_failures": a.opts.MaxFinalFailures,
							"last_error":   formatErr(err),
						})
					}
					continue
				}
				a.logf("apply status=error duration_ms=%d err=%v", applyMS, err)
				return nil, err
			}
			a.logf("apply status=ok duration_ms=%d diffs=%d dry_run=%v", applyMS, len(resp.Diffs), applyReq.DryRun)

			return &Result{
				Steps:         steps,
				Patches:       patches,
				Ops:           internalOps,
				Applied:       a.opts.Apply,
				ApplyResponse: resp,
			}, nil

		default:
			history = append(history, llm.Message{
				Role:    llm.RoleUser,
				Content: formatValidatorError("Invalid JSON format: unknown step type", raw),
			})
		}
	}

	return nil, protocol.NewError(protocol.InvalidLLMOutput, "max_steps exceeded", map[string]any{
		"max_steps": a.opts.MaxSteps,
	})
}

// nextStep returns the next step, raw response, full LLM response, and error.
// It builds messages from history and adds system/user prompts.
func (a *Agent) nextStep(ctx context.Context, userQuery string, history []llm.Message) (*Step, string, *llm.CompleteResponse, error) {
	toolDefs := tools.ListTools(a.opts.AllowExec)
	systemPrompt := promptpkg.BuildSystemPrompt()
	snap := promptpkg.BuildUserInfoSnapshot(a.tools.WorkspaceRoot())
	userPrompt := promptpkg.BuildUserPrompt(userQuery, snap, tools.ToolNames(toolDefs))

	// Build messages: system + user (initial) + history (assistant tool calls + tool results)
	messages := make([]llm.Message, 0, len(history)+2)
	messages = append(messages, llm.Message{
		Role:    llm.RoleSystem,
		Content: systemPrompt,
	})
	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: userPrompt,
	})
	messages = append(messages, history...)

	// Debug: log history length before truncation
	if a.opts.Debug {
		a.logf("agent.nextStep history_len=%d messages_before_truncate=%d", len(history), len(messages))
	}

	// Truncate messages if needed to stay within budget (best-effort)
	if a.opts.MaxPromptBytes > 0 {
		beforeTruncate := len(messages)
		messages = truncateMessages(messages, a.opts.MaxPromptBytes)
		if a.opts.Debug && len(messages) != beforeTruncate {
			a.logf("agent.nextStep messages truncated: %d -> %d (budget=%d)", beforeTruncate, len(messages), a.opts.MaxPromptBytes)
		}
	}

	if a.opts.Debug {
		totalBytes := 0
		for _, m := range messages {
			totalBytes += len(m.Content)
		}
		// Build roles string for debug logging
		roles := make([]string, 0, len(messages))
		for _, m := range messages {
			roleStr := string(m.Role)
			if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
				roleStr = fmt.Sprintf("%s(tool_calls=%d)", roleStr, len(m.ToolCalls))
			}
			if m.Role == llm.RoleTool && m.ToolCallID != "" {
				roleStr = fmt.Sprintf("%s(id=%s)", roleStr, truncateID(m.ToolCallID, 12))
			}
			roles = append(roles, roleStr)
		}
		a.logf("agent.step messages_count=%d roles=%v total_bytes=%d tools=%d", len(messages), roles, totalBytes, len(toolDefs))
	}

	var lastInvalid *protocol.Error
	var lastRaw string

	for attempt := 0; attempt <= a.opts.MaxInvalidRetries; attempt++ {
		stepCtx := ctx
		var cancel context.CancelFunc
		if a.opts.LLMStepTimeout > 0 {
			stepCtx, cancel = context.WithTimeout(ctx, a.opts.LLMStepTimeout)
		}
		resp, err := a.llm.Complete(stepCtx, llm.CompleteRequest{
			Messages: messages,
			Tools:    toolDefs,
		})
		if cancel != nil {
			cancel() // Always cancel timeout context to free resources
		}
		if err != nil {
			return nil, "", nil, err
		}
		step, raw, nerr := NormalizeLLM(a.validator, resp)
		lastRaw = raw

		if nerr != nil {
			// Inject validation error as user message and retry
			if pe, ok := protocol.AsError(nerr); ok {
				lastInvalid = pe
			} else {
				lastInvalid = protocol.NewError(protocol.InvalidLLMOutput, nerr.Error(), nil)
			}
			// Add error feedback to messages for retry
			errorMsg := llm.Message{
				Role:    llm.RoleUser,
				Content: formatValidatorErrorCompact(lastInvalid.Message),
			}
			messages = append(messages, errorMsg)
			// Truncate again if needed
			if a.opts.MaxPromptBytes > 0 {
				messages = truncateMessages(messages, a.opts.MaxPromptBytes)
			}
			continue
		}

		// Note: exec.run policy validation is handled in Run() after adding assistant message to history.
		// This allows proper tool calling loop with tool messages.

		return step, raw, resp, nil
	}

	if lastInvalid != nil {
		return nil, lastRaw, nil, lastInvalid
	}
	return nil, lastRaw, nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: unknown validation failure", nil)
}

func (a *Agent) logf(format string, args ...any) {
	if !a.opts.Debug {
		return
	}
	if a.opts.Logger != nil {
		a.opts.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func buildBasePrompt(userQuery string, allowExec bool) string {
	// Keep this prompt compact but explicit: JSON-only, step schema, allowed tools.
	toolsList := strings.TrimSpace(`
Доступные инструменты:
- fs.list
- fs.read
- search.text
- code.symbols
`)
	if allowExec {
		toolsList += "\n- exec.run"
	} else {
		toolsList += "\nВАЖНО: exec.run сейчас НЕДОСТУПЕН (не предлагай и не вызывай его)."
	}

	return strings.TrimSpace(fmt.Sprintf(`
Ты — агент для работы с кодовой базой в workspace. Твоя цель: выполнить задачу пользователя.

ВАЖНО:
- Ты ДОЛЖЕН отвечать ТОЛЬКО валидным JSON (без markdown, без пояснений).
- Каждый ответ — это ОДИН следующий шаг.

Формат шага (AgentStep):
1) Tool call:
{"type":"tool_call","tool":{"name":"fs.list","input":{...}}}

2) Final:
{"type":"final","final":{"patches":[ ... ]}}

%s

Патчи (final.patches) поддерживают только:
- {"type":"file.search_replace","path":"...","search":"...","replace":"...","file_hash":"sha256:..."}
- {"type":"file.unified_diff","path":"...","diff":"...","file_hash":"sha256:..."}

Правила:
- Для каждого файла, который ты меняешь, сначала сделай fs.read, чтобы получить точный file_hash.
- Не генерируй внутренние ops и не пытайся применять изменения инструментами. Верни изменения ТОЛЬКО через final.patches.

Задача пользователя:
%s
`, toolsList, userQuery))
}

func buildPromptWithHistory(base string, history []string, maxBytes int) string {
	if maxBytes <= 0 {
		return base + "\n"
	}
	header := base + "\n\nИстория (самые свежие события в конце):\n"
	footer := "\n\nВерни ТОЛЬКО JSON следующего шага.\n"

	// Keep the tail of history within the byte budget.
	budget := maxBytes - len(header) - len(footer)
	if budget <= 0 {
		// Worst case: return only base prompt.
		return base
	}

	var selected []string
	size := 0
	for i := len(history) - 1; i >= 0; i-- {
		item := history[i]
		need := len(item) + 2
		if size+need > budget {
			break
		}
		selected = append(selected, item)
		size += need
	}
	// Reverse back to chronological order.
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}

	return header + strings.Join(selected, "\n\n") + footer
}

func formatToolOK(name string, input json.RawMessage, output json.RawMessage) string {
	return "TOOL_OK " + name + "\ninput=" + compactJSON(input) + "\noutput=" + compactJSON(output)
}

func formatToolError(name string, input json.RawMessage, err error) string {
	code := ""
	if pe, ok := protocol.AsError(err); ok {
		code = string(pe.Code)
	}
	if code != "" {
		return "TOOL_ERR " + name + " code=" + code + "\ninput=" + compactJSON(input) + "\nerror=" + formatErr(err)
	}
	return "TOOL_ERR " + name + "\ninput=" + compactJSON(input) + "\nerror=" + formatErr(err)
}

func formatToolDenied(name string, input json.RawMessage, reason string) string {
	return "TOOL_DENIED " + name + "\ninput=" + compactJSON(input) + "\nreason=" + reason
}

func formatValidatorError(msg string, raw string) string {
	return "VALIDATION_ERROR\nmessage=" + msg + "\nraw=" + truncate(strings.TrimSpace(raw), 400)
}

// formatValidatorErrorCompact returns a compact error message without raw JSON to avoid prompt bloat.
func formatValidatorErrorCompact(msg string) string {
	return "VALIDATION_ERROR\nmessage=" + msg + "\nИсправь формат JSON согласно схеме (tool call или PatchSet)."
}

// formatPolicyDeniedCompact returns a compact policy denial message.
func formatPolicyDeniedCompact(toolName string) string {
	return fmt.Sprintf("TOOL_DENIED %s\nreason=требует явного разрешения\nИспользуй только доступные инструменты из списка.", toolName)
}

func formatResolveError(err error) string {
	return "RESOLVE_ERROR\nerror=" + formatErr(err)
}

// formatResolveErrorCompact returns a compact resolve error message.
func formatResolveErrorCompact(err error) string {
	if pe, ok := protocol.AsError(err); ok {
		return fmt.Sprintf("RESOLVE_ERROR code=%s\nПеречитай файл (fs.read) и обнови file_hash в патче.", pe.Code)
	}
	return "RESOLVE_ERROR code=unknown\nerror=" + err.Error() + "\nПеречитай файл (fs.read) и обнови file_hash в патче."
}

func formatApplyError(err error) string {
	return "APPLY_ERROR\nerror=" + formatErr(err)
}

// formatApplyErrorCompact returns a compact apply error message with actionable hint.
func formatApplyErrorCompact(err error, code protocol.ErrorCode) string {
	if code == protocol.StaleContent {
		return "APPLY_ERROR code=StaleContent\nФайл изменился. Перечитай файл (fs.read) и обнови патч с новым file_hash."
	}
	if code == protocol.AmbiguousMatch {
		return "APPLY_ERROR code=AmbiguousMatch\nПоиск неоднозначен. Уточни search-блок в патче (добавь больше контекста)."
	}
	return "APPLY_ERROR code=unknown\nerror=" + formatErr(err)
}

func formatErr(err error) string {
	if err == nil {
		return ""
	}
	if pe, ok := protocol.AsError(err); ok {
		b, _ := json.Marshal(pe)
		return string(b)
	}
	return err.Error()
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return truncate(string(raw), 400)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return truncate(string(raw), 400)
	}
	return string(b)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + " ...(truncated)"
}

// truncateID truncates an ID string for logging
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen] + "..."
}

// truncateMessages truncates message history to fit within byte budget.
// Always keeps system and first user message, then keeps as many history messages as possible.
// Optimized to preserve assistant+tool pairs and account for tool_calls/ToolCallID overhead.
func truncateMessages(messages []llm.Message, maxBytes int) []llm.Message {
	if maxBytes <= 0 || len(messages) <= 2 {
		return messages
	}

	// Always keep system (0) and first user (1)
	required := messages[:2]
	requiredSize := 0
	for _, m := range required {
		requiredSize += estimateMessageSize(m)
	}

	if requiredSize >= maxBytes {
		// Worst case: return only required messages
		return required
	}

	budget := maxBytes - requiredSize
	var selected []llm.Message
	size := 0
	used := make(map[int]bool, len(messages)) // Track which messages we've already added

	// Keep history messages from the end (most recent first)
	// Try to preserve assistant+tool pairs together
	// Note: In history, order is: assistant (with tool_calls) -> tool (with result)
	for i := len(messages) - 1; i >= 2; i-- {
		if used[i] {
			continue // Already added as part of a pair
		}

		msg := messages[i]
		msgSize := estimateMessageSize(msg)

		// If this is a tool message, try to include its corresponding assistant message
		// In history, assistant comes BEFORE tool, so we check i-1
		if msg.Role == llm.RoleTool && i > 2 && !used[i-1] {
			// Check if previous message is assistant with matching tool_call
			prevMsg := messages[i-1]
			if prevMsg.Role == llm.RoleAssistant && len(prevMsg.ToolCalls) > 0 {
				// Check if tool_call_id matches
				if msg.ToolCallID != "" {
					for _, tc := range prevMsg.ToolCalls {
						if tc.ID == msg.ToolCallID {
							// Include both as a pair
							// We're iterating backwards, so we add: tool, then assistant
							// After reverse: assistant, then tool (correct order)
							pairSize := estimateMessageSize(prevMsg) + msgSize
							if size+pairSize <= budget {
								selected = append(selected, msg)     // tool first (will be last after reverse)
								selected = append(selected, prevMsg) // assistant second (will be first after reverse)
								size += pairSize
								used[i] = true
								used[i-1] = true
								i-- // Skip assistant message in next iteration
								continue
							}
						}
					}
				}
			}
		}

		// Single message (or pair didn't fit)
		if size+msgSize > budget {
			break
		}
		selected = append(selected, msg)
		size += msgSize
		used[i] = true
	}

	// Reverse to restore chronological order
	// After reverse: assistant messages come before their corresponding tool messages
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}

	result := make([]llm.Message, 0, len(required)+len(selected))
	result = append(result, required...)
	result = append(result, selected...)
	return result
}

// estimateMessageSize estimates the byte size of a message for truncation purposes.
// Accounts for Content, ToolCalls (JSON serialization overhead), and ToolCallID.
func estimateMessageSize(msg llm.Message) int {
	size := len(msg.Content)
	if msg.ToolCallID != "" {
		// ToolCallID adds to JSON size (field name + value)
		size += len(msg.ToolCallID) + 20 // approximate overhead for "tool_call_id":"..."
	}
	if len(msg.ToolCalls) > 0 {
		// Estimate tool_calls size: each tool call has id, type, function.name, function.arguments
		for _, tc := range msg.ToolCalls {
			size += len(tc.ID) + len(tc.Type) + len(tc.Function.Name)
			// Arguments are already in Content or as separate field, but add overhead
			size += len(tc.Function.Arguments.Raw()) + 50 // JSON structure overhead
		}
	}
	return size
}

// formatToolDeniedJSON formats a tool denial as JSON for tool message content.
func formatToolDeniedJSON(name string, input json.RawMessage, reason string) string {
	result := map[string]any{
		"status": "denied",
		"tool":   name,
		"reason": reason,
		"input":  compactJSON(input),
	}
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"status":"denied","tool":"%s","reason":"%s"}`, name, reason)
	}
	return string(b)
}

// formatToolErrorJSON formats a tool error as JSON for tool message content.
func formatToolErrorJSON(name string, input json.RawMessage, err error) string {
	result := map[string]any{
		"status": "error",
		"tool":   name,
		"input":  compactJSON(input),
	}
	if pe, ok := protocol.AsError(err); ok {
		result["code"] = string(pe.Code)
		result["error"] = pe.Message
	} else {
		result["error"] = err.Error()
	}
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"status":"error","tool":"%s","error":"%s"}`, name, err.Error())
	}
	return string(b)
}
