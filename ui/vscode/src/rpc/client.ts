import { ChildProcessWithoutNullStreams, spawn } from "child_process";
import { EventEmitter } from "events";
import { encodeMessage, FrameDecoder } from "./framing";

export interface JsonRpcError {
  code: number;
  message: string;
  data?: unknown;
}

export interface JsonRpcResponse {
  jsonrpc: "2.0";
  id: number | string | null;
  result?: unknown;
  error?: JsonRpcError;
}

type Pending = {
  resolve: (value: unknown) => void;
  reject: (err: Error) => void;
};

export type ServerRequestHandler = (
  method: string,
  params: unknown,
  id: number | string
) => Promise<unknown> | unknown;

/**
 * Minimal JSON-RPC 2.0 client over orchestra core stdio.
 * - "notification" — server notifications (no id)
 * - "serverRequest" — server-initiated requests (method + id); use setServerRequestHandler
 * - "stderr" / "error" / "exit"
 */
export class RpcClient extends EventEmitter {
  private readonly proc: ChildProcessWithoutNullStreams;
  private readonly decoder = new FrameDecoder();
  private readonly pending = new Map<number | string, Pending>();
  private nextId = 1;
  private closed = false;
  private stderrBuf = "";
  private serverRequestHandler: ServerRequestHandler | undefined;

  constructor(
    binaryPath: string,
    args: string[],
    options: { cwd?: string; env?: NodeJS.ProcessEnv } = {}
  ) {
    super();
    this.proc = spawn(binaryPath, args, {
      cwd: options.cwd,
      env: { ...process.env, ...options.env },
      stdio: ["pipe", "pipe", "pipe"],
      windowsHide: true,
    });

    this.proc.stdout.on("data", (chunk: Buffer) => {
      try {
        for (const body of this.decoder.push(chunk)) {
          this.handleMessage(body);
        }
      } catch (err) {
        this.emit("error", err instanceof Error ? err : new Error(String(err)));
      }
    });

    this.proc.stderr.on("data", (chunk: Buffer) => {
      this.stderrBuf += chunk.toString("utf8");
      if (this.stderrBuf.length > 64_000) {
        this.stderrBuf = this.stderrBuf.slice(-32_000);
      }
      this.emit("stderr", chunk.toString("utf8"));
    });

    this.proc.on("error", (err) => {
      this.emit("error", err);
    });

    this.proc.on("exit", (code, signal) => {
      this.closed = true;
      const msg = `orchestra core exited (code=${code}, signal=${signal})`;
      for (const [, p] of this.pending) {
        p.reject(new Error(msg + (this.stderrBuf ? `\nstderr:\n${this.stderrBuf}` : "")));
      }
      this.pending.clear();
      this.emit("exit", code, signal);
    });
  }

  get pid(): number | undefined {
    return this.proc.pid;
  }

  get isClosed(): boolean {
    return this.closed;
  }

  setServerRequestHandler(handler: ServerRequestHandler | undefined): void {
    this.serverRequestHandler = handler;
  }

  async request(method: string, params?: unknown, timeoutMs = 30_000): Promise<unknown> {
    if (this.closed) {
      throw new Error("rpc client is closed");
    }
    const id = this.nextId++;
    const payload = JSON.stringify({
      jsonrpc: "2.0",
      id,
      method,
      params: params ?? {},
    });

    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`rpc timeout after ${timeoutMs}ms: ${method}`));
      }, timeoutMs);

      this.pending.set(id, {
        resolve: (value) => {
          clearTimeout(timer);
          resolve(value);
        },
        reject: (err) => {
          clearTimeout(timer);
          reject(err);
        },
      });

      try {
        this.proc.stdin.write(encodeMessage(payload));
      } catch (err) {
        clearTimeout(timer);
        this.pending.delete(id);
        reject(err instanceof Error ? err : new Error(String(err)));
      }
    });
  }

  /** Reply to a server-initiated request. */
  respond(id: number | string, result: unknown): void {
    if (this.closed) {
      return;
    }
    const payload = JSON.stringify({
      jsonrpc: "2.0",
      id,
      result,
    });
    this.proc.stdin.write(encodeMessage(payload));
  }

  respondError(id: number | string, code: number, message: string): void {
    if (this.closed) {
      return;
    }
    const payload = JSON.stringify({
      jsonrpc: "2.0",
      id,
      error: { code, message },
    });
    this.proc.stdin.write(encodeMessage(payload));
  }

  dispose(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    for (const [, p] of this.pending) {
      p.reject(new Error("rpc client disposed"));
    }
    this.pending.clear();
    try {
      this.proc.stdin.end();
    } catch {
      // ignore
    }
    this.proc.kill();
  }

  private handleMessage(body: string): void {
    let msg: JsonRpcResponse & { method?: string; params?: unknown };
    try {
      msg = JSON.parse(body) as JsonRpcResponse & { method?: string; params?: unknown };
    } catch {
      this.emit("error", new Error(`invalid json from core: ${body.slice(0, 200)}`));
      return;
    }

    // Server → client: notification or request
    if (typeof msg.method === "string") {
      const hasId = msg.id !== undefined && msg.id !== null;
      if (!hasId) {
        this.emit("notification", msg.method, msg.params);
        return;
      }
      void this.dispatchServerRequest(msg.method, msg.params, msg.id as number | string);
      return;
    }

    // Response to our request
    if (msg.id === undefined || msg.id === null) {
      return;
    }

    const pending = this.pending.get(msg.id);
    if (!pending) {
      return;
    }
    this.pending.delete(msg.id);

    if (msg.error) {
      pending.reject(new Error(`rpc error ${msg.error.code}: ${msg.error.message}`));
      return;
    }
    pending.resolve(msg.result);
  }

  private async dispatchServerRequest(
    method: string,
    params: unknown,
    id: number | string
  ): Promise<void> {
    this.emit("serverRequest", method, params, id);
    try {
      if (this.serverRequestHandler) {
        const result = await this.serverRequestHandler(method, params, id);
        this.respond(id, result);
        return;
      }
      // Method not found — core falls back to static permission gate for permission/request
      this.respondError(id, -32601, `Method not found: ${method}`);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.respondError(id, -32000, message);
    }
  }
}
