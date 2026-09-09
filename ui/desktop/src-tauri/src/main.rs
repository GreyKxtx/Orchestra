#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::Duration;

use orchestra_desktop::{boot, sidecar};
use sidecar::Sidecar;
use tauri::{AppHandle, Manager, RunEvent, WebviewUrl, WebviewWindowBuilder};
use tauri_plugin_dialog::{DialogExt, MessageDialogKind};

/// The running core, if any. Taken (and stopped) on exit.
struct CoreSlot(Mutex<Option<Sidecar>>);

const ANNOUNCE_TIMEOUT: Duration = Duration::from_secs(15);
const STOP_GRACE: Duration = Duration::from_secs(3);

fn main() {
    if sidecar::maybe_run_fake_sidecar() {
        return;
    }
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .manage(CoreSlot(Mutex::new(None)))
        .setup(|app| {
            // Everything that may block — the folder picker, waiting for the
            // announce — runs off the main thread: the dialog plugin's blocking
            // calls dispatch to the main thread and would deadlock it.
            let handle = app.handle().clone();
            std::thread::spawn(move || boot(handle));
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building Orchestra desktop")
        .run(|app, event| {
            if let RunEvent::Exit = event {
                if let Some(core) = app
                    .state::<CoreSlot>()
                    .0
                    .lock()
                    .ok()
                    .and_then(|mut s| s.take())
                {
                    core.stop(STOP_GRACE);
                }
            }
        });
}

/// `std::fs::canonicalize` returns a `\\?\`-prefixed "verbatim" path on
/// Windows. That is a valid Windows path, but it confuses the core's sqlite
/// `file:` URI opener (SQLITE_CANTOPEN) — so strip the prefix back to an
/// ordinary path, the same simplification `tauri-plugin-fs` applies to picked
/// paths internally. A no-op on every other platform.
fn simplify_canonical_path(p: PathBuf) -> PathBuf {
    if cfg!(windows) {
        let s = p.to_string_lossy();
        if let Some(rest) = s.strip_prefix(r"\\?\UNC\") {
            return PathBuf::from(format!(r"\\{rest}"));
        }
        if let Some(rest) = s.strip_prefix(r"\\?\") {
            return PathBuf::from(rest);
        }
    }
    p
}

fn orchestra_home() -> Option<PathBuf> {
    // Same location as the Go side: %USERPROFILE% / $HOME + .orchestra.
    std::env::var_os("USERPROFILE")
        .or_else(|| std::env::var_os("HOME"))
        .map(|h| PathBuf::from(h).join(".orchestra"))
}

fn boot(app: AppHandle) {
    let arg = std::env::args().nth(1);
    let home = orchestra_home();
    let read = |name: &str| {
        home.as_ref()
            .and_then(|h| std::fs::read_to_string(h.join(name)).ok())
    };
    let desktop_json = read("desktop.json");
    let projects_json = read("projects.json");

    let is_dir = |p: &Path| p.is_dir();
    let mut pick = || {
        app.dialog()
            .file()
            .set_title("Choose a project folder")
            .blocking_pick_folder()
            .and_then(|f| f.into_path().ok())
    };
    let Some(workspace) = boot::resolve_project(
        boot::Sources {
            arg: arg.as_deref(),
            desktop_json: desktop_json.as_deref(),
            projects_json: projects_json.as_deref(),
        },
        &is_dir,
        &mut pick,
    ) else {
        app.exit(0); // nothing to show
        return;
    };
    let workspace = std::fs::canonicalize(&workspace).unwrap_or(workspace);
    let workspace = simplify_canonical_path(workspace);

    let exe_dir = std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(Path::to_path_buf));
    let (core, announce) = match sidecar::start(exe_dir.as_deref(), &workspace, ANNOUNCE_TIMEOUT) {
        Ok(ok) => ok,
        Err(e) => {
            fatal(&app, &e);
            return;
        }
    };
    if let Ok(mut slot) = app.state::<CoreSlot>().0.lock() {
        *slot = Some(core);
    }

    if let Some(h) = &home {
        let _ = std::fs::create_dir_all(h);
        let _ = write_atomic(
            &h.join("desktop.json"),
            boot::remember_project_json(&workspace).as_bytes(),
        );
    }

    let url = format!(
        "{}/?token={}",
        announce.url.trim_end_matches('/'),
        announce.token
    );
    let Ok(url) = url.parse::<tauri::Url>() else {
        fatal(
            &app,
            &sidecar::StartError {
                message: format!("bad url from the core: {url}"),
                stderr_tail: String::new(),
            },
        );
        return;
    };
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        let built = WebviewWindowBuilder::new(&handle, "main", WebviewUrl::External(url))
            .title("Orchestra")
            .inner_size(1200.0, 800.0)
            .build();
        if let Err(e) = built {
            eprintln!("[orchestra-desktop] cannot create the window: {e}");
            handle.exit(1);
        }
    });
}

/// Show what went wrong (with the core's last stderr lines) and exit 1.
fn fatal(app: &AppHandle, e: &sidecar::StartError) {
    let mut text = e.message.clone();
    if !e.stderr_tail.is_empty() {
        text.push_str("\n\n");
        text.push_str(&e.stderr_tail);
    }
    eprintln!("[orchestra-desktop] {text}");
    app.dialog()
        .message(text)
        .kind(MessageDialogKind::Error)
        .title("Orchestra could not start")
        .blocking_show();
    app.exit(1);
}

/// Temp file then rename, so a crash never leaves a half-written memory file.
fn write_atomic(path: &Path, data: &[u8]) -> std::io::Result<()> {
    let tmp = path.with_extension("json.tmp");
    std::fs::write(&tmp, data)?;
    std::fs::rename(&tmp, path)
}
