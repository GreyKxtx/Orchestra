/**
 * LSP-style Content-Length framing for Orchestra JSON-RPC over stdio.
 * Wire format matches internal/jsonrpc codec.
 */

/** Hard cap: a corrupt/hostile Content-Length must not balloon memory. */
const MAX_CONTENT_LENGTH = 64 * 1024 * 1024;

const HEADER_END = Buffer.from("\r\n\r\n", "utf8");
const HEADER_NEEDLE = Buffer.from("Content-Length:", "utf8");

export function encodeMessage(body: string): Buffer {
  const len = Buffer.byteLength(body, "utf8");
  return Buffer.from(`Content-Length: ${len}\r\n\r\n${body}`, "utf8");
}

export class FrameDecoder {
  private buffer = Buffer.alloc(0);

  /** Non-fatal framing diagnostics (skipped garbage, oversized frames). */
  onDiagnostic: ((message: string) => void) | undefined;

  push(chunk: Buffer): string[] {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    const out: string[] = [];

    while (true) {
      const headerEnd = this.buffer.indexOf(HEADER_END);
      if (headerEnd < 0) {
        // No complete header yet. If the buffered prefix cannot possibly be a
        // header (no "Content-Length:" anywhere), drop garbage to resync.
        if (this.buffer.length > 8192 && this.buffer.indexOf(HEADER_NEEDLE) < 0) {
          this.diag(`dropping ${this.buffer.length} bytes of non-framed output`);
          this.buffer = Buffer.alloc(0);
        }
        break;
      }
      const header = this.buffer.subarray(0, headerEnd).toString("utf8");
      const match = /Content-Length:\s*(\d+)/i.exec(header);
      if (!match) {
        // Garbage before/instead of a header (e.g. stray print to stdout).
        // Resync: skip to the next possible header instead of poisoning the stream.
        const next = this.buffer.indexOf(HEADER_NEEDLE, 1);
        this.diag(
          `invalid frame header, resyncing: ${JSON.stringify(header.slice(0, 120))}`
        );
        this.buffer = next > 0 ? this.buffer.subarray(next) : this.buffer.subarray(headerEnd + 4);
        continue;
      }
      const contentLength = Number.parseInt(match[1]!, 10);
      if (!Number.isFinite(contentLength) || contentLength < 0 || contentLength > MAX_CONTENT_LENGTH) {
        // Skip this header and resync — waiting for an absurd body would hang forever.
        this.diag(`invalid Content-Length ${match[1]}, resyncing`);
        this.buffer = this.buffer.subarray(headerEnd + 4);
        continue;
      }
      const bodyStart = headerEnd + 4;
      const bodyEnd = bodyStart + contentLength;
      if (this.buffer.length < bodyEnd) {
        break;
      }
      const body = this.buffer.subarray(bodyStart, bodyEnd).toString("utf8");
      this.buffer = this.buffer.subarray(bodyEnd);
      out.push(body);
    }

    return out;
  }

  private diag(message: string): void {
    try {
      this.onDiagnostic?.(message);
    } catch {
      // diagnostics must never break decoding
    }
  }
}
