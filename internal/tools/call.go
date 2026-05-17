package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Call executes a tool by name with a JSON input object, returning JSON output.
func (r *Runner) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tool name is empty")
	}

	// Route mcp:* calls to the registered MCP manager (use original name).
	if r.mcpCaller != nil && strings.HasPrefix(name, "mcp:") {
		return r.mcpCaller.Call(ctx, name, input)
	}

	switch name {
	case "ls":
		var req FSListRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSList(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "read":
		var req FSReadRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSRead(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "glob":
		var req FSGlobRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSGlob(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "write":
		var req FSWriteRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSWrite(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "edit":
		var req FSEditRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSEdit(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "fs.apply_ops":
		// Normalize common LLM mistake: "type" → "op" in ops
		normalizedInput := normalizeOpsJSON(input)
		var req FSApplyOpsRequest
		if err := decodeToolInput(normalizedInput, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSApplyOps(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "grep":
		var req SearchTextRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.SearchText(ctx, req)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(formatSearchResults(req.Query, resp)), nil

	case "symbols":
		var req CodeSymbolsRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.CodeSymbols(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "explore":
		var req ExploreCodebaseRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.ExploreCodebase(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "diff.preview":
		var req FSPreviewRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSPreview(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "bash":
		// run_in_background is parsed from raw input to keep the request struct
		// focused on the foreground contract.
		var probe struct {
			RunInBackground bool `json:"run_in_background"`
		}
		_ = decodeToolInput(input, &probe)
		var req ExecRunRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		if probe.RunInBackground {
			resp, err := r.ExecBashBackground(ctx, req)
			if err != nil {
				return nil, err
			}
			return mustJSON(resp)
		}
		resp, err := r.ExecRun(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "bash.output":
		var req ExecBashOutputRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.ExecBashOutput(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "bash.kill":
		var req ExecBashKillRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.ExecBashKill(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "webfetch":
		var req WebFetchRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.WebFetch(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "websearch":
		var req WebSearchRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.WebSearch(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "memory_write":
		var req MemoryWriteRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.MemoryWrite(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "runtime_query":
		var req RuntimeQueryRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.RuntimeQuery(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "lsp.definition":
		var req LSPDefinitionRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.LSPDefinition(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "lsp.references":
		var req LSPReferencesRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.LSPReferences(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "lsp.hover":
		var req LSPHoverRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.LSPHover(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "lsp.diagnostics":
		var req LSPDiagnosticsRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.LSPDiagnostics(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "lsp.rename":
		var req LSPRenameRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.LSPRename(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "fs.delete":
		var req FSDeleteRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSDelete(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "fs.rename":
		var req FSRenameRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.FSRename(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "git.status":
		var req GitStatusRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GitStatus(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "git.log":
		var req GitLogRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GitLog(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "git.diff":
		var req GitDiffRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GitDiff(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "git.commit":
		var req GitCommitRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GitCommit(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "git.branch":
		var req GitBranchRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GitBranch(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "git.checkout":
		var req GitCheckoutRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GitCheckout(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "git.push":
		var req GitPushRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GitPush(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.navigate":
		var req BrowserNavigateRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserNavigate(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.snapshot":
		var req BrowserSnapshotRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserSnapshot(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.screenshot":
		var req BrowserScreenshotRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserScreenshot(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.click":
		var req BrowserClickRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserClick(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.type":
		var req BrowserTypeRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserType(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.fill":
		var req BrowserFillRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserFill(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.select":
		var req BrowserSelectRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserSelect(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.eval":
		var req BrowserEvalRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserEval(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.wait":
		var req BrowserWaitRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserWait(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "browser.close":
		var req BrowserCloseRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.BrowserClose(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.pr.list":
		var req GHPRListRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHPRList(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.pr.create":
		var req GHPRCreateRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHPRCreate(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.pr.view":
		var req GHPRViewRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHPRView(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.issue.list":
		var req GHIssueListRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHIssueList(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	case "gh.issue.view":
		var req GHIssueViewRequest
		if err := decodeToolInput(input, &req); err != nil {
			return nil, err
		}
		resp, err := r.GHIssueView(ctx, req)
		if err != nil {
			return nil, err
		}
		return mustJSON(resp)

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// normalizeOpsJSON normalizes common LLM mistakes in ops JSON:
// - "type" field → "op" field (if "op" is missing)
func normalizeOpsJSON(input json.RawMessage) json.RawMessage {
	if len(input) == 0 {
		return input
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(input, &raw); err != nil {
		return input // Return original if parsing fails
	}

	// Check if this is fs.apply_ops request with ops array
	if opsRaw, hasOps := raw["ops"]; hasOps {
		var opsArray []json.RawMessage
		if err := json.Unmarshal(opsRaw, &opsArray); err == nil {
			// Normalize each op: "type" → "op"
			normalized := make([]json.RawMessage, 0, len(opsArray))
			for _, opRaw := range opsArray {
				var opMap map[string]json.RawMessage
				if err := json.Unmarshal(opRaw, &opMap); err == nil {
					// If "op" is missing but "type" exists, use "type" as "op"
					if _, hasOp := opMap["op"]; !hasOp {
						if typeVal, hasType := opMap["type"]; hasType {
							opMap["op"] = typeVal
							// Remove "type" to avoid confusion
							delete(opMap, "type")
						}
					}
					// Re-encode normalized op
					if normalizedOp, err := json.Marshal(opMap); err == nil {
						normalized = append(normalized, normalizedOp)
					} else {
						normalized = append(normalized, opRaw) // Fallback to original
					}
				} else {
					normalized = append(normalized, opRaw) // Fallback to original
				}
			}
			// Re-encode ops array
			if normalizedOps, err := json.Marshal(normalized); err == nil {
				raw["ops"] = normalizedOps
				// Re-encode entire request
				if normalizedInput, err := json.Marshal(raw); err == nil {
					return normalizedInput
				}
			}
		}
	}

	return input // Return original if normalization not needed or failed
}

func decodeToolInput(input json.RawMessage, out any) error {
	if len(input) == 0 {
		// Treat missing input as empty object.
		input = []byte(`{}`)
	}
	dec := json.NewDecoder(strings.NewReader(string(input)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON")
		}
		return err
	}
	return nil
}

func mustJSON(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
