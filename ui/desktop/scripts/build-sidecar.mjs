// Builds the Go core for the host and places it where the desktop shell looks.
//
//   node ui/desktop/scripts/build-sidecar.mjs [--profile debug|release]
//
// Two outputs:
//   src-tauri/binaries/orchestra-<host-triple>[.exe]  — the name Tauri's bundler
//       expects for bundle.externalBin (declared in part C).
//   src-tauri/target/<profile>/orchestra[.exe]        — next to the debug/release
//       shell executable, so `cargo run` uses the freshly built core instead of
//       falling back to PATH. Only written when that target dir exists.
//
// No dependencies; needs `go` and `rustc` on PATH.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const desktop = path.join(here, "..");
const repo = path.join(desktop, "..", "..");
const srcTauri = path.join(desktop, "src-tauri");

const profile = (() => {
  const i = process.argv.indexOf("--profile");
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : "debug";
})();
if (!["debug", "release"].includes(profile)) {
  console.error(`unknown profile ${profile}; use debug or release`);
  process.exit(2);
}

const ext = process.platform === "win32" ? ".exe" : "";
// `rustc --print host-tuple` needs Rust >= 1.84; src-tauri/Cargo.toml
// declares rust-version = "1.77", so use the `host:` line of `rustc -vV`
// instead — it has existed since long before the crate's declared MSRV and
// keeps this script usable on the oldest Rust that MSRV promises to support.
const verbose = execFileSync("rustc", ["-vV"], { encoding: "utf8" });
const hostLine = verbose.split(/\r?\n/).find((line) => line.startsWith("host:"));
const triple = hostLine ? hostLine.slice("host:".length).trim() : "";
if (!triple) {
  console.error("could not find a `host:` line in `rustc -vV` output; is Rust on PATH?");
  process.exit(1);
}

const binaries = path.join(srcTauri, "binaries");
fs.mkdirSync(binaries, { recursive: true });
const out = path.join(binaries, `orchestra-${triple}${ext}`);

execFileSync("go", ["build", "-o", out, "./cmd/orchestra"], { cwd: repo, stdio: "inherit" });
console.log(`built ${path.relative(repo, out)}`);

const targetDir = path.join(srcTauri, "target", profile);
if (fs.existsSync(targetDir)) {
  const beside = path.join(targetDir, `orchestra${ext}`);
  fs.copyFileSync(out, beside);
  console.log(`copied next to the shell: ${path.relative(repo, beside)}`);
} else {
  console.log(`no ${path.relative(repo, targetDir)} yet — run cargo build first to get the copy next to the shell`);
}
