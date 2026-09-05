import assert from "node:assert/strict";
import test from "node:test";
import { isBuiltinSlashCommand, matchSkillCommand, skillSlashNames } from "./skillCommands";

test("isBuiltinSlashCommand recognises panel.ts's built-ins and nothing else", () => {
  assert.equal(isBuiltinSlashCommand("help"), true);
  assert.equal(isBuiltinSlashCommand("clear"), true);
  assert.equal(isBuiltinSlashCommand("refactor-go"), false);
});

test("skillSlashNames drops built-in collisions and blank names", () => {
  const names = skillSlashNames([
    { name: "refactor-go" },
    { name: "help" },
    { name: "" },
    { name: "  " },
  ]);
  assert.deepEqual(names, ["refactor-go"]);
});

test("matchSkillCommand matches a known skill with non-empty arguments", () => {
  assert.equal(matchSkillCommand("/refactor-go", "clean up foo.ts", ["refactor-go"]), "refactor-go");
});

test("matchSkillCommand rejects a bare command with no arguments", () => {
  assert.equal(matchSkillCommand("/refactor-go", "", ["refactor-go"]), null);
  assert.equal(matchSkillCommand("/refactor-go", undefined, ["refactor-go"]), null);
  assert.equal(matchSkillCommand("/refactor-go", "   ", ["refactor-go"]), null);
});

test("matchSkillCommand rejects a name that is not a loaded skill", () => {
  assert.equal(matchSkillCommand("/not-a-skill", "hello", ["refactor-go"]), null);
});
