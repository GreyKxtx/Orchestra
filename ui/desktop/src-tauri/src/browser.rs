//! The browser panel: a webview of its own inside the main window, with the
//! element picker injected into every page it opens.
//!
//! It is a child webview rather than a second window because the panel is a
//! view of the app, beside Chat, Trajectory and Graph — the page tells us the
//! rectangle its pane occupies and this module keeps the webview over it.
//! (`Window::add_child` is behind tauri's `unstable` feature; that is the one
//! API here that is not on the stable path.)
//!
//! The picker (`ui/desktop/picker.js`) shares a world with the page's own
//! scripts — WebView2 has no isolated world — so this module is the only
//! channel it has. Arming is an `eval`; collecting is `eval_with_callback`,
//! polled while the picker is armed. Nothing in the page is ever handed a
//! token, the core's address or an IPC bridge, and a pick reaches the chat
//! only as a Tauri event to our own window, where the user still sends it by
//! hand.
//!
//! Every command is `async`. A plain command runs on the main thread, and
//! `add_child` waits for the main thread to build the webview: the two
//! together froze the app.

use std::process::Command;
use std::sync::mpsc::channel;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use serde::Serialize;
use tauri::webview::WebviewBuilder;
use tauri::{AppHandle, Emitter, LogicalPosition, LogicalSize, Manager, Url, WebviewUrl};

/// The panel's webview label.
pub const LABEL: &str = "browser";

/// The window that owns the chat, and so the one a pick is delivered to.
const MAIN: &str = "main";

/// Emitted to the main window when the user picks an element.
pub const PICK_EVENT: &str = "orchestra://pick";

/// Emitted to the main window whenever the panel navigates, so the address
/// bar follows the page.
pub const URL_EVENT: &str = "orchestra://browser-url";

const PICKER_JS: &str = include_str!("../../picker.js");

/// How often the armed picker is asked whether a pick has happened. A push
/// would need an IPC bridge inside a page we do not control, which is the one
/// thing this design refuses, so the Rust side asks instead.
const POLL_EVERY: Duration = Duration::from_millis(200);

/// Bounds the poll loop so a panel left armed and forgotten cannot poll for
/// the life of the process.
const POLL_FOR: Duration = Duration::from_secs(300);

/// How long a command waits for the panel to answer. A page can be busy, and
/// a screenshot of a long document is not instant; a page that never answers
/// must still not hold the command's thread for good.
const ANSWER_WITHIN: Duration = Duration::from_secs(20);

/// True while the picker is armed. One panel, one flag.
#[derive(Default)]
pub struct PickerState(pub Arc<Mutex<bool>>);

#[derive(Clone, Serialize)]
struct PickEvent {
    payload: String,
}

#[derive(Clone, Serialize)]
struct UrlEvent {
    url: String,
}

/// The pane's rectangle in the page's own CSS pixels. Logical units, so the
/// webview lands in the same place whatever the display's scaling.
#[derive(serde::Deserialize, Clone, Copy)]
pub struct Rect {
    x: f64,
    y: f64,
    width: f64,
    height: f64,
}

impl Rect {
    /// A rectangle too small to show anything is how the page says "hidden":
    /// a zero-sized webview is not something every platform accepts.
    fn is_visible(&self) -> bool {
        self.width >= 1.0 && self.height >= 1.0
    }

    fn position(&self) -> LogicalPosition<f64> {
        LogicalPosition::new(self.x.max(0.0), self.y.max(0.0))
    }

    fn size(&self) -> LogicalSize<f64> {
        LogicalSize::new(self.width.max(1.0), self.height.max(1.0))
    }
}

/// Only http(s) reaches the panel: a `file://` or `javascript:` address typed
/// into the bar would run with the app's privileges, and `data:` pages carry
/// an opaque origin the picker's own checks do not cover.
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

/// A link the page follows is held to the same rule as the bar, plus the
/// blank page a site may bounce through.
fn may_navigate(url: &Url) -> bool {
    matches!(url.scheme(), "http" | "https" | "about")
}

/// Opens the panel at `url` over `rect`, or navigates it there when it is
/// already open.
#[tauri::command(async)]
pub fn browser_open(app: AppHandle, url: String, rect: Rect) -> Result<(), String> {
    let target = web_url(&url)?;
    if let Some(webview) = app.get_webview(LABEL) {
        webview.navigate(target).map_err(|e| e.to_string())?;
        let _ = webview.set_bounds(tauri::Rect {
            position: rect.position().into(),
            size: rect.size().into(),
        });
        let _ = webview.show();
        return Ok(());
    }
    let window = app
        .get_window(MAIN)
        .ok_or_else(|| "the main window is gone".to_string())?;
    // The panel is a browser: unlike the chat window, which is pinned to the
    // core's origin, it must reach any site the user types. It is granted no
    // capability, so the page it shows can invoke nothing.
    let handle = app.clone();
    let builder = WebviewBuilder::new(LABEL, WebviewUrl::External(target))
        .initialization_script(PICKER_JS)
        .on_navigation(move |url| {
            if !may_navigate(url) {
                return false;
            }
            let _ = handle.emit_to(
                MAIN,
                URL_EVENT,
                UrlEvent {
                    url: url.to_string(),
                },
            );
            true
        });
    window
        .add_child(builder, rect.position(), rect.size())
        .map(|_| ())
        .map_err(|e| e.to_string())
}

/// Keeps the webview over its pane as the window resizes or the view changes.
#[tauri::command(async)]
pub fn browser_bounds(app: AppHandle, rect: Rect) -> Result<(), String> {
    let Some(webview) = app.get_webview(LABEL) else {
        return Ok(()); // nothing open yet: the next open carries the rectangle
    };
    if !rect.is_visible() {
        return webview.hide().map_err(|e| e.to_string());
    }
    webview
        .set_bounds(tauri::Rect {
            position: rect.position().into(),
            size: rect.size().into(),
        })
        .map_err(|e| e.to_string())?;
    webview.show().map_err(|e| e.to_string())
}

/// Hides the panel without closing it: leaving the Browser view must not throw
/// the page away, or coming back would reload it.
#[tauri::command(async)]
pub fn browser_hide(app: AppHandle) {
    if let Ok(mut armed) = app.state::<PickerState>().0.lock() {
        *armed = false;
    }
    if let Some(webview) = app.get_webview(LABEL) {
        let _ = webview.hide();
    }
}

/// Closes the panel for good.
#[tauri::command(async)]
pub fn browser_close(app: AppHandle) {
    if let Ok(mut armed) = app.state::<PickerState>().0.lock() {
        *armed = false;
    }
    if let Some(webview) = app.get_webview(LABEL) {
        let _ = webview.close();
    }
}

/// `back`, `forward` or `reload` on the panel.
#[tauri::command(async)]
pub fn browser_navigate(app: AppHandle, action: String) -> Result<(), String> {
    let js = match action.as_str() {
        "back" => "history.back()",
        "forward" => "history.forward()",
        "reload" => "location.reload()",
        other => return Err(format!("unknown navigation: {other}")),
    };
    let webview = app
        .get_webview(LABEL)
        .ok_or_else(|| "the browser panel is not open".to_string())?;
    webview.eval(js).map_err(|e| e.to_string())
}

/// Arms or disarms the picker. While armed, a poll loop asks the page for a
/// payload and emits the first one to the main window.
#[tauri::command(async)]
pub fn browser_pick(app: AppHandle, on: bool) -> Result<(), String> {
    let webview = app
        .get_webview(LABEL)
        .ok_or_else(|| "the browser panel is not open".to_string())?;
    let state = app.state::<PickerState>().0.clone();

    if !on {
        if let Ok(mut armed) = state.lock() {
            *armed = false;
        }
        return webview
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
    webview
        .eval("window.__orchPick && window.__orchPick.arm()")
        .map_err(|e| e.to_string())?;

    let app = app.clone();
    std::thread::spawn(move || poll_until_picked(app, state));
    Ok(())
}

/// Runs one line in the panel and hands back what it evaluated to, JSON
/// encoded. The console's own input arrives here, so the page must wrap its
/// own throws: the runtime drops exceptions on Windows rather than reporting
/// them.
#[tauri::command(async)]
pub fn browser_eval(app: AppHandle, js: String) -> Result<String, String> {
    let webview = app
        .get_webview(LABEL)
        .ok_or_else(|| "the browser panel is not open".to_string())?;
    let (tx, rx) = channel();
    webview
        .eval_with_callback(js, move |answer| {
            let _ = tx.send(answer);
        })
        .map_err(|e| e.to_string())?;
    rx.recv_timeout(ANSWER_WITHIN)
        .map_err(|_| "the page did not answer".to_string())
}

/// The panel's zoom, as the menu's − and + set it.
#[tauri::command(async)]
pub fn browser_zoom(app: AppHandle, scale: f64) -> Result<(), String> {
    let webview = app
        .get_webview(LABEL)
        .ok_or_else(|| "the browser panel is not open".to_string())?;
    webview
        .set_zoom(scale.clamp(0.25, 3.0))
        .map_err(|e| e.to_string())
}

/// One devtools-protocol call on the panel — the same protocol F12 speaks.
/// The screenshot, the reload that ignores the cache and the Clear items have
/// no other API in WebView2, so they all come through here.
#[tauri::command(async)]
pub fn browser_cdp(app: AppHandle, method: String, params: String) -> Result<String, String> {
    let webview = app
        .get_webview(LABEL)
        .ok_or_else(|| "the browser panel is not open".to_string())?;
    let (tx, rx) = channel::<Result<String, String>>();
    let failed = tx.clone();
    webview
        .with_webview(move |platform| {
            if let Err(e) = call_devtools(&platform, &method, &params, tx) {
                let _ = failed.send(Err(e));
            }
        })
        .map_err(|e| e.to_string())?;
    rx.recv_timeout(ANSWER_WITHIN)
        .map_err(|_| "the panel did not answer".to_string())?
}

/// The call itself, on the thread that owns the webview. The answer is sent
/// from WebView2's own completion callback, which the window's message loop
/// delivers — nothing here waits on that thread.
#[cfg(windows)]
fn call_devtools(
    platform: &tauri::webview::PlatformWebview,
    method: &str,
    params: &str,
    tx: std::sync::mpsc::Sender<Result<String, String>>,
) -> Result<(), String> {
    use webview2_com::CallDevToolsProtocolMethodCompletedHandler;
    use windows::core::PCWSTR;

    let wide = |s: &str| -> Vec<u16> { s.encode_utf16().chain(std::iter::once(0)).collect() };
    let (name, args) = (wide(method), wide(params));
    let core = unsafe { platform.controller().CoreWebView2() }.map_err(|e| e.to_string())?;
    // The handler is handed the call's status and its JSON already decoded.
    let handler = CallDevToolsProtocolMethodCompletedHandler::create(Box::new(
        move |status: windows::core::Result<()>, json: String| {
            let _ = tx.send(match status {
                Ok(()) => Ok(json),
                Err(e) => Err(format!("the devtools call failed: {e}")),
            });
            Ok(())
        },
    ));
    unsafe {
        core.CallDevToolsProtocolMethod(PCWSTR(name.as_ptr()), PCWSTR(args.as_ptr()), &handler)
    }
    .map_err(|e| e.to_string())
}

#[cfg(not(windows))]
fn call_devtools(
    _platform: &tauri::webview::PlatformWebview,
    _method: &str,
    _params: &str,
    _tx: std::sync::mpsc::Sender<Result<String, String>>,
) -> Result<(), String> {
    Err("the panel's devtools are a WebView2 interface, so this is Windows only".into())
}

/// Opens the address in a browser of the machine's own: the one chosen in the
/// panel's menu, or whatever the system opens links with. The panel itself
/// draws with WebView2 because that is the only engine an app may embed here,
/// so this is how a page reaches the others.
#[tauri::command(async)]
pub fn browser_external(url: String, exe: String) -> Result<(), String> {
    let target = web_url(&url)?;
    let exe = exe.trim();
    let mut command = if exe.is_empty() {
        default_opener()
    } else {
        Command::new(exe)
    };
    command
        .arg(target.as_str())
        .spawn()
        .map(|_| ())
        .map_err(|e| format!("could not open a browser: {e}"))
}

#[cfg(windows)]
fn default_opener() -> Command {
    // The shell's own handler for a URL, with no cmd.exe in between to
    // reinterpret the &s of a query string.
    let mut c = Command::new("rundll32.exe");
    c.arg("url.dll,FileProtocolHandler");
    c
}

#[cfg(not(windows))]
fn default_opener() -> Command {
    Command::new(if cfg!(target_os = "macos") {
        "open"
    } else {
        "xdg-open"
    })
}

/// One browser installed on this machine, as the menu lists it.
#[derive(Clone, Serialize)]
pub struct Browser {
    id: String,
    name: String,
    exe: String,
}

/// The browsers this machine has.
#[tauri::command(async)]
pub fn browser_installed() -> Vec<Browser> {
    installed_browsers()
}

/// `"C:\...\chrome.exe" -- "%1"` — the registry keeps a command line, and
/// what the menu needs out of it is the program.
#[cfg_attr(not(windows), allow(dead_code))]
fn exe_from_command(raw: &str) -> String {
    let raw = raw.trim();
    if let Some(rest) = raw.strip_prefix('"') {
        return rest.split('"').next().unwrap_or_default().to_string();
    }
    match raw.to_ascii_lowercase().find(".exe") {
        Some(end) => raw[..end + 4].to_string(),
        None => raw
            .split_whitespace()
            .next()
            .unwrap_or_default()
            .to_string(),
    }
}

/// Windows registers every browser under one key, which is what the Default
/// Apps page reads too.
#[cfg(windows)]
fn installed_browsers() -> Vec<Browser> {
    use winreg::enums::{HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE};
    use winreg::RegKey;

    const CLIENTS: &str = r"SOFTWARE\Clients\StartMenuInternet";
    let mut out: Vec<Browser> = Vec::new();
    for root in [HKEY_LOCAL_MACHINE, HKEY_CURRENT_USER] {
        let Ok(clients) = RegKey::predef(root).open_subkey(CLIENTS) else {
            continue;
        };
        for key in clients.enum_keys().flatten() {
            let Ok(entry) = clients.open_subkey(&key) else {
                continue;
            };
            let Ok(open) = entry.open_subkey(r"shell\open\command") else {
                continue;
            };
            let raw: String = open.get_value("").unwrap_or_default();
            let exe = exe_from_command(&raw);
            if exe.is_empty() || out.iter().any(|b| b.exe.eq_ignore_ascii_case(&exe)) {
                continue;
            }
            let name: String = entry.get_value("").unwrap_or_else(|_| key.clone());
            out.push(Browser {
                id: key.clone(),
                name,
                exe,
            });
        }
    }
    out
}

#[cfg(not(windows))]
fn installed_browsers() -> Vec<Browser> {
    Vec::new()
}

fn poll_until_picked(app: AppHandle, state: Arc<Mutex<bool>>) {
    let deadline = std::time::Instant::now() + POLL_FOR;
    loop {
        std::thread::sleep(POLL_EVERY);
        let still_armed = state.lock().map(|a| *a).unwrap_or(false);
        if !still_armed || std::time::Instant::now() > deadline {
            break;
        }
        let Some(webview) = app.get_webview(LABEL) else {
            break;
        };
        let handle = app.clone();
        let flag = state.clone();
        let asked = webview.eval_with_callback(
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
    fn the_program_is_read_out_of_a_registered_command_line() {
        assert_eq!(
            exe_from_command("\"C:\\Program Files\\Google\\Chrome\\chrome.exe\" -- \"%1\""),
            "C:\\Program Files\\Google\\Chrome\\chrome.exe"
        );
        // Unquoted, which older entries are, and with the switches dropped.
        assert_eq!(
            exe_from_command("C:\\Windows\\firefox.EXE -osint -url \"%1\""),
            "C:\\Windows\\firefox.EXE"
        );
        assert_eq!(exe_from_command("   "), "");
    }

    #[test]
    fn a_link_opens_only_where_the_bar_would() {
        for ok in [
            "https://example.com/a",
            "http://127.0.0.1:5173/",
            "about:blank",
        ] {
            assert!(may_navigate(&Url::parse(ok).unwrap()), "{ok}");
        }
        for no in [
            "file:///C:/Windows/win.ini",
            "javascript:alert(1)",
            "data:text/html,x",
        ] {
            assert!(!may_navigate(&Url::parse(no).unwrap()), "{no}");
        }
    }

    #[test]
    fn a_pane_with_no_room_is_how_the_page_says_hidden() {
        let shown = Rect {
            x: 12.0,
            y: 48.0,
            width: 900.0,
            height: 600.0,
        };
        assert!(shown.is_visible());
        assert_eq!(shown.position().x, 12.0);
        assert_eq!(shown.size().height, 600.0);

        let hidden = Rect {
            x: 0.0,
            y: 0.0,
            width: 0.0,
            height: 0.0,
        };
        assert!(!hidden.is_visible());
        // A negative origin would put the panel off the window; clamped.
        let off = Rect {
            x: -40.0,
            y: -10.0,
            width: 10.0,
            height: 10.0,
        };
        assert_eq!(off.position().x, 0.0);
        assert_eq!(off.position().y, 0.0);
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
