import * as vscode from "vscode";
import { ChatPanel } from "./chat/panel";
import { CoreSession } from "./coreSession";

export function activate(context: vscode.ExtensionContext): void {
  const output = vscode.window.createOutputChannel("Orchestra");
  const session = new CoreSession(output, context.extensionPath, context);
  const chat = new ChatPanel(session, context.extensionUri);

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

  output.appendLine(
    "Orchestra extension activated. Commands: Open Chat · Settings · Ping Core"
  );
}

export function deactivate(): void {
  // disposables handle cleanup
}
