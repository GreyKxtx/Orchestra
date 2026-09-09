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
// Window creation is normally near-instant; 10s is generous headroom for a
// slow machine while still bounding the wait so a main thread that never
// runs the dispatched closure cannot hang the boot thread forever.
const WINDOW_DISPATCH_TIMEOUT: Duration = Duration::from_secs(10);

fn main() {
    if sidecar::maybe_run_fake_sidecar() {
        return;
    }
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_notification::init())
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
    // `build()` and dispatch failures both must end in `fatal()` — a native
    // dialog — same as every other startup failure. `fatal()` blocks on the
    // dialog and must only be called from this (boot) thread, but the
    // closure below runs ON the main thread, so it cannot call `fatal()`
    // itself (that would deadlock the dialog against the very thread that
    // needs to pump it). Instead it sends the outcome back over a channel
    // and this thread calls `fatal()` after receiving it.
    let (tx, rx) = std::sync::mpsc::channel::<Option<String>>();
    let handle = app.clone();
    let dispatched = app.run_on_main_thread(move || {
        let built = WebviewWindowBuilder::new(&handle, "main", WebviewUrl::External(url))
            .title("Orchestra")
            .inner_size(1200.0, 800.0)
            .build();
        if let Ok(window) = &built {
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
        let _ = tx.send(built.err().map(|e| e.to_string()));
    });
    if let Err(e) = dispatched {
        // Dispatch to the main thread itself failed: no window is coming and
        // the closure above will never run — without a dialog here the
        // process would sit headless with a live core and no explanation.
        fatal(
            &app,
            &sidecar::StartError {
                message: format!(
                    "Orchestra could not reach its own main thread to open a window: {e}"
                ),
                stderr_tail: String::new(),
            },
        );
        return;
    }
    match rx.recv_timeout(WINDOW_DISPATCH_TIMEOUT) {
        Ok(None) => {} // window created; the success path above already wired it up
        Ok(Some(msg)) => {
            // In a release build there is no console (windows_subsystem =
            // "windows"), so this is the only place the user learns why the
            // app just vanished. The most likely real cause by far is a
            // missing/broken WebView2 runtime, so name it.
            fatal(
                &app,
                &sidecar::StartError {
                    message: format!(
                        "Orchestra could not open its window: {msg}\n\nThis usually means the Microsoft Edge WebView2 Runtime is missing or broken. Install it from https://developer.microsoft.com/microsoft-edge/webview2/ and try again."
                    ),
                    stderr_tail: String::new(),
                },
            );
        }
        Err(_) => {
            // The closure was dispatched but never ran (or never sent) within
            // the timeout: bounded wait, not recv() forever, so the user
            // still gets told something rather than the app hanging.
            fatal(
                &app,
                &sidecar::StartError {
                    message: format!(
                        "Orchestra's main thread did not respond within {}s while opening a window.",
                        WINDOW_DISPATCH_TIMEOUT.as_secs()
                    ),
                    stderr_tail: String::new(),
                },
            );
        }
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
