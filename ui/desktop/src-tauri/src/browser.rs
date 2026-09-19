//! The browser panel: a second webview the user drives, with the element
//! picker injected into every page it opens.
//!
//! The picker (`ui/desktop/picker.js`) shares a world with the page's own
//! scripts — WebView2 has no isolated world — so this module is the only
//! channel it has. Arming is an `eval`; collecting is `eval_with_callback`,
//! polled while the picker is armed. Nothing in the page is ever handed a
//! token, the core's address or an IPC bridge, and the payload reaches the
//! chat only as a Tauri event to our own window, where the user still has to
//! send it.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager, Url, WebviewUrl, WebviewWindowBuilder};

/// The panel's window label.
pub const LABEL: &str = "browser";

/// The window that owns the chat, and so the one the pick is delivered to.
const MAIN: &str = "main";

/// Emitted to the main window when the user picks an element.
pub const PICK_EVENT: &str = "orchestra://pick";

const PICKER_JS: &str = include_str!("../../picker.js");

/// How often the armed picker is asked whether a pick has happened. A push
/// would need an IPC bridge inside a page we do not control, which is the one
/// thing this design refuses, so the Rust side asks instead.
const POLL_EVERY: Duration = Duration::from_millis(200);

/// Bounds the poll loop so a window left armed and forgotten cannot poll for
/// the life of the process.
const POLL_FOR: Duration = Duration::from_secs(300);

/// True while the picker is armed. One panel, one flag.
#[derive(Default)]
pub struct PickerState(pub Arc<Mutex<bool>>);

#[derive(Clone, Serialize)]
struct PickEvent {
    payload: String,
}

/// Only http(s) reaches a webview: a `file://` or `javascript:` URL typed into
/// the bar would run with the panel's privileges, and `data:` pages inherit an
/// opaque origin that the picker's own checks do not cover.
fn web_url(raw: &str) -> Result<Url, String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err("no address".into());
    }
    let candidate = if trimmed.contains("://") {
        trimmed.to_string()
    } else {
        format!("https://{trimmed}")
    };
    let parsed = Url::parse(&candidate).map_err(|e| format!("bad address: {e}"))?;
    match parsed.scheme() {
        "http" | "https" => Ok(parsed),
        other => Err(format!("{other}: only http and https open in the panel")),
    }
}

/// Opens the panel at `url`, or navigates it there when it is already open.
#[tauri::command]
pub fn browser_open(app: AppHandle, url: String) -> Result<(), String> {
    let target = web_url(&url)?;
    if let Some(window) = app.get_webview_window(LABEL) {
        window.navigate(target).map_err(|e| e.to_string())?;
        let _ = window.set_focus();
        return Ok(());
    }
    let handle = app.clone();
    WebviewWindowBuilder::new(&app, LABEL, WebviewUrl::External(target))
        .title("Orchestra Browser")
        .inner_size(1100.0, 800.0)
        // Unlike the main window, which is pinned to the core's origin, this
        // one is a browser: it must reach any site the user types.
        .initialization_script(PICKER_JS)
        .build()
        .map(|window| {
            // A closed panel is a disarmed picker: the poll loop stops on its
            // own once the flag is down, and a stale flag would keep the next
            // panel from ever arming.
            let state = handle.state::<PickerState>().0.clone();
            window.on_window_event(move |event| {
                if let tauri::WindowEvent::CloseRequested { .. } = event {
                    if let Ok(mut armed) = state.lock() {
                        *armed = false;
                    }
                }
            });
        })
        .map_err(|e| e.to_string())
}

/// Closes the panel if it is open. Never an error: the caller wants it gone.
#[tauri::command]
pub fn browser_close(app: AppHandle) {
    if let Ok(mut armed) = app.state::<PickerState>().0.lock() {
        *armed = false;
    }
    if let Some(window) = app.get_webview_window(LABEL) {
        let _ = window.close();
    }
}

/// `back`, `forward` or `reload` on the panel. The page's own history is the
/// only navigation the panel needs; an address bar lives in the chat UI.
#[tauri::command]
pub fn browser_navigate(app: AppHandle, action: String) -> Result<(), String> {
    let js = match action.as_str() {
        "back" => "history.back()",
        "forward" => "history.forward()",
        "reload" => "location.reload()",
        other => return Err(format!("unknown navigation: {other}")),
    };
    let window = app
        .get_webview_window(LABEL)
        .ok_or_else(|| "the browser panel is not open".to_string())?;
    window.eval(js).map_err(|e| e.to_string())
}

/// Arms or disarms the picker. While armed, a poll loop asks the page for a
/// payload and emits the first one to the main window.
#[tauri::command]
pub fn browser_pick(app: AppHandle, on: bool) -> Result<(), String> {
    let window = app
        .get_webview_window(LABEL)
        .ok_or_else(|| "the browser panel is not open".to_string())?;
    let state = app.state::<PickerState>().0.clone();

    if !on {
        if let Ok(mut armed) = state.lock() {
            *armed = false;
        }
        return window
            .eval("window.__orchPick && window.__orchPick.disarm()")
            .map_err(|e| e.to_string());
    }

    {
        let mut armed = state.lock().map_err(|_| "picker state poisoned")?;
        if *armed {
            return Ok(());
        }
        *armed = true;
    }
    window
        .eval("window.__orchPick && window.__orchPick.arm()")
        .map_err(|e| e.to_string())?;

    let app = app.clone();
    std::thread::spawn(move || poll_until_picked(app, state));
    Ok(())
}

fn poll_until_picked(app: AppHandle, state: Arc<Mutex<bool>>) {
    let deadline = std::time::Instant::now() + POLL_FOR;
    loop {
        std::thread::sleep(POLL_EVERY);
        let still_armed = state.lock().map(|a| *a).unwrap_or(false);
        if !still_armed || std::time::Instant::now() > deadline {
            break;
        }
        let Some(window) = app.get_webview_window(LABEL) else {
            break;
        };
        let handle = app.clone();
        let flag = state.clone();
        let asked = window.eval_with_callback(
            "window.__orchPick ? window.__orchPick.take() : 'idle'",
            move |result| deliver(&handle, &flag, result),
        );
        if asked.is_err() {
            break;
        }
    }
    if let Ok(mut armed) = state.lock() {
        *armed = false;
    }
}

/// One poll's answer. The page returns a string, which arrives here JSON
/// encoded, so the payload is the inner string: empty while nothing has been
/// picked, `idle` when the picker is not armed, otherwise the pick.
fn deliver(app: &AppHandle, armed: &Arc<Mutex<bool>>, result: String) {
    let Ok(inner) = serde_json::from_str::<String>(&result) else {
        return;
    };
    if inner.is_empty() || inner == "idle" {
        return;
    }
    // Untrusted: a hostile page can write anything here. It is delivered as
    // text to our own window, where it becomes an attachment the user sends
    // deliberately — never parsed into a command.
    if let Ok(mut flag) = armed.lock() {
        *flag = false;
    }
    let _ = app.emit_to(MAIN, PICK_EVENT, PickEvent { payload: inner });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_http_urls_open_and_a_bare_host_gets_https() {
        assert_eq!(
            web_url("example.com/pricing").unwrap().as_str(),
            "https://example.com/pricing"
        );
        assert_eq!(
            web_url(" http://127.0.0.1:5173/ ").unwrap().as_str(),
            "http://127.0.0.1:5173/"
        );
        for bad in [
            "file:///C:/Windows/win.ini",
            "javascript:fetch('/steal')",
            "data:text/html,<script>1</script>",
            "",
            "   ",
        ] {
            assert!(web_url(bad).is_err(), "{bad} must not open in the panel");
        }
    }

    #[test]
    fn a_poll_answer_only_reaches_the_chat_when_it_is_a_pick() {
        // The page's string arrives JSON encoded; empty and "idle" are the
        // two answers that mean "nothing happened yet".
        assert_eq!(serde_json::from_str::<String>("\"\"").unwrap(), "");
        assert_eq!(serde_json::from_str::<String>("\"idle\"").unwrap(), "idle");
        let pick = serde_json::to_string("{\"tag\":\"button\"}").unwrap();
        assert_eq!(
            serde_json::from_str::<String>(&pick).unwrap(),
            "{\"tag\":\"button\"}"
        );
        // Rubbish that is not a JSON string is ignored rather than delivered.
        assert!(serde_json::from_str::<String>("{not json}").is_err());
    }
}
