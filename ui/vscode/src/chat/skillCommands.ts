/**
 * Turns a loaded skill into its own "/<name>" slash command, the same way
 * the TUI does. Kept as pure logic (no vscode API) so it can be unit
 * tested directly, unlike panel.ts's message-dispatch glue.
 */

/** Slash commands panel.ts's handleSlashCommand already owns. A skill
 * sharing one of these names would either sit unreachable behind the
 * built-in or, worse, silently steal that built-in's input — so it is
 * left out of the discovered command list entirely. */
export const BUILTIN_SLASH_COMMANDS: readonly string[] = [
  "clear",
  "compact",
  "sessions",
  "model",
  "settings",
  "help",
  "rewind",
];

export function isBuiltinSlashCommand(name: string): boolean {
  return BUILTIN_SLASH_COMMANDS.includes(name);
}

/** Names usable as their own "/<name>" command, from a skill.list result. */
export function skillSlashNames(skills: ReadonlyArray<{ name: string }>): string[] {
  const out: string[] = [];
  for (const s of skills) {
    const name = (s.name || "").trim();
    if (name && !isBuiltinSlashCommand(name)) {
      out.push(name);
    }
  }
  return out;
}

/**
 * Matches an already-split "/<name>" command and its trailing argument text
 * against known skill names. Returns the matched name, or null.
 *
 * skill.invoke requires non-empty arguments (same as the TUI's
 * `/skill <name> <args>`), so a bare "/<name>" with nothing after it is not
 * a match — it falls through to "unknown command" instead of failing inside
 * the RPC call.
 */
export function matchSkillCommand(
  cmd: string,
  arg: string | undefined,
  skillNames: ReadonlyArray<string>
): string | null {
  const name = cmd.replace(/^\//, "").trim();
  if (!name || !skillNames.includes(name)) {
    return null;
  }
  if (!arg || arg.trim() === "") {
    return null;
  }
  return name;
}
