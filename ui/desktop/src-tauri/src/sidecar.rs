//! The core as a child process: find it, start it, read its one announce line,
//! keep the last lines of its stderr for error dialogs, and stop it cleanly.

use crate::boot::{parse_announce, sidecar_args, sidecar_candidates, Announce};
use std::collections::VecDeque;
use std::io::{BufRead, BufReader, Write};
use std::path::Path;
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

/// Ring buffer of the last `max_lines` lines.
pub struct Tail {
    max_lines: usize,
    lines: VecDeque<String>,
}

impl Tail {
    pub fn new(max_lines: usize) -> Self {
        Tail {
            max_lines,
            lines: VecDeque::with_capacity(max_lines),
        }
    }
    pub fn push(&mut self, line: String) {
        if self.max_lines == 0 {
            return;
        }
        if self.lines.len() == self.max_lines {
            self.lines.pop_front();
        }
        self.lines.push_back(line);
    }
    pub fn text(&self) -> String {
        self.lines.iter().cloned().collect::<Vec<_>>().join("\n")
    }
}

pub struct StartError {
    pub message: String,
    pub stderr_tail: String,
}

pub struct Sidecar {
    child: Child,
    stdin: Option<ChildStdin>,
    tail: Arc<Mutex<Tail>>,
}

const STDERR_TAIL_LINES: usize = 40;

/// Spawn `program args…` with piped stdio and read exactly one stdout line
/// within `timeout`. On any failure the child (if any) is killed. Public so the
/// process tests can drive it against the fake sidecar.
pub fn spawn_and_announce(
    program: &Path,
    args: &[String],
    timeout: Duration,
) -> Result<(Sidecar, Announce), StartError> {
    let mut child = Command::new(program)
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| StartError {
            message: format!("cannot start {}: {e}", program.display()),
            stderr_tail: String::new(),
        })?;

    let tail = Arc::new(Mutex::new(Tail::new(STDERR_TAIL_LINES)));
    if let Some(stderr) = child.stderr.take() {
        let tail = Arc::clone(&tail);
        thread::spawn(move || {
            for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                eprintln!("[orchestra] {line}");
                if let Ok(mut t) = tail.lock() {
                    t.push(line);
                }
            }
        });
    }

    let stdout = child.stdout.take().expect("stdout is piped");
    let (tx, rx) = mpsc::channel::<Option<String>>();
    thread::spawn(move || {
        let mut line = String::new();
        let got = BufReader::new(stdout)
            .read_line(&mut line)
            .ok()
            .filter(|n| *n > 0)
            .map(|_| line);
        let _ = tx.send(got);
    });

    let stdin = child.stdin.take();
    let sidecar = Sidecar { child, stdin, tail };

    let line = match rx.recv_timeout(timeout) {
        Ok(Some(line)) => line,
        Ok(None) => {
            return Err(fail(
                sidecar,
                "the core exited before announcing its address".to_string(),
            ))
        }
        Err(_) => {
            return Err(fail(
                sidecar,
                format!(
                    "the core did not announce its address within {}s",
                    timeout.as_secs()
                ),
            ))
        }
    };
    match parse_announce(&line) {
        Ok(a) => Ok((sidecar, a)),
        Err(e) => Err(fail(sidecar, e)),
    }
}

/// Kill the child and turn the situation into a StartError with its stderr.
fn fail(mut sidecar: Sidecar, message: String) -> StartError {
    let stderr_tail = sidecar.stderr_tail();
    let _ = sidecar.child.kill();
    let _ = sidecar.child.wait();
    StartError {
        message,
        stderr_tail,
    }
}

/// Try each candidate in order; the first that spawns wins. A candidate that
/// cannot be spawned (not found) is skipped with a note on stderr; one that
/// spawns but fails to announce is an error — that is the core misbehaving,
/// not a missing binary.
pub fn start(
    exe_dir: Option<&Path>,
    workspace: &Path,
    announce_timeout: Duration,
) -> Result<(Sidecar, Announce), StartError> {
    let args = sidecar_args(workspace);
    let candidates = sidecar_candidates(exe_dir);
    let mut last_not_found = String::new();
    for (i, program) in candidates.iter().enumerate() {
        match spawn_and_announce(program, &args, announce_timeout) {
            Ok(ok) => {
                if i > 0 {
                    eprintln!(
                        "[orchestra-desktop] no bundled core next to the app; using {} from PATH",
                        program.display()
                    );
                }
                return Ok(ok);
            }
            Err(e) if e.message.starts_with("cannot start ") => {
                last_not_found = e.message;
                continue;
            }
            Err(e) => return Err(e),
        }
    }
    Err(StartError {
        message: format!("orchestra was not found next to the app or on PATH ({last_not_found})"),
        stderr_tail: String::new(),
    })
}

impl Sidecar {
    pub fn stderr_tail(&self) -> String {
        self.tail.lock().map(|t| t.text()).unwrap_or_default()
    }

    /// Close stdin (the shutdown signal under --announce), wait up to `grace`
    /// for a clean exit, then kill.
    pub fn stop(mut self, grace: Duration) {
        drop(self.stdin.take()); // EOF: the shutdown signal under --announce
        let deadline = Instant::now() + grace;
        loop {
            match self.child.try_wait() {
                Ok(Some(_)) => return,
                Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(50)),
                _ => break,
            }
        }
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// Entry point of the fake sidecar. main() calls this first; it returns false
/// when FAKE_SIDECAR is not set (the normal case).
pub fn maybe_run_fake_sidecar() -> bool {
    let Ok(mode) = std::env::var("FAKE_SIDECAR") else {
        return false;
    };
    if !std::env::args().any(|a| a == "--fake-sidecar") {
        return false;
    }
    let out = std::io::stdout();
    match mode.as_str() {
        "announce-then-wait-eof" => {
            let mut o = out.lock();
            let _ = writeln!(
                o,
                r#"{{"url":"http://127.0.0.1:1","token":"t","pid":1,"protocol_version":15}}"#
            );
            let _ = o.flush();
            // Block until the parent closes our stdin, then exit 0.
            let mut sink = String::new();
            let _ = std::io::stdin().read_line(&mut sink);
            let stdin = std::io::stdin();
            for _ in stdin.lock().lines() {}
        }
        "stderr-then-hang" => {
            eprintln!("boom: something went wrong");
            thread::sleep(Duration::from_secs(30));
        }
        "garbage" => {
            let mut o = out.lock();
            let _ = writeln!(o, "[orchestra] web UI: http://127.0.0.1:1/?token=x");
            let _ = o.flush();
            thread::sleep(Duration::from_secs(30));
        }
        _ => {}
    }
    std::process::exit(0);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tail_keeps_only_the_last_lines_in_order() {
        let mut t = Tail::new(3);
        for l in ["a", "b", "c", "d", "e"] {
            t.push(l.to_string());
        }
        assert_eq!(t.text(), "c\nd\ne");
    }

    #[test]
    fn tail_of_zero_lines_is_empty() {
        let mut t = Tail::new(0);
        t.push("x".into());
        assert_eq!(t.text(), "");
    }
}
