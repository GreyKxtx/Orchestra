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
