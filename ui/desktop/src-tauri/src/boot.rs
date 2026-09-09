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

/// argument → desktop.json last_project → first projects.json entry → pick().
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
}
