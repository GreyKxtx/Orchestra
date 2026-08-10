import * as fs from "fs";
import * as path from "path";

/** VSIX layout: bin/<platform>-<arch>/orchestra[.exe] */
export function corePlatformArch(): string {
  return `${process.platform}-${process.arch}`;
}

export function coreExecutableName(platform: NodeJS.Platform = process.platform): string {
  return platform === "win32" ? "orchestra.exe" : "orchestra";
}

export function bundledCorePath(extensionPath: string): string {
  const exeName = coreExecutableName();
  return path.join(extensionPath, "bin", corePlatformArch(), exeName);
}

/** Candidate paths in priority order (dedupe happens in resolveBinaryPath). */
export function coreBinaryCandidates(workspaceRoot: string, extensionPath: string): string[] {
  const exeName = coreExecutableName();
  const out: string[] = [
    bundledCorePath(extensionPath),
    path.join(extensionPath, exeName),
    path.join(extensionPath, "..", "..", exeName),
    path.join(extensionPath, "..", exeName),
    path.join(workspaceRoot, exeName),
  ];
  if (process.platform === "win32") {
    out.push(
      path.join(workspaceRoot, "orchestra-new.exe"),
      path.join(extensionPath, "..", "..", "orchestra-new.exe")
    );
  }
  let dir = workspaceRoot;
  for (let i = 0; i < 6; i++) {
    out.push(path.join(dir, exeName));
    const parent = path.dirname(dir);
    if (parent === dir) {
      break;
    }
    dir = parent;
  }
  return out;
}

export function pickExistingBinary(candidates: string[]): string {
  const seen = new Set<string>();
  let best = "";
  let bestMtime = 0;
  for (const c of candidates) {
    const abs = path.resolve(c);
    if (seen.has(abs)) {
      continue;
    }
    seen.add(abs);
    try {
      const st = fs.statSync(abs);
      if (st.isFile() && st.mtimeMs >= bestMtime) {
        best = abs;
        bestMtime = st.mtimeMs;
      }
    } catch {
      // missing
    }
  }
  return best;
}
