#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::Duration;

use orchestra_desktop::{boot, sidecar};
use sidecar::Sidecar;
use tauri::{AppHandle, Manager, RunEvent, WebviewUrl, WebviewWindowBuilder, WindowEvent};
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
                // Recover a poisoned mutex rather than skip the stop: nothing
                // else that locks this ever panics, but if it somehow did,
                // the core still deserves to be told to shut down.
                let core = app
                    .state::<CoreSlot>()
                    .0
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .take();
                if let Some(core) = core {
                    core.stop(STOP_GRACE);
                }
            }
        });
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
    let workspace = boot::simplify_canonical_path(workspace);

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
    // Recover a poisoned mutex rather than drop `core` unstopped: nothing
    // else that locks this ever panics, but if it somehow did, the sidecar
    // must still land in the slot so `RunEvent::Exit` can stop it.
    *app.state::<CoreSlot>()
        .0
        .lock()
        .unwrap_or_else(|e| e.into_inner()) = Some(core);

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
    let dispatched = app.run_on_main_thread(move || {
        let built = WebviewWindowBuilder::new(&handle, "main", WebviewUrl::External(url))
            .title("Orchestra")
            .inner_size(1200.0, 800.0)
            .build();
        match built {
            Ok(window) => {
                // The window is the only thing a user can close. Tauri does
                // not exit on its own once it closes — the dialog plugin
                // keeps its own (untitled) windows alive — so drive the exit
                // explicitly here. This still funnels through
                // `AppHandle::exit` -> `RunEvent::Exit` -> `Sidecar::stop`,
                // the one place that stops the core; nothing stops it from
                // here directly.
                let handle = handle.clone();
                window.on_window_event(move |event| {
                    if let WindowEvent::CloseRequested { .. } = event {
                        handle.exit(0);
                    }
                });
            }
            Err(e) => {
                eprintln!("[orchestra-desktop] cannot create the window: {e}");
                handle.exit(1);
            }
        }
    });
    if let Err(e) = dispatched {
        // Dispatch to the main thread itself failed: no window, no dialog —
        // without this the process would sit headless with a live core.
        eprintln!("[orchestra-desktop] cannot dispatch to the main thread: {e}");
        app.exit(1);
    }
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

/// Temp file → fsync → rename, so a crash never leaves a half-written or
/// not-yet-durable memory file (the atomic-write invariant in CLAUDE.md).
fn write_atomic(path: &Path, data: &[u8]) -> std::io::Result<()> {
    let tmp = path.with_extension("json.tmp");
    {
        let mut f = std::fs::File::create(&tmp)?;
        f.write_all(data)?;
        f.sync_all()?;
    }
    std::fs::rename(&tmp, path)
}
