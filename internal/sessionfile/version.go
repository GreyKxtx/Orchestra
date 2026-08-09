package sessionfile

// Version is the on-disk session schema version (unified TUI + core).
// v3 adds chronological UIMessage.segments (assistant parts).
// v4 adds UIMessage.attachments for chat file/image refs.
const Version = 4
