import type { ToolDiagnosticPayload } from "../protocol/events";

/** Mirrors sessionfile.UIToolBlock for session.ui_sync. */
export interface PersistedToolBlock {
  id?: string;
  name: string;
  args_preview?: string;
  args_raw?: string;
  status: string;
  result?: string;
  diagnostics?: ToolDiagnosticPayload[];
  duration_ms?: number;
}

/** Assistant turn projection persisted into ui_messages (TUI-compatible). */
export interface AssistantTurnProjection {
  text: string;
  reasoning?: string;
  tool_blocks?: PersistedToolBlock[];
  prompt_ctx?: number;
  tokens_in?: number;
  tokens_out?: number;
}

export interface TurnToolTracker {
  id: string;
  name: string;
  argsRaw: string;
  status: "running" | "completed" | "failed" | "skipped";
  result: string;
  diagnostics?: ToolDiagnosticPayload[];
}

/** Best-effort before/after for write/edit tool blocks (history diff restore). */
export function joinAssistantStreamSegments(segments: readonly string[], current: string): string {
  const parts = segments.map((s) => s.trim()).filter(Boolean);
  const cur = current.trim();
  if (cur) {
    parts.push(cur);
  }
  return parts.join("\n\n");
}

export function diffFromToolArgs(
  name: string,
  argsRaw?: string
): { before: string; after: string } | undefined {
  const n = (name || "").toLowerCase();
  const args = parseToolArgsJSON(argsRaw);
  if (n === "write" || n === "fs.write" || n === "file.write_atomic") {
    const content = typeof args.content === "string" ? args.content : "";
    return { before: "", after: content };
  }
  if (n === "edit" || n === "fs.edit") {
    const search = typeof args.search === "string" ? args.search : "";
    const replace = typeof args.replace === "string" ? args.replace : "";
    if (search || replace) {
      return { before: search, after: replace };
    }
  }
  return undefined;
}

function parseToolArgsJSON(raw?: string): Record<string, unknown> {
  const text = (raw || "").trim();
  if (!text) {
    return {};
  }
  try {
    const v = JSON.parse(text) as unknown;
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

export function toolStatusFromResult(content: string): TurnToolTracker["status"] {
  if (content.startsWith("error: ")) {
    return "failed";
  }
  if (content.startsWith("skipped: ")) {
    return "skipped";
  }
  return "completed";
}

export function buildAssistantProjection(input: {
  text: string;
  reasoning: string;
  tools: Map<string, TurnToolTracker>;
  promptCtx: number;
  tokensIn: number;
  tokensOut: number;
}): AssistantTurnProjection | undefined {
  const text = input.text.trim();
  const reasoning = input.reasoning.trim();
  const tool_blocks = [...input.tools.values()]
    .filter((t) => t.status !== "running")
    .map((t) => ({
      id: t.id,
      name: t.name,
      args_raw: t.argsRaw || undefined,
      status: t.status,
      result: t.result || undefined,
      diagnostics: t.diagnostics?.length ? t.diagnostics : undefined,
    }));
  if (!text && !reasoning && tool_blocks.length === 0) {
    return undefined;
  }
  return {
    text,
    reasoning: reasoning || undefined,
    tool_blocks: tool_blocks.length ? tool_blocks : undefined,
    prompt_ctx: input.promptCtx > 0 ? input.promptCtx : undefined,
    tokens_in: input.tokensIn > 0 ? input.tokensIn : undefined,
    tokens_out: input.tokensOut > 0 ? input.tokensOut : undefined,
  };
}

/** Raw ui_messages row from session.get. */
export interface RawUIMessage {
  role?: string;
  text?: string;
  content?: string;
  reasoning?: string;
  tool_blocks?: Array<{
    id?: string;
    name?: string;
    args_preview?: string;
    args_raw?: string;
    status?: string;
    result?: string;
    diagnostics?: ToolDiagnosticPayload[];
  }>;
  segments?: Array<{
    kind?: string;
    text?: string;
    tools?: RawUIMessage["tool_blocks"];
  }>;
  attachments?: Array<{ path?: string; name?: string; kind?: string; ext?: string; mime?: string }>;
  prompt_ctx?: number;
  tokens_in?: number;
  tokens_out?: number;
}

export function reasoningFromUIMessage(m: RawUIMessage): string {
  const direct = (m.reasoning || "").trim();
  if (direct) {
    return direct;
  }
  const parts: string[] = [];
  for (const seg of m.segments || []) {
    if (seg.kind === "reasoning" && seg.text) {
      parts.push(seg.text);
    }
  }
  return parts.join("").trim();
}

export function toolBlocksFromUIMessage(m: RawUIMessage): PersistedToolBlock[] {
  const direct = m.tool_blocks || [];
  if (direct.length > 0) {
    return direct
      .filter((t) => t?.name)
      .map((t) => ({
        id: t.id,
        name: t.name || "tool",
        args_preview: t.args_preview,
        args_raw: t.args_raw,
        status: t.status || "completed",
        result: t.result,
        diagnostics: t.diagnostics,
      }));
  }
  const out: PersistedToolBlock[] = [];
  for (const seg of m.segments || []) {
    if (seg.kind !== "tools" || !Array.isArray(seg.tools)) {
      continue;
    }
    for (const t of seg.tools) {
      if (!t?.name) {
        continue;
      }
      out.push({
        id: t.id,
        name: t.name,
        args_preview: t.args_preview,
        args_raw: t.args_raw,
        status: t.status || "completed",
        result: t.result,
        diagnostics: t.diagnostics,
      });
    }
  }
  return out;
}

/** Fixed system+tools overhead — mirrors agent.promptOverheadBytes / 4 B per token. */
const PROMPT_OVERHEAD_BYTES = 32 * 1024;
const BYTES_PER_CONTEXT_TOKEN = 4;

function utf8ByteLength(s: string): number {
  return new TextEncoder().encode(s).length;
}

function uiMessageContentBytes(m: RawUIMessage): number {
  let base = 0;
  const text = (m.text || m.content || "").trim();
  if (text) {
    base += utf8ByteLength(text);
  }
  const reasoning = reasoningFromUIMessage(m);
  if (reasoning) {
    base += utf8ByteLength(reasoning);
  }
  for (const tb of toolBlocksFromUIMessage(m)) {
    base += utf8ByteLength(tb.args_raw || tb.args_preview || "");
    base += utf8ByteLength(tb.result || "");
  }
  return base;
}

/** Rough prompt token estimate from persisted UI scrollback (LM Studio often omits usage). */
export function estimatePromptTokensFromUI(msgs: RawUIMessage[]): number {
  if (!msgs.length) {
    return 0;
  }
  let base = 0;
  for (const m of msgs) {
    base += uiMessageContentBytes(m);
  }
  const tokens = Math.floor((base + PROMPT_OVERHEAD_BYTES) / BYTES_PER_CONTEXT_TOKEN);
  return tokens > 0 ? tokens : 0;
}

/** Sum assistant completion tokens saved on UI messages (fallback display). */
export function sumCompletionTokensFromUI(msgs: RawUIMessage[]): number {
  let out = 0;
  for (const m of msgs) {
    if (String(m.role || "").toLowerCase() !== "assistant") {
      continue;
    }
    if (typeof m.tokens_out === "number" && m.tokens_out > 0) {
      out += m.tokens_out;
    } else {
      const text = (m.text || m.content || "").trim();
      if (text) {
        out += Math.ceil(utf8ByteLength(text) / BYTES_PER_CONTEXT_TOKEN);
      }
    }
  }
  return out;
}

export function effectiveContextLimit(numCtx: number, contextTokens: number, fallback = 128000): number {
  if (contextTokens > 0) {
    return contextTokens;
  }
  if (numCtx > 0) {
    return numCtx;
  }
  return fallback;
}
