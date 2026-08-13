/**
 * Minimal "vscode" module stub so pure-logic tests can run under `node --test`
 * outside the extension host (the real module only exists inside VS Code).
 * Import this file BEFORE any module that does `import * as vscode from "vscode"`.
 */
import Module = require("module");

class Position {
  constructor(
    public readonly line: number,
    public readonly character: number
  ) {}
}

class Range {
  public readonly start: Position;
  public readonly end: Position;
  constructor(startLine: number, startCharacter: number, endLine: number, endCharacter: number) {
    this.start = new Position(startLine, startCharacter);
    this.end = new Position(endLine, endCharacter);
  }
}

class ThemeColor {
  constructor(public readonly id: string) {}
}

const stub = { Position, Range, ThemeColor };

type LoadFn = (request: string, parent: unknown, isMain: boolean) => unknown;
const loader = Module as unknown as { _load: LoadFn };
const originalLoad = loader._load;
loader._load = function (request: string, parent: unknown, isMain: boolean): unknown {
  if (request === "vscode") {
    return stub;
  }
  return originalLoad.call(this, request, parent, isMain);
};
