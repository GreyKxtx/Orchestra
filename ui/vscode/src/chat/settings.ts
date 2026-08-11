import { spawn } from "child_process";
import * as fs from "fs";
import * as path from "path";
import * as vscode from "vscode";
import type { CoreSession } from "../coreSession";
import { resolveBinaryPath, resolveProjectRoot } from "../coreSession";
import {
  enrichFeaturedVersions,
  fetchMcpRegistryCatalog,
  mapLocalCatalog,
  mergeMcpEntries,
  type McpCatalogEntry,
  type McpCatalogPayload,
} from "./mcpRegistry";

type PostFn = (msg: Record<string, unknown>) => void;

type McpCatalogFile = { version?: number; entries?: unknown[] };

function loadMcpCatalogFile(extensionUri: vscode.Uri): McpCatalogFile {
  try {
    const file = path.join(extensionUri.fsPath, "media", "mcp-catalog.json");
    const raw = fs.readFileSync(file, "utf8");
    const parsed = JSON.parse(raw) as McpCatalogFile;
    if (parsed && Array.isArray(parsed.entries)) {
      return parsed;
    }
  } catch {
    // fall through
  }
  return { version: 1, entries: [] };
}

/**
 * Settings UI hosted inside the Orchestra chat panel (same webview, swap HTML).
 * Six tabs: General, Models, Index & Graph, Agent, Tools & MCP, Plugins.
 */
export class SettingsView {
  private readonly session: CoreSession;
  private readonly extensionUri: vscode.Uri;
  private post: PostFn = () => undefined;
  /** Section to open when settings webview loads (e.g. orchestra). Cleared after first pushState. */
  pendingSection = "general";
  private registryFetchSeq = 0;

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
      .asWebviewUri(vscode.Uri.joinPath(this.extensionUri, "media", "settings.bundle.js"))
      .with({ query: `v=${v}` });
    const iconBaseUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, "media", "provider-icons")
    );
    const nonce = getNonce();
    const csp = [
      `default-src 'none'`,
      `style-src ${webview.cspSource}`,
      `script-src 'nonce-${nonce}'`,
      `img-src ${webview.cspSource} https: data:`,
    ].join("; ");

    const localCatalog = {
      version: 1,
      entries: mapLocalCatalog(loadMcpCatalogFile(this.extensionUri)),
      source: "local" as const,
    };

    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link rel="stylesheet" href="${styleUri}" />
  <script nonce="${nonce}">window.__ORCH_ICON_BASE=${JSON.stringify(String(iconBaseUri).replace(/\/?$/, "/"))};window.__ORCH_ICON_V=${JSON.stringify(v)};window.__ORCH_MCP_CATALOG=${JSON.stringify(localCatalog)};</script>
  <title>Orchestra Settings</title>
</head>
<body>
  <div id="layout">
    <nav id="nav">
      <div class="nav-top">
        <button type="button" id="backChat" class="back">← Chat</button>
      </div>
      <div class="nav-items">
        <button type="button" class="nav-item active" data-section="general">General</button>
        <button type="button" class="nav-item" data-section="models">Models</button>
        <button type="button" class="nav-item" data-section="orchestra">Orchestra</button>
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
          <button type="button" id="saveGeneral" class="primary">Save</button>
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
          <label>API Key
            <span class="api-key-row">
              <input id="apiKey" type="password" placeholder="paste API key" autocomplete="off" />
              <button type="button" id="toggleApiKey" class="secondary api-key-toggle">Show</button>
            </span>
            <span id="keyHint" class="hint"></span>
          </label>
        </div>
        <h2>Model</h2>
        <p id="modelsStatus" class="hint"></p>
        <input id="modelSearch" type="search" class="search-input" placeholder="Search models…" autocomplete="off" />
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
          <button type="button" id="saveModels" class="primary">Save</button>
        </footer>
      </section>

      <section id="sec-orchestra" class="panel">
        <h1>Orchestra</h1>
        <p class="sub">Lead planner and worker tiers (complex / focused / micro). Up to 3 models per role for failover.</p>
        <label>Shared provider
          <div id="orchSharedProviderWrap"></div>
        </label>
        <p class="hint">Pick one gateway (OpenRouter) and assign different models per role. Primary model = first selected.</p>
        <div id="orchRoles" class="orch-roles"></div>
        <details class="adv">
          <summary>Verification &amp; retries</summary>
          <label class="row check"><input id="orchVerifyEnabled" type="checkbox" checked /> Deterministic worker verify (LSP + go build)</label>
          <label class="row check"><input id="orchLLMVerify" type="checkbox" /> LLM verifier after deterministic pass</label>
          <label>Max worker retries <input id="orchMaxRetries" type="number" min="1" max="12" value="3" /></label>
          <label>Max verify retries <input id="orchMaxVerifyRetries" type="number" min="0" max="6" value="1" /></label>
          <label>Default tier
            <select id="orchDefaultTier">
              <option value="complex">complex</option>
              <option value="focused" selected>focused</option>
              <option value="micro">micro</option>
            </select>
          </label>
        </details>
        <footer class="inline-footer">
          <button type="button" id="refreshOrchModels" class="secondary">Refresh models</button>
          <button type="button" id="saveOrchestra" class="secondary">Save orchestra</button>
        </footer>
        <div id="orchModelModal" class="orch-modal hidden" role="dialog" aria-modal="true">
          <div class="orch-modal-card">
            <div class="orch-modal-head">
              <strong id="orchModalTitle">Pick models</strong>
              <button type="button" id="orchModalClose" class="secondary">✕</button>
            </div>
            <input id="orchModelSearch" type="search" class="search-input" placeholder="Search models…" autocomplete="off" />
            <div id="orchModalSlots" class="orch-modal-slots"></div>
            <p id="orchModalHint" class="hint orch-modal-hint"></p>
            <div id="orchModelPickList" class="orch-pick-list"></div>
            <footer class="inline-footer">
              <button type="button" id="orchModalApply" class="secondary">Apply</button>
            </footer>
          </div>
        </div>
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
        <p class="sub">Prompts are model-independent — one shared prompt, plus optional project override. Custom agents add a named prompt and tool set.</p>
        <h2>System prompt</h2>
        <label>Project override<textarea id="systemPrompt" rows="10" placeholder="Leave empty to use the built-in / shared prompt…"></textarea></label>
        <p id="promptPath" class="hint"></p>
        <footer class="inline-footer">
          <button type="button" id="clearPrompt" class="secondary">Clear override</button>
          <button type="button" id="savePrompt">Save prompt</button>
        </footer>
        <h2>Custom agents</h2>
        <p class="sub">Use as <code>mode</code> in chat / <code>agent.run</code>. Prompt applies to all models.</p>
        <div id="agentsList" class="list"></div>
        <label>Name <input id="agentName" type="text" placeholder="reviewer" /></label>
        <label>System prompt <textarea id="agentPrompt" rows="5"></textarea></label>
        <div class="agent-tools-block">
          <h3 class="agent-tools-title">Tools</h3>
          <p class="hint">Toggle tools for this agent. All on = inherit full build toolset.</p>
          <div id="agentToolsList" class="agent-tools-list"></div>
          <p id="agentToolsHint" class="hint"></p>
        </div>
        <footer>
          <button type="button" id="newAgent" class="secondary">New</button>
          <button type="button" id="deleteAgent" class="danger">Delete</button>
          <button type="button" id="saveAgent">Save agent</button>
        </footer>
      </section>

      <section id="sec-tools" class="panel">
        <h1>Tools &amp; MCP</h1>
        <p class="sub">Browse the official MCP Registry (plus featured locals) — install into <code>.orchestra.yml</code>. Installed servers use on/off toggles; open one to configure tools.</p>
        <div class="mcp-tabs" role="tablist">
          <button type="button" class="mcp-tab active" data-mcp-tab="browse" role="tab" aria-selected="true">Browse</button>
          <button type="button" class="mcp-tab" data-mcp-tab="installed" role="tab" aria-selected="false">Installed</button>
        </div>
        <div id="mcpBrowsePane">
          <input id="mcpCatalogSearch" type="search" class="search-input" placeholder="Search registry (filesystem, github, slack…)" autocomplete="off" />
          <div id="mcpCatalogCats" class="mcp-cats"></div>
          <p id="mcpCatalogStatus" class="hint">Loading catalog…</p>
          <div id="mcpCatalogList" class="mcp-catalog"></div>
          <div class="mcp-catalog-footer">
            <button type="button" id="mcpCatalogPrev" class="secondary">Prev</button>
            <span id="mcpCatalogPageLabel" class="mcp-page-label">1 / 1</span>
            <button type="button" id="mcpCatalogNext" class="secondary">Next</button>
          </div>
        </div>
        <div id="mcpInstalledPane" class="hidden">
          <div id="mcpList" class="mcp-installed-list"></div>
          <button type="button" id="mcpAddCustom" class="secondary mcp-add-custom">+ Custom server</button>
        </div>
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
  <div id="mcpConfigure" class="mcp-modal hidden" role="dialog" aria-modal="true" aria-labelledby="mcpCfgTitle">
    <div class="mcp-modal-card" id="mcpConfigureCard">
      <header class="mcp-cfg-head">
        <div class="mcp-cfg-title-wrap">
          <h2 id="mcpCfgTitle">Configure</h2>
          <p id="mcpCfgSub" class="mcp-cfg-sub"></p>
        </div>
        <button type="button" id="mcpCfgClose" class="mcp-cfg-close" aria-label="Close">×</button>
      </header>
      <div class="mcp-modal-body">
        <section class="mcp-cfg-section">
          <h3>Source</h3>
          <p class="hint">Command and environment for this MCP server.</p>
          <div class="mcp-cfg-card">
            <div class="mcp-cfg-card-main">
              <strong id="mcpCfgSourceLabel">Server</strong>
              <span id="mcpCfgSourceMeta" class="mcp-cfg-meta"></span>
            </div>
            <label class="mcp-switch" title="Enable server">
              <input id="mcpEnabled" type="checkbox" />
              <span class="mcp-switch-ui" aria-hidden="true"></span>
            </label>
          </div>
          <label>Name <input id="mcpName" type="text" placeholder="filesystem" /></label>
          <label>Command <input id="mcpCommand" type="text" placeholder='npx -y @modelcontextprotocol/server-filesystem .' /></label>
          <label>Env (KEY=VAL per line) <textarea id="mcpEnv" rows="3"></textarea></label>
        </section>
        <section class="mcp-cfg-section">
          <h3>Tools</h3>
          <p class="hint">Enable or disable individual tools.</p>
          <div id="mcpToolsList" class="mcp-tools-list"></div>
          <p id="mcpToolsHint" class="hint"></p>
        </section>
        <p id="mcpTestOut" class="hint"></p>
      </div>
      <footer class="mcp-cfg-footer">
        <button type="button" id="deleteMCP" class="danger hidden">Remove</button>
        <div class="mcp-cfg-footer-right">
          <button type="button" id="reloadMCP" class="secondary">Reload</button>
          <button type="button" id="saveMCP">Done</button>
        </div>
      </footer>
    </div>
  </div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
  }

  async pushState(): Promise<void> {
    try {
      const [llm, prompt, agents, mcp, index, skills, providerCatalog, orchestra] = await Promise.all([
        this.session.getLLM(),
        this.session.getSystemPrompt(),
        this.session.listAgents(),
        this.session.listMCP(),
        this.session.getIndexStatus(),
        this.session.listSkills(),
        this.session.listProviders({ probe: true, includeSecrets: true }),
        this.session.getOrchestra().catch(() => null),
      ]);
      const bin = vscode.workspace.getConfiguration("orchestra").get<string>("binaryPath") || "";
      const root = vscode.workspace.getConfiguration("orchestra").get<string>("projectRoot") || "";
      const ws = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || "";
      const localEntries = mapLocalCatalog(loadMcpCatalogFile(this.extensionUri), ws);
      const navigateSection = this.pendingSection;
      this.pendingSection = "";
      this.post({
        type: "state",
        llm,
        prompt,
        agents,
        mcp,
        index,
        skills,
        providerCatalog,
        orchestra,
        ...(navigateSection ? { navigateSection } : {}),
        extension: { binaryPath: bin, projectRoot: root },
        workspaceRoot: ws,
        mcpCatalog: {
          version: 1,
          entries: localEntries,
          source: "local",
        } satisfies McpCatalogPayload,
      });
      void this.pullMcpRegistry({ search: "" });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.post({ type: "error", message });
    }
  }

  private workspaceRootPath(): string {
    return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || "";
  }

  private featuredLocalEntries(workspaceRoot?: string): McpCatalogEntry[] {
    return mapLocalCatalog(loadMcpCatalogFile(this.extensionUri), workspaceRoot || this.workspaceRootPath());
  }

  private async pullMcpRegistry(opts: {
    search?: string;
  }): Promise<void> {
    const seq = ++this.registryFetchSeq;
    const search = String(opts.search || "").trim();
    const workspaceRoot = this.workspaceRootPath();
    const featured = search ? [] : this.featuredLocalEntries(workspaceRoot);

    this.post({
      type: "mcpCatalogBusy",
      busy: true,
      message: search ? `Searching registry for “${search}”…` : "Loading MCP Registry…",
      prefetching: true,
    });

    let cursor: string | undefined;
    let remoteAll: McpCatalogEntry[] = [];
    let source: McpCatalogPayload["source"] = search ? "registry" : "mixed";
    let lastError = "";

    try {
      // Pull every page in batches; UI paginates locally by 20.
      for (let page = 0; page < 80; page++) {
        const remote = await fetchMcpRegistryCatalog({
          search,
          cursor,
          limit: 100,
          workspaceRoot,
        });
        if (seq !== this.registryFetchSeq) {
          return;
        }
        remoteAll = mergeMcpEntries(remoteAll, remote.entries);
        const entries = search
          ? remoteAll
          : mergeMcpEntries(enrichFeaturedVersions(featured, remoteAll), remoteAll);
        source = search ? "registry" : "mixed";
        this.post({
          type: "mcpCatalog",
          catalog: {
            version: 1,
            entries,
            nextCursor: remote.nextCursor,
            source,
            search,
            prefetching: Boolean(remote.nextCursor),
            loadedCount: entries.length,
          } satisfies McpCatalogPayload,
          replace: true,
        });
        cursor = remote.nextCursor || undefined;
        if (!cursor) {
          break;
        }
      }
    } catch (err) {
      if (seq !== this.registryFetchSeq) {
        return;
      }
      lastError = err instanceof Error ? err.message : String(err);
      if (!remoteAll.length) {
        this.post({
          type: "mcpCatalog",
          catalog: {
            version: 1,
            entries: featured,
            source: featured.length ? "local" : "registry",
            search,
            error: lastError,
            prefetching: false,
            loadedCount: featured.length,
          } satisfies McpCatalogPayload,
          replace: true,
        });
      } else {
        this.post({
          type: "mcpCatalogBusy",
          busy: false,
          error: lastError,
          prefetching: false,
        });
      }
    } finally {
      if (seq === this.registryFetchSeq) {
        this.post({
          type: "mcpCatalogBusy",
          busy: false,
          prefetching: false,
          error: lastError || undefined,
        });
      }
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
      "saveOrchestra",
      "refreshOrchModels",
      "fetchMcpRegistry",
      "openExternal",
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
      if (t === "openExternal") {
        const rawUrl = String(msg.url || "").trim();
        if (rawUrl.startsWith("https://") || rawUrl.startsWith("http://")) {
          await vscode.env.openExternal(vscode.Uri.parse(rawUrl));
        }
        return true;
      }
      if (t === "fetchMcpRegistry") {
        await this.pullMcpRegistry({
          search: String(msg.search || ""),
        });
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
            persist: Boolean(apiKey),
          });
        }
        this.post({ type: "modelsBusy", busy: true });
        const catalog = await this.session.listProviders(
          probeKey ? { probeKey, includeSecrets: true } : { probe: true, includeSecrets: true }
        );
        this.post({ type: "providerCatalog", catalog, probeKey: probeKey || undefined });
        this.post({ type: "modelsBusy", busy: false });
        return true;
      }
      if (t === "saveModels") {
        const provider = String(msg.provider || "").trim();
        const apiKey = String(msg.apiKey || "").trim();
        const model = String(msg.model || "").trim();
        await this.session.configureLLM({
          apiBase: String(msg.apiBase || "").trim() || undefined,
          apiKey: apiKey || undefined,
          model: model || undefined,
          provider: provider || undefined,
          temperature: numOrUndef(msg.temperature),
          maxTokens: posIntOrUndef(msg.maxTokens),
          timeoutS: posIntOrUndef(msg.timeoutS),
          promptFamily: msg.promptFamily !== undefined ? String(msg.promptFamily) : undefined,
          multimodal: msg.multimodal === undefined ? undefined : Boolean(msg.multimodal),
          persist: true,
        });
        const note =
          apiKey && !model
            ? "Provider credentials saved — pick a model and save again to activate"
            : "Model settings saved";
        void vscode.window.showInformationMessage(note);
        await this.pushState();
        return true;
      }
      if (t === "refreshOrchModels") {
        this.post({ type: "modelsBusy", busy: true, message: "Loading models…" });
        const catalog = await this.session.listProviders({ probe: true });
        this.post({ type: "providerCatalog", catalog });
        this.post({ type: "modelsBusy", busy: false });
        return true;
      }
      if (t === "saveOrchestra") {
        const rolesRaw = msg.roles;
        if (!Array.isArray(rolesRaw)) {
          throw new Error("roles required");
        }
        await this.session.configureOrchestra({
          roles: rolesRaw as {
            key: string;
            label: string;
            provider: string;
            model: string;
            models?: string[];
          }[],
          defaultTier: String(msg.defaultTier || "focused"),
          maxWorkerRetries: posIntOrUndef(msg.maxWorkerRetries),
          workerVerifyEnabled:
            msg.workerVerifyEnabled === undefined ? undefined : Boolean(msg.workerVerifyEnabled),
          maxWorkerVerifyRetries: posIntOrUndef(msg.maxWorkerVerifyRetries),
          workerLLMVerifyEnabled:
            msg.workerLLMVerifyEnabled === undefined ? undefined : Boolean(msg.workerLLMVerifyEnabled),
        });
        void vscode.window.showInformationMessage("Orchestra settings saved");
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
        let tools: string[] | undefined;
        if (Array.isArray(msg.tools)) {
          tools = msg.tools
            .map((x) => String(x || "").trim())
            .filter(Boolean);
        } else {
          const toolsRaw = String(msg.tools || "").trim();
          tools = toolsRaw
            ? toolsRaw.split(/[,\s]+/).map((x) => x.trim()).filter(Boolean)
            : undefined;
        }
        if (tools && tools.length === 0) {
          tools = undefined;
        }
        await this.session.upsertAgent({
          name: String(msg.name || "").trim(),
          system_prompt: String(msg.system_prompt || ""),
          tools,
          // Prompt/tools are model-independent for now — do not persist overrides from UI.
          model: undefined,
          provider: undefined,
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
        const name = String(msg.name || "").trim();
        if (!name) {
          return true;
        }
        const pick = await vscode.window.showWarningMessage(
          `Remove MCP server “${name}” from .orchestra.yml?`,
          { modal: true },
          "Remove"
        );
        if (pick !== "Remove") {
          return true;
        }
        await this.session.deleteMCP(name);
        void vscode.window.showInformationMessage(`MCP “${name}” removed`);
        await this.pushState();
        return true;
      }
      if (t === "setMCPDisabled") {
        const name = String(msg.name || "").trim();
        const disabled = Boolean(msg.disabled);
        await this.session.setMCPDisabled(name, disabled);
        void vscode.window.showInformationMessage(
          disabled ? `MCP “${name}” disabled` : `MCP “${name}” enabled`
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
