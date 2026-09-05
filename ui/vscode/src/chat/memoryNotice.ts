import type { MemoryNotePayload } from "../protocol/events";

/**
 * memoryNoticeText mirrors ui/tui/app_rpc.go noticeTurnMemory: a failed
 * write is loud, with its reason — the field run's memory failures went
 * silently to stderr and were missed for nine days. A written note gets one
 * short line naming its source, so a run on a dead endpoint still shows
 * memory working through the digest fallback. A skip stays silent: most
 * turns change nothing, and saying so every time would be the grep-noise
 * problem again, in a different UI.
 */
export function memoryNoticeText(memory: MemoryNotePayload | undefined | null): string | null {
  if (!memory) {
    return null;
  }
  switch (memory.outcome) {
    case "written":
      return `Память: заметка записана в agent.md (${memorySourceLabel(memory.source)})`;
    case "failed":
      return `Память: запись не удалась — ${(memory.detail || "").trim()}`;
    default:
      return null;
  }
}

function memorySourceLabel(source: string | undefined): string {
  switch (source) {
    case "model":
      return "сводка модели";
    case "digest":
      return "из дайджеста хода";
    default:
      return source || "";
  }
}
