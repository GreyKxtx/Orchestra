package llmauth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// counterScript writes a script that prints token and appends a line to a
// counter file on every run, so TTL caching can be proven by counting
// invocations rather than by timing.
func counterScript(t *testing.T, token string) (argv []string, counter string) {
	t.Helper()
	dir := t.TempDir()
	counter = filepath.Join(dir, "runs.txt")
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "tok.bat")
		body := "@echo off\r\n>>\"" + counter + "\" echo run\r\necho " + token + "\r\n"
		if err := os.WriteFile(path, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		return []string{"cmd", "/c", path}, counter
	}
	path := filepath.Join(dir, "tok.sh")
	body := "#!/bin/sh\necho run >> \"" + counter + "\"\necho " + token + "\n"
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return []string{"sh", path}, counter
}

func runCount(t *testing.T, counter string) int {
	t.Helper()
	data, err := os.ReadFile(counter)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

func TestCommandTokenSource_ReturnsTrimmedStdout(t *testing.T) {
	argv, _ := counterScript(t, "tok-abc")
	src := newCommandTokenSource("vertex", argv, time.Minute)
	got, err := src()
	if err != nil {
		t.Fatalf("token source: %v", err)
	}
	if got != "tok-abc" {
		t.Fatalf("token = %q, want %q (trailing newline must be trimmed)", got, "tok-abc")
	}
}

func TestCommandTokenSource_CachesWithinTTL(t *testing.T) {
	argv, counter := counterScript(t, "tok-abc")
	src := newCommandTokenSource("vertex", argv, time.Minute)
	for i := 0; i < 3; i++ {
		if _, err := src(); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if n := runCount(t, counter); n != 1 {
		t.Fatalf("helper ran %d times, want 1 -- the TTL cache is not holding", n)
	}
}

func TestCommandTokenSource_ReRunsAfterTTLExpires(t *testing.T) {
	argv, counter := counterScript(t, "tok-abc")
	src := newCommandTokenSource("vertex", argv, time.Nanosecond)
	if _, err := src(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := src(); err != nil {
		t.Fatal(err)
	}
	if n := runCount(t, counter); n != 2 {
		t.Fatalf("helper ran %d times, want 2 -- an expired cache entry was reused", n)
	}
}

func TestCommandTokenSource_FailureCarriesStderr(t *testing.T) {
	dir := t.TempDir()
	var argv []string
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "fail.bat")
		if err := os.WriteFile(path, []byte("@echo off\r\n>&2 echo not logged in\r\nexit /b 1\r\n"), 0700); err != nil {
			t.Fatal(err)
		}
		argv = []string{"cmd", "/c", path}
	} else {
		path := filepath.Join(dir, "fail.sh")
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho not logged in >&2\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
		argv = []string{"sh", path}
	}
	_, err := newCommandTokenSource("vertex", argv, time.Minute)()
	if err == nil {
		t.Fatal("expected an error from a failing helper")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %q, want it to carry the helper's stderr", err.Error())
	}
}

func TestCommandTokenSource_EmptyOutputIsAnError(t *testing.T) {
	// Not counterScript(t, ""): a batch `echo` with no argument prints
	// "ECHO is on." rather than nothing, so the silent case needs its own
	// script that succeeds and writes no stdout at all.
	dir := t.TempDir()
	var argv []string
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "silent.bat")
		if err := os.WriteFile(path, []byte("@echo off\r\nexit /b 0\r\n"), 0700); err != nil {
			t.Fatal(err)
		}
		argv = []string{"cmd", "/c", path}
	} else {
		path := filepath.Join(dir, "silent.sh")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
		argv = []string{"sh", path}
	}
	_, err := newCommandTokenSource("vertex", argv, time.Minute)()
	if err == nil {
		t.Fatal("expected an error when the helper prints nothing")
	}
}

// A failing helper must not be cached: the next call has to retry, otherwise
// a transient failure would lock the provider out for the whole TTL.
func TestCommandTokenSource_DoesNotCacheAFailure(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs.txt")
	var argv []string
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "fail.bat")
		body := "@echo off\r\n>>\"" + counter + "\" echo run\r\nexit /b 1\r\n"
		if err := os.WriteFile(path, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		argv = []string{"cmd", "/c", path}
	} else {
		path := filepath.Join(dir, "fail.sh")
		body := "#!/bin/sh\necho run >> \"" + counter + "\"\nexit 1\n"
		if err := os.WriteFile(path, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		argv = []string{"sh", path}
	}
	src := newCommandTokenSource("vertex", argv, time.Hour)
	for i := 0; i < 2; i++ {
		if _, err := src(); err == nil {
			t.Fatalf("call %d: expected an error", i)
		}
	}
	if n := runCount(t, counter); n != 2 {
		t.Fatalf("helper ran %d times, want 2 -- a failure was cached", n)
	}
}
