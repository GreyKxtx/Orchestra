/**
 * Hide agent final-action JSON (e.g. {"patches":[]}) from chat UI text.
 * Port of ui/tui/view/chat_helpers.go stripFinalEnvelope.
 */

function matchJSONObject(s: string, start: number): number {
  let depth = 0;
  let inStr = false;
  let esc = false;
  for (let j = start; j < s.length; j++) {
    const c = s[j];
    if (esc) {
      esc = false;
      continue;
    }
    if (c === "\\") {
      esc = true;
      continue;
    }
    if (c === '"') {
      inStr = !inStr;
      continue;
    }
    if (inStr) {
      continue;
    }
    if (c === "{") {
      depth++;
    } else if (c === "}") {
      depth--;
      if (depth === 0) {
        return j;
      }
    }
  }
  return -1;
}

/** Removes balanced (or trailing partial) final JSON envelopes from assistant text. */
export function stripFinalEnvelope(text: string): string {
  let out = text;
  for (;;) {
    const i = out.indexOf("{");
    if (i < 0) {
      return out;
    }
    const end = matchJSONObject(out, i);
    if (end < 0) {
      const tail = out.slice(i);
      if (
        tail.includes('"patches"') ||
        (tail.includes('"type"') && tail.includes('"final"'))
      ) {
        return out.slice(0, i).trimEnd();
      }
      return out;
    }
    const blob = out.slice(i, end + 1);
    if (blob.includes('"patches"')) {
      out = (out.slice(0, i) + out.slice(end + 1)).trim();
      continue;
    }
    return out.slice(0, end + 1) + stripFinalEnvelope(out.slice(end + 1));
  }
}

function digitLikeRatio(s: string): number {
  if (!s.length) {
    return 0;
  }
  let n = 0;
  for (const c of s) {
    if (/[\d.eE+\-]/.test(c)) {
      n++;
    }
  }
  return n / s.length;
}

/** Drop individual stream chunks that look like embedding vectors or zero padding. */
export function shouldSuppressStreamChunk(chunk: string): boolean {
  if (!chunk) {
    return false;
  }
  if (/^0{16,}/.test(chunk)) {
    return true;
  }
  if (chunk.length >= 6 && digitLikeRatio(chunk) >= 0.92) {
    return true;
  }
  return false;
}

/** Errors from intentional turn cancel (Stop / session.cancel) — hide in chat UI. */
export function isBenignTurnError(message: string): boolean {
  const m = message.trim();
  if (!m) {
    return false;
  }
  if (/context canceled|context cancelled|request cancelled|request canceled/i.test(m)) {
    return true;
  }
  if (/SSE read error/i.test(m) && /cancel/i.test(m)) {
    return true;
  }
  return false;
}

/**
 * Context-compaction housekeeping events arrive on the error channel
 * (recoverable_error) but are not failures — render them as a neutral gray
 * system note instead of a red error. Returns null for real errors.
 */
export function compactionNoticeText(message: string): string | null {
  const m = message.trim();
  if (!m) {
    return null;
  }
  if (m === "CONTEXT_PRESSURE") {
    return "Контекст почти заполнен — история чата будет суммаризирована";
  }
  if (m === "CONTEXT_COMPACTED") {
    return "Суммаризация чата: история сжата, работа продолжается";
  }
  if (/контекст переполнен/i.test(m)) {
    return `Суммаризация чата — ${m}`;
  }
  return null;
}

/** True when accumulated assistant text is mostly non-human garbage. */
export function looksLikeCorruptedStream(text: string): boolean {
  const t = text.trim();
  if (t.length < 40) {
    return false;
  }
  if (/0{48,}/.test(t)) {
    return true;
  }
  if (t.length >= 120 && digitLikeRatio(t) > 0.75) {
    return true;
  }
  if (/Serving user request/i.test(t) && digitLikeRatio(t.slice(30)) > 0.6) {
    return true;
  }
  return false;
}

/** Strip embedding-like tails and broken JSON string prefixes from streamed assistant text. */
export function sanitizeAssistantStream(text: string): string {
  let t = text.trim();
  if (!t) {
    return "";
  }
  // Trim broken opening quote from partial JSON strings streamed as content.
  if (t.startsWith('"') && !t.endsWith('"') && t.length < 400) {
    t = t.replace(/^"+/, "").trim();
  }
  const numericRun = t.match(/([\d.eE+\-]{80,}|0{32,})/);
  if (numericRun && numericRun.index !== undefined && numericRun.index > 0) {
    t = t.slice(0, numericRun.index).trimEnd().replace(/^"+|"+$/g, "").trim();
  }
  if (looksLikeCorruptedStream(t)) {
    const prefix = t.split(/[\d.eE]{20,}/)[0]?.trim().replace(/^"+|"+$/g, "").trim() || "";
    if (prefix.length > 0 && prefix.length < 240 && !looksLikeCorruptedStream(prefix)) {
      return prefix;
    }
    return "";
  }
  return t;
}
