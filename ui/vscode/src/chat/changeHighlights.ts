import * as vscode from "vscode";

export type DiffRow =
  | { type: "same"; left: string; right: string; leftNum: number; rightNum: number }
  | { type: "add"; right: string; rightNum: number }
  | { type: "del"; left: string; leftNum: number };

export function alignDiffLines(before: string, after: string): DiffRow[] {
  const a = normalizeEol(before || "").split("\n");
  const b = normalizeEol(after || "").split("\n");
  const n = a.length;
  const m = b.length;
  const dp = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && a[i] === b[j]) {
      rows.push({ type: "same", left: a[i], right: b[j], leftNum: i + 1, rightNum: j + 1 });
      i++;
      j++;
    } else if (j < m && (i >= n || dp[i][j + 1] >= dp[i + 1][j])) {
      rows.push({ type: "add", right: b[j], rightNum: j + 1 });
      j++;
    } else if (i < n) {
      rows.push({ type: "del", left: a[i], leftNum: i + 1 });
      i++;
    }
  }
  return rows;
}

export interface HighlightPlan {
  /** Green whole-line highlights (applied content). */
  addedRanges: vscode.Range[];
  /** Amber whole-line highlights (lines that will change / were removed). */
  changedRanges: vscode.Range[];
  /** Ghost inserted text when the file on disk is still the pre-apply version. */
  ghostDecorations: vscode.DecorationOptions[];
  revealLine: number;
  showsAfterContent: boolean;
}

export function buildHighlightPlan(
  document: vscode.TextDocument,
  before: string,
  after: string
): HighlightPlan {
  const rows = alignDiffLines(before, after);
  const docText = document.getText();
  const showsAfterContent = textsEqual(docText, after);
  const showsBeforeContent = textsEqual(docText, before);

  const addedRanges: vscode.Range[] = [];
  const changedRanges: vscode.Range[] = [];
  const ghostDecorations: vscode.DecorationOptions[] = [];
  let revealLine = 0;

  if (showsAfterContent) {
    for (const row of rows) {
      if (row.type === "add") {
        addedRanges.push(lineRange(document, row.rightNum - 1));
        if (revealLine === 0 || row.rightNum - 1 < revealLine) {
          revealLine = row.rightNum - 1;
        }
      }
    }
    return {
      addedRanges: mergeWholeLineRanges(addedRanges),
      changedRanges,
      ghostDecorations,
      revealLine,
      showsAfterContent: true,
    };
  }

  // Pending (or stale before): highlight removed/changed lines + ghost insertions.
  let pendingAdds: string[] = [];
  let anchorLine = 0; // 0-based line in before doc for ghost anchor

  const flushGhost = (anchor0: number) => {
    if (pendingAdds.length === 0) {
      return;
    }
    const line = Math.min(Math.max(anchor0, 0), Math.max(0, document.lineCount - 1));
    const lineText = document.lineAt(line).text;
    ghostDecorations.push({
      range: new vscode.Range(line, lineText.length, line, lineText.length),
      renderOptions: {
        after: {
          contentText: `\n${pendingAdds.join("\n")}`,
          color: new vscode.ThemeColor("diffEditor.insertedTextBackground"),
          backgroundColor: new vscode.ThemeColor("diffEditor.insertedLineBackground"),
          fontStyle: "italic",
          margin: "0 0 0 0",
        },
      },
    });
    if (revealLine === 0 || line < revealLine) {
      revealLine = line;
    }
    pendingAdds = [];
  };

  let lastSameLeft = 0;
  for (const row of rows) {
    switch (row.type) {
      case "same":
        flushGhost(lastSameLeft);
        lastSameLeft = row.leftNum - 1;
        anchorLine = row.leftNum - 1;
        break;
      case "del":
        flushGhost(anchorLine);
        if (showsBeforeContent || !showsAfterContent) {
          changedRanges.push(lineRange(document, row.leftNum - 1));
          if (revealLine === 0 || row.leftNum - 1 < revealLine) {
            revealLine = row.leftNum - 1;
          }
        }
        anchorLine = row.leftNum - 1;
        lastSameLeft = row.leftNum - 1;
        break;
      case "add":
        pendingAdds.push(row.right);
        break;
    }
  }
  flushGhost(lastSameLeft);

  if (revealLine === 0 && document.lineCount > 0) {
    revealLine = Math.max(0, document.lineCount - 1);
  }

  return {
    addedRanges: mergeWholeLineRanges(addedRanges),
    changedRanges: mergeWholeLineRanges(changedRanges),
    ghostDecorations,
    revealLine,
    showsAfterContent: false,
  };
}

function lineRange(document: vscode.TextDocument, line: number): vscode.Range {
  const safe = Math.min(Math.max(line, 0), Math.max(0, document.lineCount - 1));
  return new vscode.Range(safe, 0, safe, Number.MAX_SAFE_INTEGER);
}

function mergeWholeLineRanges(ranges: vscode.Range[]): vscode.Range[] {
  if (ranges.length === 0) {
    return [];
  }
  const sorted = [...ranges].sort((a, b) => a.start.line - b.start.line);
  const out: vscode.Range[] = [];
  let cur = sorted[0];
  for (let i = 1; i < sorted.length; i++) {
    const next = sorted[i];
    if (next.start.line <= cur.end.line + 1) {
      cur = new vscode.Range(cur.start.line, 0, Math.max(cur.end.line, next.end.line), Number.MAX_SAFE_INTEGER);
    } else {
      out.push(cur);
      cur = next;
    }
  }
  out.push(cur);
  return out;
}

function normalizeEol(s: string): string {
  return s.replace(/\r\n/g, "\n");
}

function textsEqual(a: string, b: string): boolean {
  return normalizeEol(a).trimEnd() === normalizeEol(b).trimEnd();
}
