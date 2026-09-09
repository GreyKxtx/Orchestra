//! Pure decisions of the shell: which project to open, what to spawn, what the
//! sidecar's announce line means. No I/O, no Tauri — so every branch is a unit
//! test.

use serde::Deserialize;
use std::path::{Path, PathBuf};

/// The one line `orchestra web --announce` prints: the discovery object.
#[derive(Debug, Deserialize, PartialEq)]
pub struct Announce {
    pub url: String,
    pub token: String,
    #[serde(default)]
    pub pid: i64,
    #[serde(default)]
    pub protocol_version: i64,
}

pub fn parse_announce(line: &str) -> Result<Announce, String> {
    let a: Announce = serde_json::from_str(line.trim())
        .map_err(|e| format!("announce line is not the discovery object: {e}"))?;
    if a.url.is_empty() || a.token.is_empty() {
        return Err("announce line lacks url or token".to_string());
    }
    Ok(a)
}

/// Exactly the arguments the spec fixes; a test pins them byte for byte.
pub fn sidecar_args(workspace: &Path) -> Vec<String> {
    vec![
        "web".into(),
        "--workspace-root".into(),
        workspace.to_string_lossy().into_owned(),
        "--no-open".into(),
        "--port".into(),
        "0".into(),
        "--init".into(),
        "--announce".into(),
    ]
}

/// Where a project may come from, in priority order. All optional: a missing
/// file is `None`, a present one is its raw content.
pub struct Sources<'a> {
    pub arg: Option<&'a str>,
    pub desktop_json: Option<&'a str>,
    pub projects_json: Option<&'a str>,
}

#[derive(Deserialize)]
struct DesktopJson {
    last_project: Option<String>,
}

#[derive(Deserialize)]
struct ProjectsJson {
    #[serde(default)]
    projects: Vec<String>,
}

/// argument → desktop.json last_project → each projects.json entry in order → pick().
/// Candidates that are not directories (per `is_dir`) are skipped.
pub fn resolve_project(
    src: Sources,
    is_dir: &dyn Fn(&Path) -> bool,
    pick: &mut dyn FnMut() -> Option<PathBuf>,
) -> Option<PathBuf> {
    let mut candidates: Vec<PathBuf> = Vec::new();
    if let Some(a) = src.arg {
        candidates.push(PathBuf::from(a));
    }
    if let Some(d) = src.desktop_json {
        if let Ok(DesktopJson {
            last_project: Some(p),
        }) = serde_json::from_str::<DesktopJson>(d)
        {
            candidates.push(PathBuf::from(p));
        }
    }
    if let Some(p) = src.projects_json {
        if let Ok(list) = serde_json::from_str::<ProjectsJson>(p) {
            candidates.extend(list.projects.iter().map(PathBuf::from));
        }
    }
    if let Some(found) = candidates.into_iter().find(|c| is_dir(c)) {
        return Some(found);
    }
    // Last resort: ask. A cancelled dialog, or a pick that is not a directory
    // by the time we look, means there is nothing to start.
    pick().filter(|p| is_dir(p))
}

/// The content of ~/.orchestra/desktop.json after a successful start.
pub fn remember_project_json(path: &Path) -> String {
    serde_json::json!({ "last_project": path.to_string_lossy() }).to_string()
}

/// Where to look for the core: next to our executable first (that is where
/// every Tauri bundler puts an externalBin), then whatever `orchestra` PATH
/// resolves — the development fallback.
pub fn sidecar_candidates(exe_dir: Option<&Path>) -> Vec<PathBuf> {
    let name = if cfg!(windows) {
        "orchestra.exe"
    } else {
        "orchestra"
    };
    let mut out = Vec::new();
    if let Some(dir) = exe_dir {
        out.push(dir.join(name));
    }
    out.push(PathBuf::from(name));
    out
}

/// `std::fs::canonicalize` returns a `\\?\`-prefixed "verbatim" path on
/// Windows. That is always a valid Windows path, but it confuses the core's
/// sqlite `file:` URI opener (SQLITE_CANTOPEN) — so simplify it back to an
/// ordinary path when that is safe to do, the same judgment
/// `dunce::simplified` makes: skip simplification when a legacy
/// (non-verbatim) path cannot represent the same file — a component ending
/// in `.` or ` ` (Windows silently drops those without the verbatim prefix,
/// which would change which file the path names) or a result long enough
/// that it may genuinely need verbatim addressing (over the legacy
/// MAX_PATH, 260 chars). In those rare cases the sqlite-URI incompatibility
/// this exists to fix is left unresolved rather than risk handing
/// `sidecar::start` a path that resolves differently, or not at all. A
/// no-op on every non-Windows target.
pub fn simplify_canonical_path(p: PathBuf) -> PathBuf {
    if !cfg!(windows) {
        return p;
    }
    let s = p.to_string_lossy();
    let rest = if let Some(rest) = s.strip_prefix(r"\\?\UNC\") {
        format!(r"\\{rest}")
    } else if let Some(rest) = s.strip_prefix(r"\\?\") {
        rest.to_string()
    } else {
        return p; // not a verbatim path; nothing to simplify
    };
    let safe = rest.len() <= 260
        && rest
            .split('\\')
            .all(|part| !part.ends_with('.') && !part.ends_with(' '));
    if safe {
        PathBuf::from(rest)
    } else {
        p
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn dir_set(dirs: &[&str]) -> impl Fn(&Path) -> bool {
        let owned: Vec<PathBuf> = dirs.iter().map(PathBuf::from).collect();
        move |p: &Path| owned.iter().any(|d| d == p)
    }

    #[test]
    fn announce_is_the_discovery_object() {
        let line = r#"{"protocol_version":15,"workspace_root":"C:\\w","url":"http://127.0.0.1:5123","port":5123,"token":"abc","pid":42,"started_at_unix":1,"written_at_unix":1}"#;
        let a = parse_announce(line).unwrap();
        assert_eq!(a.url, "http://127.0.0.1:5123");
        assert_eq!(a.token, "abc");
        assert_eq!(a.pid, 42);
        assert_eq!(a.protocol_version, 15);
    }

    #[test]
    fn announce_rejects_a_log_line_and_a_missing_token() {
        assert!(parse_announce("[orchestra] web UI: http://127.0.0.1:1/?token=x").is_err());
        assert!(
            parse_announce(r#"{"url":"http://127.0.0.1:1"}"#).is_err(),
            "no token"
        );
        assert!(
            parse_announce(r#"{"url":"http://127.0.0.1:1","token":""}"#).is_err(),
            "empty token"
        );
        assert!(parse_announce("").is_err());
    }

    #[test]
    fn sidecar_args_are_the_contract() {
        let args = sidecar_args(Path::new("C:\\repo"));
        assert_eq!(
            args,
            vec![
                "web",
                "--workspace-root",
                "C:\\repo",
                "--no-open",
                "--port",
                "0",
                "--init",
                "--announce"
            ]
        );
    }

    #[test]
    fn argument_beats_memory_beats_dialog() {
        let is_dir = dir_set(&["A", "B", "C"]);
        let mut picked = false;
        let mut pick = || {
            picked = true;
            Some(PathBuf::from("D"))
        };
        let got = resolve_project(
            Sources {
                arg: Some("A"),
                desktop_json: Some(r#"{"last_project":"B"}"#),
                projects_json: Some(r#"{"projects":["C"]}"#),
            },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("A")));
        assert!(
            !picked,
            "the dialog must not open when an argument is given"
        );
    }

    #[test]
    fn last_project_beats_the_registry_list() {
        let is_dir = dir_set(&["B", "C"]);
        let mut pick = || None;
        let got = resolve_project(
            Sources {
                arg: None,
                desktop_json: Some(r#"{"last_project":"B"}"#),
                projects_json: Some(r#"{"projects":["C"]}"#),
            },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("B")));
    }

    #[test]
    fn vanished_directories_and_corrupt_files_fall_through() {
        let is_dir = dir_set(&["C"]);
        let mut pick = || None;
        let got = resolve_project(
            Sources {
                arg: Some("gone"),
                desktop_json: Some("{not json"),
                projects_json: Some(r#"{"projects":["also-gone","C"]}"#),
            },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("C")));
    }

    #[test]
    fn the_dialog_is_last_and_its_cancel_is_none() {
        let is_dir = dir_set(&["D"]);
        let mut calls = 0;
        let mut pick = || {
            calls += 1;
            Some(PathBuf::from("D"))
        };
        let got = resolve_project(
            Sources {
                arg: None,
                desktop_json: None,
                projects_json: None,
            },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("D")));
        assert_eq!(calls, 1);

        let mut cancel = || None;
        let none = resolve_project(
            Sources {
                arg: None,
                desktop_json: None,
                projects_json: None,
            },
            &is_dir,
            &mut cancel,
        );
        assert_eq!(none, None);
    }

    #[test]
    fn a_picked_non_directory_is_none_too() {
        // The dialog returned something that is not a directory (deleted
        // between pick and use): do not start a server on it.
        let is_dir = dir_set(&[]);
        let mut pick = || Some(PathBuf::from("ghost"));
        assert_eq!(
            resolve_project(
                Sources {
                    arg: None,
                    desktop_json: None,
                    projects_json: None
                },
                &is_dir,
                &mut pick
            ),
            None
        );
    }

    #[test]
    fn remembered_json_round_trips_through_resolve() {
        let json = remember_project_json(Path::new("C:\\my repo"));
        let is_dir = dir_set(&["C:\\my repo"]);
        let mut pick = || None;
        let got = resolve_project(
            Sources {
                arg: None,
                desktop_json: Some(&json),
                projects_json: None,
            },
            &is_dir,
            &mut pick,
        );
        assert_eq!(got, Some(PathBuf::from("C:\\my repo")));
    }

    #[test]
    fn candidates_are_next_to_exe_then_path() {
        let c = sidecar_candidates(Some(Path::new("C:\\app")));
        let exe = if cfg!(windows) {
            "orchestra.exe"
        } else {
            "orchestra"
        };
        assert_eq!(
            c,
            vec![PathBuf::from("C:\\app").join(exe), PathBuf::from(exe)]
        );
        assert_eq!(sidecar_candidates(None), vec![PathBuf::from(exe)]);
    }

    #[test]
    fn a_verbatim_drive_path_is_simplified_on_windows() {
        let got = simplify_canonical_path(PathBuf::from(r"\\?\C:\Users\a\proj"));
        if cfg!(windows) {
            assert_eq!(got, PathBuf::from(r"C:\Users\a\proj"));
        } else {
            // `\\?\` is Windows-only syntax; elsewhere this is a no-op.
            assert_eq!(got, PathBuf::from(r"\\?\C:\Users\a\proj"));
        }
    }

    #[test]
    fn a_verbatim_unc_path_is_simplified_on_windows() {
        let got = simplify_canonical_path(PathBuf::from(r"\\?\UNC\server\share\dir"));
        if cfg!(windows) {
            assert_eq!(got, PathBuf::from(r"\\server\share\dir"));
        } else {
            assert_eq!(got, PathBuf::from(r"\\?\UNC\server\share\dir"));
        }
    }

    #[test]
    fn an_already_simple_path_is_unchanged() {
        let p = PathBuf::from(r"C:\Users\a\proj");
        assert_eq!(simplify_canonical_path(p.clone()), p);
    }

    #[test]
    fn an_empty_path_is_unchanged() {
        let p = PathBuf::new();
        assert_eq!(simplify_canonical_path(p.clone()), p);
    }

    #[test]
    fn a_trailing_dot_or_space_component_blocks_simplification() {
        // Windows silently drops a trailing `.` or ` ` from a path component
        // when the path is opened NOT in verbatim mode, which would change
        // which file the path names. Stripping the `\\?\` prefix here would
        // therefore be unsafe, so this is deliberately left untouched (still
        // verbatim, still exactly the file `canonicalize` resolved) —
        // matching `dunce::simplified`'s own rule, at the cost of leaving the
        // sqlite-URI incompatibility this function exists to fix unresolved
        // for this rare case.
        let p = PathBuf::from(r"\\?\C:\Users\a\trailing. \proj");
        assert_eq!(simplify_canonical_path(p.clone()), p);
    }
}
