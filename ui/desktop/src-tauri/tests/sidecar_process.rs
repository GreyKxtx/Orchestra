//! Process behaviour of the sidecar module, driven against the real
//! orchestra-desktop binary running as a fake core (see
//! sidecar::maybe_run_fake_sidecar). Tests that touch the process environment
//! serialise on FAKE_LOCK.

use orchestra_desktop::sidecar::{spawn_and_announce, start};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, Instant};

static FAKE_LOCK: Mutex<()> = Mutex::new(());

fn fake(mode: &str) -> (PathBuf, Vec<String>) {
    std::env::set_var("FAKE_SIDECAR", mode);
    (
        PathBuf::from(env!("CARGO_BIN_EXE_orchestra-desktop")),
        vec!["--fake-sidecar".into()],
    )
}

#[test]
fn announce_is_read_and_stop_closes_cleanly() {
    let _g = FAKE_LOCK.lock().unwrap();
    let (exe, args) = fake("announce-then-wait-eof");
    let (sc, a) = spawn_and_announce(&exe, &args, Duration::from_secs(10))
        .map_err(|e| e.message)
        .unwrap();
    assert_eq!(a.url, "http://127.0.0.1:1");
    assert_eq!(a.token, "t");
    let started = Instant::now();
    sc.stop(Duration::from_secs(5));
    assert!(
        started.elapsed() < Duration::from_secs(4),
        "stop waited for the kill instead of the EOF exit"
    );
}

#[test]
fn a_child_that_never_announces_is_a_timeout_with_its_stderr() {
    let _g = FAKE_LOCK.lock().unwrap();
    let (exe, args) = fake("stderr-then-hang");
    let err = spawn_and_announce(&exe, &args, Duration::from_millis(800))
        .err()
        .expect("must time out");
    assert!(
        err.message.contains("15") || err.message.contains("announce"),
        "message: {}",
        err.message
    );
    assert!(
        err.stderr_tail.contains("boom"),
        "stderr tail was not captured: {:?}",
        err.stderr_tail
    );
}

#[test]
fn a_child_that_prints_garbage_is_an_error() {
    let _g = FAKE_LOCK.lock().unwrap();
    let (exe, args) = fake("garbage");
    let err = spawn_and_announce(&exe, &args, Duration::from_secs(10))
        .err()
        .expect("must fail");
    assert!(err.message.contains("discovery object"), "{}", err.message);
}

#[test]
fn a_missing_binary_is_skipped_and_the_next_candidate_used() {
    // start() with an exe_dir that has no orchestra falls through to PATH.
    // The developer machine may well have a real orchestra on PATH, so the
    // test empties PATH for its duration (under the lock) — both candidates
    // must then fail, and the error must say what was looked for.
    let _g = FAKE_LOCK.lock().unwrap();
    let saved = std::env::var_os("PATH");
    std::env::set_var("PATH", "");
    let empty = std::env::temp_dir().join("orchestra-desktop-empty-dir");
    let _ = std::fs::create_dir_all(&empty);
    let result = start(Some(&empty), Path::new("."), Duration::from_millis(500));
    if let Some(p) = saved {
        std::env::set_var("PATH", p);
    }
    let err = result.err().expect("no orchestra anywhere here");
    assert!(err.message.contains("orchestra"), "{}", err.message);
}
