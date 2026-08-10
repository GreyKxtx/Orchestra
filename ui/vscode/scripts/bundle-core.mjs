#!/usr/bin/env node
/**
 * Build orchestra and copy into ui/vscode/bin/<platform>-<arch>/ for VSIX packaging.
 *
 *   node scripts/bundle-core.mjs              # current OS/arch only
 *   node scripts/bundle-core.mjs --all        # all targets (cross-compile; may fail for cgo)
 *   node scripts/bundle-core.mjs --target win32-x64
 */
import { execSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const vscodeDir = path.join(__dirname, "..");
const repoRoot = path.join(vscodeDir, "..", "..");

/** Must match coreBinary.ts and .github/workflows/vscode-vsix.yml */
const TARGETS = [
  { goos: "windows", goarch: "amd64", platform: "win32", arch: "x64", cc: "" },
  { goos: "linux", goarch: "amd64", platform: "linux", arch: "x64", cc: "" },
  { goos: "darwin", goarch: "arm64", platform: "darwin", arch: "arm64", cc: "" },
  {
    goos: "darwin",
    goarch: "amd64",
    platform: "darwin",
    arch: "x64",
    cc: "clang -arch x86_64 -mmacosx-version-min=11.0",
  },
];

function exeName(platform) {
  return platform === "win32" ? "orchestra.exe" : "orchestra";
}

function nodeGoos() {
  return ({ win32: "windows", darwin: "darwin", linux: "linux" })[process.platform] ?? process.platform;
}

function nodeGoarch() {
  return ({ x64: "amd64", arm64: "arm64", ia32: "386" })[process.arch] ?? process.arch;
}

function targetKey(t) {
  return `${t.platform}-${t.arch}`;
}

function parseArgs(argv) {
  const all = argv.includes("--all");
  let target = "";
  const ti = argv.indexOf("--target");
  if (ti >= 0 && argv[ti + 1]) {
    target = argv[ti + 1];
  }
  for (const a of argv) {
    if (a.startsWith("--target=")) {
      target = a.slice("--target=".length);
    }
  }
  return { all, target };
}

function bundleOne({ goos, goarch, platform, arch, cc }) {
  const dir = path.join(vscodeDir, "bin", `${platform}-${arch}`);
  const out = path.join(dir, exeName(platform));
  fs.mkdirSync(dir, { recursive: true });
  const env = { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "1" };
  if (cc) {
    env.CC = cc;
  }
  const cross = goos !== nodeGoos() || goarch !== nodeGoarch();
  if (cross) {
    console.warn(
      `Cross-build ${goos}/${goarch}: needs cgo — use CI matrix (.github/workflows/vscode-vsix.yml) for reliable multi-platform builds.`
    );
  }
  console.log(`→ go build ${goos}/${goarch} → ${path.relative(vscodeDir, out)}`);
  execSync(`go build -trimpath -ldflags="-s -w" -o "${out}" ./cmd/orchestra`, {
    cwd: repoRoot,
    stdio: "inherit",
    env,
  });
  return targetKey({ platform, arch });
}

const { all, target } = parseArgs(process.argv);

let selected = [];
if (target) {
  const t = TARGETS.find((x) => targetKey(x) === target);
  if (!t) {
    console.error(`Unknown --target ${target}. Valid: ${TARGETS.map(targetKey).join(", ")}`);
    process.exit(1);
  }
  selected = [t];
} else if (all) {
  selected = TARGETS;
} else {
  const local = TARGETS.find((t) => t.platform === process.platform && t.arch === process.arch);
  if (!local) {
    console.error(
      `No default target for ${process.platform}-${process.arch}. Use --target or --all.`
    );
    process.exit(1);
  }
  selected = [local];
}

const ok = [];
const failed = [];
for (const t of selected) {
  try {
    ok.push(bundleOne(t));
  } catch (err) {
    failed.push(targetKey(t));
    console.error(`✗ ${targetKey(t)} failed`);
    if (!all) {
      throw err;
    }
  }
}

console.log("");
if (ok.length) {
  console.log(`Built: ${ok.join(", ")}`);
}
if (failed.length) {
  console.error(`Failed: ${failed.join(", ")}`);
  console.error("For linux/macOS on Windows → push repo and run GitHub Actions workflow vscode-vsix.");
}
if (failed.length && !ok.length) {
  process.exit(1);
}
if (failed.length) {
  process.exit(2);
}

console.log("Done. Package with: npm run package");
