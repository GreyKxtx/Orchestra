//go:build windows

package applier

// syncDir is a no-op on Windows: directory metadata durability is handled
// by NTFS's journal under MoveFileEx — there's no equivalent of POSIX's
// "fsync the parent dir after rename" pattern. M10 in audit ledger.
func syncDir(_ string) error { return nil }
