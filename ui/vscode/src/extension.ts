import * as fs from "fs";
import * as path from "path";
import * as vscode from "vscode";
import { ChatPanel } from "./chat/panel";
import { CoreSession } from "./coreSession";

/** Last maxLines lines of a file, or a placeholder when unreadable. */
function tailFile(filePath: string, maxLines: number): string {
  try {
    const raw = fs.readFileSync(filePath, "utf8");
    const lines = raw.split(/\r?\n/).filter((l) => l.trim() !== "");
    return lines.slice(-maxLines).join("\n") || "(empty)";
  } catch {
    return "(not found)";
  }
}

export function activate(context: vscode.ExtensionContext): void {
  const output = vscode.window.createOutputChannel("Orchestra");
  const session = new CoreSession(output, context.extensionPath, context);
  const chat = new ChatPanel(session, context.extensionUri, context.workspaceState);

  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider(
      ChatPanel.sidebarViewType,
      chat,
      { webviewOptions: { retainContextWhenHidden: true } }
    )
  );

  context.subscriptions.push(output, session, chat);

  context.subscriptions.push(
    vscode.commands.registerCommand("orchestra.showOutput", () => {
      output.show(true);
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("orchestra.pingCore", async () => {
      output.show(true);
      try {
        const result = await session.ping();
        void vscode.window.showInformationMessage(
          result.ok ? "Orchestra core: OK (see Output → Orchestra)" : "Orchestra core: failed"
        );
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        output.appendLine(`[orchestra] FAILED: ${msg}`);
        void vscode.window.showErrorMessage(`Orchestra Ping failed: ${msg}`);
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("orchestra.openChat", async () => {
      try {
        await chat.show();
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        output.appendLine(`[orchestra] Open Chat failed: ${msg}`);
        void vscode.window.showErrorMessage(`Orchestra Chat failed: ${msg}`);
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("orchestra.openSettings", async () => {
      try {
        await chat.showSettings();
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        output.appendLine(`[orchestra] Settings failed: ${msg}`);
        void vscode.window.showErrorMessage(`Orchestra Settings failed: ${msg}`);
      }
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("orchestra.collectDiagnostics", async () => {
      const ws = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || "";
      const parts: string[] = [];
      parts.push("# Orchestra diagnostics");
      parts.push(`generated: ${new Date().toISOString()}`);
      parts.push(`vscode: ${vscode.version} · platform: ${process.platform}/${process.arch} · node: ${process.version}`);
      parts.push(`extension: ${context.extension.packageJSON.version ?? "?"}`);
      parts.push(`workspace: ${ws || "(none)"}`);
      parts.push(`connection: ${session.getConnectionStatus()}`);
      try {
        const health = await session.healthInfo();
        parts.push(`core health: model=${health.model} provider=${health.provider}`);
        parts.push("core health raw: " + JSON.stringify(health.raw ?? {}, null, 2));
      } catch (err) {
        parts.push(`core health: FAILED — ${err instanceof Error ? err.message : String(err)}`);
      }
      if (ws) {
        parts.push("\n## .orchestra/llm_last_error.json");
        parts.push(tailFile(path.join(ws, ".orchestra", "llm_last_error.json"), 40));
        parts.push("\n## .orchestra/llm_log.jsonl (last 40 lines, secrets are masked at write time)");
        parts.push(tailFile(path.join(ws, ".orchestra", "llm_log.jsonl"), 40));
      }
      const doc = await vscode.workspace.openTextDocument({
        language: "markdown",
        content: parts.join("\n"),
      });
      await vscode.window.showTextDocument(doc, { preview: false });
    })
  );

  const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 90);
  statusBar.text = "$(comment-discussion) Orchestra";
  statusBar.tooltip = "Orchestra: Open Chat";
  statusBar.command = "orchestra.openChat";
  statusBar.show();
  context.subscriptions.push(statusBar);

  output.appendLine(
    "Orchestra extension activated. Commands: Open Chat · Settings · Ping Core · status bar"
  );
}

export function deactivate(): void {
  // disposables handle cleanup
}
