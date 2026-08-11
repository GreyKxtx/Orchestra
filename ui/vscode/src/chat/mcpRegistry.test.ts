import assert from "node:assert/strict";
import test from "node:test";
import { mapLocalCatalog } from "./mcpRegistry";

test("mapLocalCatalog expands workspaceRoot and marks featured", () => {
  const entries = mapLocalCatalog(
    {
      entries: [
        {
          id: "filesystem",
          name: "filesystem",
          title: "Filesystem",
          description: "files",
          category: "Local",
          command: "npx -y @modelcontextprotocol/server-filesystem ${workspaceRoot}",
          env: [],
        },
      ],
    },
    "C:/proj"
  );
  assert.equal(entries.length, 1);
  assert.ok(entries[0]?.command.includes("C:/proj"));
  assert.ok(entries[0]?.tags.includes("featured"));
  assert.equal(entries[0]?.installable, true);
});
