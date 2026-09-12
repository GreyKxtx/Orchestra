package fs

import (
	"errors"
	iofs "io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

// maxNeighboursListed caps the "what is actually there" hint. Enough to
// orient, short enough that a wrong guess costs a line rather than a page.
const maxNeighboursListed = 12

// missingPathError turns a filesystem miss into something a model can act on.
//
// The raw OS error is the worst possible answer: it names an absolute path the
// model never wrote, says nothing about the workspace, and suggests no next
// move. A model that guessed "evalws" because that is the module name reads
// "…\scratchpad\eval\ws\evalws: The system cannot find the file specified"
// and guesses again from the fragments — "eval", "scratchpad/eval", "ws" —
// until the circuit breaker ends the turn. Naming the relative path and the
// contents of the nearest directory that does exist ends that loop in one
// step.
//
// Returns nil when err is not a not-exist error, so callers can fall through
// to their existing handling.
func missingPathError(root, relSlash string, err error) error {
	if err == nil || !errors.Is(err, iofs.ErrNotExist) {
		return nil
	}
	msg := "no such path in the workspace: " + relSlash
	if hint := nearestDirectoryHint(root, relSlash); hint != "" {
		msg += " — " + hint
	}
	return protocol.NewError(protocol.NotFound, msg, map[string]any{"path": relSlash})
}

// nearestDirectoryHint walks up from relSlash to the first directory that
// exists and describes what it holds.
func nearestDirectoryHint(root, relSlash string) string {
	dir := path.Dir(strings.Trim(relSlash, "/"))
	for i := 0; i < 16; i++ {
		if dir == "" || dir == "/" {
			dir = "."
		}
		abs := root
		if dir != "." {
			abs = filepath.Join(root, filepath.FromSlash(dir))
		}
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			entries := directoryEntries(abs)
			label := dir + "/"
			if dir == "." {
				label = "the workspace root"
			}
			if len(entries) == 0 {
				return label + " is empty; paths are relative to the workspace root"
			}
			return "paths are relative to the workspace root; " + label + " holds: " + strings.Join(entries, ", ")
		}
		if dir == "." {
			return ""
		}
		dir = path.Dir(dir)
	}
	return ""
}

// directoryEntries lists a directory's names, directories marked with a
// trailing slash, capped and sorted for a stable message.
func directoryEntries(abs string) []string {
	des, err := os.ReadDir(abs)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(des))
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if de.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxNeighboursListed {
		extra := len(names) - maxNeighboursListed
		names = append(names[:maxNeighboursListed:maxNeighboursListed], "…and "+itoa(extra)+" more")
	}
	return names
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
