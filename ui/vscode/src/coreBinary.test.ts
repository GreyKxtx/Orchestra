import { strict as assert } from "node:assert";
import * as path from "node:path";
import { describe, it } from "node:test";
import {
  bundledCorePath,
  coreBinaryCandidates,
  coreExecutableName,
  corePlatformArch,
} from "./coreBinary";

describe("coreBinary", () => {
  it("bundled path uses platform-arch layout", () => {
    const ext = path.join("/", "ext");
    assert.equal(
      bundledCorePath(ext),
      path.join(ext, "bin", corePlatformArch(), coreExecutableName())
    );
  });

  it("prefers bundled path before workspace in candidate order", () => {
    const c = coreBinaryCandidates(path.join("/", "ws"), path.join("/", "ext"));
    assert.ok(c[0].includes(path.join("ext", "bin")));
    assert.ok(c.some((p) => p.includes(`${path.sep}ws${path.sep}`) || p.endsWith(`${path.sep}ws`)));
  });

  it("exe name on win32", () => {
    assert.equal(coreExecutableName("win32"), "orchestra.exe");
    assert.equal(coreExecutableName("linux"), "orchestra");
  });
});
