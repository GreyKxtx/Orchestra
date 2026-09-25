package sessionfile

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID returns a sortable, unique session id like "20260805T150405-7f3a".
func NewID() string {
	ts := time.Now().UTC().Format("20060102T150405")
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%04x", ts, time.Now().UnixNano()&0xffff)
	}
	return ts + "-" + hex.EncodeToString(b[:])
}

// ValidID reports whether id can name session files. An id is joined into
// .orchestra/sessions/<id>.json, the trajectory sidecar, the turn lock and the
// session memory file, and a client may choose it (session.start reopens an id
// it names), so one with a separator or "..", like "../../x/package", would
// read, write or delete outside the sessions directory. Ids NewID mints and
// the ones people type (letters, digits, '.', '_', '-') all pass.
func ValidID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case (r == '.' || r == '_' || r == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

// CheckID returns an error for an id ValidID refuses.
func CheckID(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("sessionfile: invalid session id %q (letters, digits, '.', '_' and '-' only)", id)
	}
	return nil
}
