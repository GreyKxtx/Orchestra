import { spawn } from "child_process";
import * as vscode from "vscode";
import type { CoreSession } from "../coreSession";
import { resolveBinaryPath, resolveProjectRoot } from "../coreSession";

type PostFn = (msg: Record<string, unknown>) => void;

/**
 * Settings UI hosted inside the Orchestra chat panel (same webview, swap HTML).
 * Six tabs: General, Models, Index & Graph, Agent, Tools & MCP, Plugins.
 */
export class SettingsView {
  private readonly session: CoreSession;
  private readonly extensionUri: vscode.Uri;
  private post: PostFn = () => undefined;

  constructor(session: CoreSession, extensionUri: vscode.Uri) {
    this.session = session;
    this.extensionUri = extensionUri;
  }

  bindPost(post: PostFn): void {
    this.post = post;
  }

  getHtml(webview: vscode.Webview): string {
    const v = String(Date.now());
    const styleUri = webview
      .asWebviewUri(vscode.Uri.joinPath(this.extensionUri, "media", "settings.css"))
      .with({ query: `v=${v}` });
    const scriptUri = webview
      .asWebviewUri(vscode.Uri.joinPath(this.extensionUri, "media", "settings.js"))
      .with({ query: `v=${v}` });
    const nonce = getNonce();
    const csp = [
      `default-src 'none'`,
      `style-src ${webview.cspSource}`,
      `script-src 'nonce-${nonce}'`,
    ].join("; ");

    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link rel="stylesheet" href="${styleUri}" />
  <title>Orchestra Settings</title>
</head>
<body>
  <div id="layout">
    <nav id="nav">
      <div class="nav-top">
        <button type="button" id="backChat" class="back">← Chat</button>
        <div class="brand">Settings</div>
      </div>
      <div class="nav-items">
        <button type="button" class="nav-item active" data-section="general">General</button>
        <button type="button" class="nav-item" data-section="models">Models</button>
        <button type="button" class="nav-item" data-section="index">Index &amp; Graph</button>
        <button type="button" class="nav-item" data-section="agent">Agent</button>
        <button type="button" class="nav-item" data-section="tools">Tools &amp; MCP</button>
        <button type="button" class="nav-item" data-section="plugins">Plugins</button>
      </div>
    </nav>
    <main id="main">
      <div id="error" class="error hidden"></div>

      <section id="sec-general" class="panel active">
        <h1>General</h1>
        <p class="sub">Workspace and Orchestra core connection</p>
        <label>Binary path <input id="binaryPath" type="text" placeholder="auto-detect orchestra.exe" /></label>
        <label>Project root <input id="projectRoot" type="text" placeholder="workspace folder" /></label>
        <p class="hint">Restart core after changing binary or project root.</p>
        <footer>
          <button type="button" id="reload" class="secondary">Reload all</button>
          <button type="button" id="saveGeneral">Save</button>
        </footer>
      </section>

      <section id="sec-models" class="panel">
        <h1>Models</h1>
        <p class="sub">LLM provider and model — saved to <code>.orchestra.yml</code></p>
        <h2>Provider</h2>
        <div id="providerList" class="pick-list"></div>
        <p id="providerStatus" class="hint"></p>
        <div id="providerCreds" class="cred-block hidden">
          <label>API Base <input id="apiBase" type="text" placeholder="http://127.0.0.1:8000/v1" /></label>
          <label>API Key <input id="apiKey" type="password" placeholder="leave blank to keep current" autocomplete="off" />
            <span id="keyHint" class="hint"></span>
          </label>
        </div>
        <h2>Model</h2>
        <p id="modelsStatus" class="hint"></p>
        <div id="modelList" class="pick-list model-list"></div>
        <input id="model" type="hidden" />
        <input id="provider" type="hidden" />
        <details class="adv">
          <summary>Advanced generation</summary>
          <label>Prompt family <input id="promptFamily" type="text" placeholder="auto / local / gpt / anthropic" /></label>
          <label>Temperature <input id="temperature" type="number" step="0.1" min="0" max="2" /></label>
          <label>Max tokens <input id="maxTokens" type="number" min="0" step="1" /></label>
          <label>Timeout (s) <input id="timeoutS" type="number" min="0" step="1" /></label>
          <label class="row check"><input id="multimodal" type="checkbox" /> Vision / multimodal (image attachments)</label>
        </details>
        <footer class="inline-footer">
          <button type="button" id="refreshModels" class="secondary">Refresh models</button>
          <button type="button" id="saveModels">Save</button>
        </footer>
      </section>

      <section id="sec-index" class="panel">
        <h1>Index &amp; Graph</h1>
        <p class="sub">CKG structural graph + semantic embeddings for <code>explore</code> / <code>semantic_search</code></p>
        <div id="indexStats" class="stat-grid">
          <div class="stat"><span class="stat-val" id="statFiles">—</span><span class="stat-lbl">files</span></div>
          <div class="stat"><span class="stat-val" id="statNodes">—</span><span class="stat-lbl">nodes</span></div>
          <div class="stat"><span class="stat-val" id="statEdges">—</span><span class="stat-lbl">edges</span></div>
          <div class="stat"><span class="stat-val" id="statEmb">—</span><span class="stat-lbl">embeddings</span></div>
        </div>
        <p id="indexStatusHint" class="hint"></p>
        <div class="action-row">
          <button type="button" id="rebuildGraph" class="secondary">Rebuild graph</button>
          <button type="button" id="runEmbed" class="secondary">Run embed</button>
          <button type="button" id="openGraph" class="secondary">Open graph viewer</button>
        </div>
        <p id="indexActionOut" class="hint"></p>
        <h2>Scope</h2>
        <label>Exclude dirs (one per line) <textarea id="excludeDirs" rows="4" placeholder=".git&#10;node_modules"></textarea></label>
        <label>Context limit (KB) <input id="contextLimitKB" type="number" min="0" step="1" /></label>
        <label>Max files <input id="limitsMaxFiles" type="number" min="0" step="1" placeholder="optional" /></label>
        <h2>Semantic search (embed)</h2>
        <label>Embed API base <input id="embedAPIBase" type="text" placeholder="http://127.0.0.1:8000/v1" /></label>
        <label>Embed API key <input id="embedAPIKey" type="password" placeholder="leave blank to keep" autocomplete="off" /></label>
        <label>Embed model <input id="embedModel" type="text" placeholder="text-embedding-…" /></label>
        <label>Batch size <input id="embedBatchSize" type="number" min="1" step="1" /></label>
        <label class="row check"><input id="semanticAutoExplore" type="checkbox" /> Auto-explore top semantic hits</label>
        <footer><button type="button" id="saveIndex">Save index settings</button></footer>
        <details class="adv">
          <summary>LSP enhancement</summary>
          <p class="hint">Language servers improve diagnostics and navigation. Configure <code>lsp.servers</code> in <code>.orchestra.yml</code> for now.</p>
        </details>
      </section>

      <section id="sec-agent" class="panel">
        <h1>Agent</h1>
        <p class="sub">System prompt override and custom named agents</p>
        <h2>System prompt</h2>
        <label>Override<textarea id="systemPrompt" rows="10" placeholder="Leave empty to use built-in prompts…"></textarea></label>
        <p id="promptPath" class="hint"></p>
        <footer class="inline-footer">
          <button type="button" id="clearPrompt" class="secondary">Clear override</button>
          <button type="button" id="savePrompt">Save prompt</button>
        </footer>
        <h2>Custom agents</h2>
        <p class="sub">Use as <code>mode</code> in chat / <code>agent.run</code></p>
        <div id="agentsList" class="list"></div>
        <label>Name <input id="agentName" type="text" placeholder="reviewer" /></label>
        <label>System prompt <textarea id="agentPrompt" rows="5"></textarea></label>
        <label>Tools (comma-separated; empty = inherit)
          <input id="agentTools" type="text" placeholder="read, grep, ls" />
        </label>
        <label>Model <input id="agentModel" type="text" placeholder="optional override" /></label>
        <label>Provider <input id="agentProvider" type="text" placeholder="optional providers: key" /></label>
        <footer>
          <button type="button" id="newAgent" class="secondary">New</button>
          <button type="button" id="deleteAgent" class="danger">Delete</button>
          <button type="button" id="saveAgent">Save agent</button>
        </footer>
      </section>

      <section id="sec-tools" class="panel">
        <h1>Tools &amp; MCP</h1>
        <p class="sub">External MCP servers — hot-reload without restarting core</p>
        <div class="preset-row">
          <button type="button" class="secondary mcp-preset" data-preset="filesystem">+ filesystem</button>
          <button type="button" class="secondary mcp-preset" data-preset="github">+ github</button>
          <button type="button" class="secondary mcp-preset" data-preset="memory">+ memory</button>
          <button type="button" class="secondary mcp-preset" data-preset="custom">+ custom</button>
        </div>
        <div id="mcpList" class="list"></div>
        <h2>Edit MCP server</h2>
        <label>Name <input id="mcpName" type="text" placeholder="filesystem" /></label>
        <label>Command <input id="mcpCommand" type="text" placeholder='npx -y @modelcontextprotocol/server-filesystem .' /></label>
        <label>Env (KEY=VAL per line) <textarea id="mcpEnv" rows="3"></textarea></label>
        <label>Allowed tools <input id="mcpAllowed" type="text" placeholder="empty = all" /></label>
        <label>Call timeout (s) <input id="mcpTimeout" type="number" min="0" step="1" value="0" /></label>
        <label class="row check"><input id="mcpDisabled" type="checkbox" /> Disabled</label>
        <p id="mcpTestOut" class="hint"></p>
        <footer>
          <button type="button" id="testMCP" class="secondary">Test</button>
          <button type="button" id="deleteMCP" class="danger">Delete</button>
          <button type="button" id="saveMCP">Save &amp; reload</button>
        </footer>
        <details class="adv">
          <summary>Hooks, Git, Browser</summary>
          <p class="hint">Configure <code>hooks</code>, <code>exec</code>, <code>web</code>, and <code>browser</code> in <code>.orchestra.yml</code>. UI editors coming later.</p>
        </details>
      </section>

      <section id="sec-plugins" class="panel">
        <h1>Plugins</h1>
        <p class="sub">Installed skills / packs — install via CLI: <code>orchestra skills install …</code></p>
        <div id="skillsList" class="list"></div>
        <p class="hint">Plugins bundle skills, rules, and optional MCP configs. MCP servers are managed under Tools &amp; MCP.</p>
      </section>
    </main>
  </div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
  }

  async pushState(): Promise<void> {
    try {
      const [llm, prompt, agents, mcp, index, skills, providerCatalog] = await Promise.all([
        this.session.getLLM(),
        this.session.getSystemPrompt(),
        this.session.listAgents(),
        this.session.listMCP(),
        this.session.getIndexStatus(),
        this.session.listSkills(),
        this.session.listProviders({ probe: true }),
      ]);
      const bin = vscode.workspace.getConfiguration("orchestra").get<string>("binaryPath") || "";
      const root = vscode.workspace.getConfiguration("orchestra").get<string>("projectRoot") || "";
      const ws = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || "";
      this.post({
        type: "state",
        llm,
        prompt,
        agents,
        mcp,
        index,
        skills,
        providerCatalog,
        extension: { binaryPath: bin, projectRoot: root },
        workspaceRoot: ws,
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.post({ type: "error", message });
    }
  }

  /** Returns true if the message was handled as a settings action. */
  async handleMessage(raw: unknown): Promise<boolean> {
    if (!raw || typeof raw !== "object") {
      return false;
    }
    const msg = raw as { type?: string; [k: string]: unknown };
    const t = msg.type;
    const settingsTypes = new Set([
      "ready",
      "reload",
      "saveGeneral",
      "saveModels",
      "saveIndex",
      "savePrompt",
      "clearPrompt",
      "upsertAgent",
      "deleteAgent",
      "upsertMCP",
      "deleteMCP",
      "setMCPDisabled",
      "testMCP",
      "rebuildGraph",
      "runEmbed",
      "openGraphViewer",
      "refreshModels",
      "backToChat",
    ]);
    if (!t || !settingsTypes.has(t)) {
      return false;
    }
    if (t === "backToChat") {
      return true;
    }

    try {
      if (t === "ready" || t === "reload") {
        await this.pushState();
        return true;
      }
      if (t === "saveGeneral") {
        const cfg = vscode.workspace.getConfiguration("orchestra");
        await cfg.update(
          "binaryPath",
          String(msg.binaryPath || "").trim(),
          vscode.ConfigurationTarget.Workspace
        );
        await cfg.update(
          "projectRoot",
          String(msg.projectRoot || "").trim(),
          vscode.ConfigurationTarget.Workspace
        );
        void vscode.window.showInformationMessage("General settings saved");
        await this.pushState();
        return true;
      }
      if (t === "refreshModels") {
        const probeKey = String(msg.provider || "").trim();
        const apiBase = String(msg.apiBase || "").trim();
        const apiKey = String(msg.apiKey || "").trim();
        if (probeKey && (apiBase || apiKey)) {
          await this.session.configureLLM({
            provider: probeKey,
            apiBase: apiBase || undefined,
            apiKey: apiKey || undefined,
            persist: false,
          });
        }
        this.post({ type: "modelsBusy", busy: true });
        const catalog = await this.session.listProviders(
          probeKey ? { probeKey } : { probe: true }
        );
        this.post({ type: "providerCatalog", catalog, probeKey: probeKey || undefined });
        this.post({ type: "modelsBusy", busy: false });
        return true;
      }
      if (t === "saveModels") {
        await this.session.configureLLM({
          apiBase: String(msg.apiBase || "").trim() || undefined,
          apiKey: String(msg.apiKey || "").trim() || undefined,
          model: String(msg.model || "").trim() || undefined,
          provider: String(msg.provider || "").trim() || undefined,
          temperature: numOrUndef(msg.temperature),
          maxTokens: posIntOrUndef(msg.maxTokens),
          timeoutS: posIntOrUndef(msg.timeoutS),
          promptFamily: msg.promptFamily !== undefined ? String(msg.promptFamily) : undefined,
          multimodal: msg.multimodal === undefined ? undefined : Boolean(msg.multimodal),
          persist: true,
        });
        void vscode.window.showInformationMessage("Model settings saved");
        await this.pushState();
        return true;
      }
      if (t === "saveIndex") {
        const excludeRaw = String(msg.excludeDirs || "");
        const excludeDirs = excludeRaw
          .split(/\r?\n/)
          .map((x) => x.trim())
          .filter(Boolean);
        await this.session.configureIndex({
          excludeDirs,
          contextLimitKB: posIntOrUndef(msg.contextLimitKB),
          limitsMaxFiles: posIntOrUndef(msg.limitsMaxFiles),
          embedAPIBase: String(msg.embedAPIBase || "").trim() || undefined,
          embedAPIKey: String(msg.embedAPIKey || "").trim() || undefined,
          embedModel: String(msg.embedModel || "").trim() || undefined,
          embedBatchSize: posIntOrUndef(msg.embedBatchSize),
          semanticAutoExplore:
            msg.semanticAutoExplore === undefined ? undefined : Boolean(msg.semanticAutoExplore),
        });
        void vscode.window.showInformationMessage("Index settings saved");
        await this.pushState();
        return true;
      }
      if (t === "rebuildGraph") {
        this.post({ type: "indexBusy", busy: true, message: "Rebuilding graph…" });
        const graph = await this.session.rebuildIndex();
        this.post({ type: "indexActionResult", action: "rebuild", graph });
        void vscode.window.showInformationMessage(
          `Graph rebuilt: ${graph.files} files, ${graph.nodes} nodes`
        );
        await this.pushState();
        return true;
      }
      if (t === "runEmbed") {
        this.post({ type: "indexBusy", busy: true, message: "Running embed…" });
        const result = await this.session.embedIndex({ rebuild: Boolean(msg.rebuild) });
        this.post({ type: "indexActionResult", action: "embed", result });
        void vscode.window.showInformationMessage(
          `Embed done: +${result.embedded} (${result.total} total, ${result.elapsed})`
        );
        await this.pushState();
        return true;
      }
      if (t === "openGraphViewer") {
        const root = await resolveProjectRoot();
        const binary = resolveBinaryPath(root, this.extensionUri.fsPath);
        const port = posIntOrUndef(msg.port) || 6061;
        const child = spawn(binary, ["ckg-ui", "-p", String(port)], {
          cwd: root,
          detached: true,
          stdio: "ignore",
          windowsHide: true,
        });
        child.unref();
        await vscode.env.openExternal(vscode.Uri.parse(`http://127.0.0.1:${port}`));
        return true;
      }
      if (t === "savePrompt") {
        await this.session.setSystemPrompt({
          content: String(msg.content ?? ""),
          promptFamily:
            msg.promptFamily !== undefined ? String(msg.promptFamily) : undefined,
        });
        void vscode.window.showInformationMessage("System prompt saved");
        await this.pushState();
        return true;
      }
      if (t === "clearPrompt") {
        await this.session.setSystemPrompt({ clear: true });
        void vscode.window.showInformationMessage("System prompt override cleared");
        await this.pushState();
        return true;
      }
      if (t === "upsertAgent") {
        const toolsRaw = String(msg.tools || "").trim();
        const tools = toolsRaw
          ? toolsRaw.split(/[,\s]+/).map((x) => x.trim()).filter(Boolean)
          : undefined;
        await this.session.upsertAgent({
          name: String(msg.name || "").trim(),
          system_prompt: String(msg.system_prompt || ""),
          tools,
          model: String(msg.model || "").trim() || undefined,
          provider: String(msg.agentProvider || msg.provider || "").trim() || undefined,
        });
        void vscode.window.showInformationMessage("Agent saved");
        await this.pushState();
        return true;
      }
      if (t === "deleteAgent") {
        await this.session.deleteAgent(String(msg.name || "").trim());
        void vscode.window.showInformationMessage("Agent deleted");
        await this.pushState();
        return true;
      }
      if (t === "upsertMCP") {
        const command = parseCommand(String(msg.command || ""));
        const env = parseEnv(String(msg.env || ""));
        const allowed = String(msg.allowed_tools || "")
          .split(/[,\s]+/)
          .map((x) => x.trim())
          .filter(Boolean);
        const result = await this.session.upsertMCP({
          name: String(msg.name || "").trim(),
          command,
          env,
          disabled: Boolean(msg.disabled),
          call_timeout_s: posIntOrUndef(msg.call_timeout_s),
          allowed_tools: allowed.length ? allowed : undefined,
        });
        const warn =
          result.warnings.length > 0
            ? ` (warnings: ${result.warnings.slice(0, 2).join("; ")})`
            : "";
        void vscode.window.showInformationMessage(`MCP reloaded${warn}`);
        await this.pushState();
        return true;
      }
      if (t === "deleteMCP") {
        await this.session.deleteMCP(String(msg.name || "").trim());
        void vscode.window.showInformationMessage("MCP server deleted");
        await this.pushState();
        return true;
      }
      if (t === "setMCPDisabled") {
        await this.session.setMCPDisabled(
          String(msg.name || "").trim(),
          Boolean(msg.disabled)
        );
        await this.pushState();
        return true;
      }
      if (t === "testMCP") {
        const command = parseCommand(String(msg.command || ""));
        const env = parseEnv(String(msg.env || ""));
        const name = String(msg.name || "").trim();
        const result = await this.session.testMCP(
          command.length > 0
            ? {
                server: {
                  name: name || "test",
                  command,
                  env,
                  call_timeout_s: posIntOrUndef(msg.call_timeout_s),
                },
              }
            : { name }
        );
        this.post({ type: "mcpTestResult", result });
        return true;
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.post({ type: "error", message });
      this.post({ type: "indexBusy", busy: false });
      void vscode.window.showErrorMessage(`Orchestra settings: ${message}`);
      return true;
    }
    return false;
  }
}

function numOrUndef(v: unknown): number | undefined {
  const n = Number(v);
  return Number.isFinite(n) ? n : undefined;
}

function posIntOrUndef(v: unknown): number | undefined {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : undefined;
}

function parseCommand(s: string): string[] {
  const t = s.trim();
  if (!t) {
    return [];
  }
  const out: string[] = [];
  const re = /"([^"]*)"|'([^']*)'|(\S+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(t))) {
    out.push(m[1] ?? m[2] ?? m[3] ?? "");
  }
  return out.filter(Boolean);
}

function parseEnv(s: string): Record<string, string> | undefined {
  const env: Record<string, string> = {};
  for (const line of s.split(/\r?\n/)) {
    const t = line.trim();
    if (!t || t.startsWith("#")) {
      continue;
    }
    const i = t.indexOf("=");
    if (i <= 0) {
      continue;
    }
    env[t.slice(0, i).trim()] = t.slice(i + 1).trim();
  }
  return Object.keys(env).length ? env : undefined;
}

function getNonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) {
    out += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return out;
}
