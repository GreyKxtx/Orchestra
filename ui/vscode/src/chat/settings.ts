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
 * The panel's markup, shared with the web UI, which wraps the same file in its
 * own shell (ui/web/scripts/bundle-settings-web.mjs). Kept out of this file so
 * the two hosts cannot drift; the leading comment is stripped so what this
 * emits is unchanged.
 */
function loadSettingsBody(extensionUri: vscode.Uri): string {
  const file = path.join(extensionUri.fsPath, "media", "settings-body.html");
  return fs
    .readFileSync(file, "utf8")
    .replace(/^<!--[\s\S]*?-->\s*/, "")
    .trimEnd();
}

/**
 * Settings UI hosted inside the Orchestra chat panel (same webview, swap HTML).
 * Tabs: General, Providers, Index & Graph, Agent, Tools & MCP.
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

    const settingsBody = loadSettingsBody(this.extensionUri);
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
  ${settingsBody}
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

  /** Persists orchestra roles/verification settings from a webview message. */
  private async saveOrchestraFromMessage(msg: { [k: string]: unknown }): Promise<void> {
    await this.session.configureOrchestra({
      roles: msg.roles as {
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
    await this.warnMissingRoleKeys();
  }

  /**
   * Post-save sanity check: a role provider without an API key makes every
   * tier request die at the gateway with an opaque 401 — surface it now.
   */
  private async warnMissingRoleKeys(): Promise<void> {
    try {
      const orch = await this.session.getOrchestra();
      const missing = new Set<string>();
      for (const role of orch.roles) {
        const prov = (role.provider || "").trim();
        if (!prov) {
          continue;
        }
        const info = orch.named[prov];
        if (info && info.needsKey && !info.apiKeySet) {
          missing.add(prov);
        }
      }
      if (missing.size > 0) {
        void vscode.window.showWarningMessage(
          `Orchestra: no API key for ${[...missing].join(", ")} — add it on the Providers tab, otherwise requests will fail (401).`
        );
      }
    } catch {
      // Advisory only — never block the save flow.
    }
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
        // The General tab hosts the Orchestra routing section too — persist the
        // roles in the same click so one Save covers the whole tab.
        if (Array.isArray(msg.roles)) {
          await this.saveOrchestraFromMessage(msg);
        }
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
        if (!Array.isArray(msg.roles)) {
          throw new Error("roles required");
        }
        await this.saveOrchestraFromMessage(msg);
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

