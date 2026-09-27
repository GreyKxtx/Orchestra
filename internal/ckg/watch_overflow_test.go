package ckg

import "github.com/fsnotify/fsnotify"

// errOverflowForTest is the error the OS reports when it dropped
// notifications, as the watcher's Errors channel would carry it.
func errOverflowForTest() error { return fsnotify.ErrEventOverflow }
