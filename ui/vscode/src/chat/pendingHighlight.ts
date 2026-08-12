import * as vscode from "vscode";
import { buildHighlightPlan } from "./changeHighlights";

/** Applies Cursor-style inline change highlights on a workspace editor tab. */
export class PendingHighlightManager implements vscode.Disposable {
  private addedLine?: vscode.TextEditorDecorationType;
  private changedLine?: vscode.TextEditorDecorationType;
  private ghostLine?: vscode.TextEditorDecorationType;
  private disposables: vscode.Disposable[] = [];
  private changeListener: vscode.Disposable | undefined;
  /** Snapshot the plan was built against; any edit invalidates the decorations. */
  private tracked: { uriKey: string; version: number; editor: vscode.TextEditor } | undefined;

  apply(editor: vscode.TextEditor, before: string, after: string): void {
    this.clearEditor(editor);
    this.ensureTypes();
    const plan = buildHighlightPlan(editor.document, before, after);

    if (plan.addedRanges.length > 0) {
      editor.setDecorations(
        this.addedLine!,
        plan.addedRanges.map((range) => ({ range }))
      );
    }
    if (plan.changedRanges.length > 0) {
      editor.setDecorations(
        this.changedLine!,
        plan.changedRanges.map((range) => ({ range }))
      );
    }
    if (plan.ghostDecorations.length > 0 && this.ghostLine) {
      editor.setDecorations(this.ghostLine, plan.ghostDecorations);
    }

    // The plan is a snapshot of this document version. If the user edits the
    // file, line anchors drift and ghost insertions point at the wrong lines —
    // clearing beats highlighting the wrong code.
    this.tracked = {
      uriKey: editor.document.uri.toString(),
      version: editor.document.version,
      editor,
    };
    this.ensureChangeListener();

    const line = Math.min(plan.revealLine, Math.max(0, editor.document.lineCount - 1));
    const pos = new vscode.Position(line, 0);
    editor.revealRange(new vscode.Range(pos, pos), vscode.TextEditorRevealType.InCenter);
  }

  clearEditor(editor: vscode.TextEditor): void {
    if (this.addedLine) {
      editor.setDecorations(this.addedLine, []);
    }
    if (this.changedLine) {
      editor.setDecorations(this.changedLine, []);
    }
    if (this.ghostLine) {
      editor.setDecorations(this.ghostLine, []);
    }
    if (this.tracked?.editor === editor) {
      this.tracked = undefined;
    }
  }

  /** Clear highlights everywhere (e.g. pending changes applied or discarded). */
  clearAll(): void {
    if (this.tracked) {
      this.clearEditor(this.tracked.editor);
    }
    this.tracked = undefined;
  }

  dispose(): void {
    this.changeListener?.dispose();
    this.changeListener = undefined;
    for (const d of this.disposables) {
      d.dispose();
    }
    this.disposables = [];
    this.addedLine = undefined;
    this.changedLine = undefined;
    this.ghostLine = undefined;
    this.tracked = undefined;
  }

  private ensureChangeListener(): void {
    if (this.changeListener) {
      return;
    }
    this.changeListener = vscode.workspace.onDidChangeTextDocument((e) => {
      const t = this.tracked;
      if (
        t &&
        e.document.uri.toString() === t.uriKey &&
        e.document.version !== t.version
      ) {
        this.clearEditor(t.editor);
      }
    });
  }

  private ensureTypes(): void {
    if (this.addedLine) {
      return;
    }
    this.addedLine = vscode.window.createTextEditorDecorationType({
      isWholeLine: true,
      backgroundColor: new vscode.ThemeColor("diffEditor.insertedLineBackground"),
      overviewRulerColor: new vscode.ThemeColor("editorOverviewRuler.addedForeground"),
      overviewRulerLane: vscode.OverviewRulerLane.Full,
    });
    this.changedLine = vscode.window.createTextEditorDecorationType({
      isWholeLine: true,
      backgroundColor: new vscode.ThemeColor("diffEditor.removedLineBackground"),
      overviewRulerColor: new vscode.ThemeColor("editorOverviewRuler.deletedForeground"),
      overviewRulerLane: vscode.OverviewRulerLane.Full,
    });
    this.ghostLine = vscode.window.createTextEditorDecorationType({});
    this.disposables.push(this.addedLine, this.changedLine, this.ghostLine);
  }
}
