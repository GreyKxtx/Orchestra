/**
 * LSP-style Content-Length framing for Orchestra JSON-RPC over stdio.
 * Wire format matches internal/jsonrpc codec.
 */

export function encodeMessage(body: string): Buffer {
  const len = Buffer.byteLength(body, "utf8");
  return Buffer.from(`Content-Length: ${len}\r\n\r\n${body}`, "utf8");
}

export class FrameDecoder {
  private buffer = Buffer.alloc(0);

  push(chunk: Buffer): string[] {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    const out: string[] = [];

    while (true) {
      const headerEnd = indexOfHeaderEnd(this.buffer);
      if (headerEnd < 0) {
        break;
      }
      const header = this.buffer.subarray(0, headerEnd).toString("utf8");
      const match = /Content-Length:\s*(\d+)/i.exec(header);
      if (!match) {
        throw new Error(`missing Content-Length in header: ${JSON.stringify(header)}`);
      }
      const contentLength = Number.parseInt(match[1]!, 10);
      if (!Number.isFinite(contentLength) || contentLength < 0) {
        throw new Error(`invalid Content-Length: ${match[1]}`);
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
}

function indexOfHeaderEnd(buf: Buffer): number {
  const needle = Buffer.from("\r\n\r\n", "utf8");
  return buf.indexOf(needle);
}
