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

use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::PathBuf;
use std::process::Command;
use std::sync::mpsc::channel;
use std::sync::{Arc, Mutex, OnceLock};
use std::time::Duration;

use serde::Serialize;
use tauri::webview::WebviewBuilder;
use tauri::{AppHandle, Emitter, Manager, PhysicalPosition, PhysicalSize, Url, WebviewUrl};

/// The panel's webview label.
pub const LABEL: &str = "browser";

/// The devtools webview's label: the browser's own developer tools, docked
/// beside the page rather than opened in a window of their own.
pub const DEVTOOLS: &str = "browser-devtools";

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

/// The pane's rectangle in physical pixels: the page multiplies what it
/// measures by its devicePixelRatio, so the display's scaling and the app's
/// own zoom are both already in the numbers, and this side converts nothing.
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

    fn position(&self) -> PhysicalPosition<i32> {
        PhysicalPosition::new(
            self.x.max(0.0).round() as i32,
            self.y.max(0.0).round() as i32,
        )
    }

    fn size(&self) -> PhysicalSize<u32> {
        PhysicalSize::new(
            self.width.max(1.0).round() as u32,
            self.height.max(1.0).round() as u32,
        )
    }
}

/// The browser arguments the app's own window is built with. WebView2 runs
/// one browser process per user-data folder and refuses a second environment
/// there with different arguments, so every webview sharing a folder has to
/// be built with the same ones. `ORCH_DEBUG_PORT=<port>` adds remote
/// debugging, which is how a script drives the shell and looks at the result;
/// the first flag restates wry's own default, which setting this would drop.
pub fn browser_args() -> Option<String> {
    let port = std::env::var("ORCH_DEBUG_PORT").ok()?;
    Some(format!("{DEFAULT_ARGS} --remote-debugging-port={port}"))
}

const DEFAULT_ARGS: &str = "--disable-features=msWebOOUI,msPdfOOUI,msSmartScreenProtection";

/// The port the panel's own browser process listens on for the devtools
/// protocol. Chosen once, by asking the system for a free one.
///
/// This is what makes the real developer tools possible: their frontend is
/// served by that same process, so it can be shown in a webview of ours
/// instead of the window WebView2 opens on its own. It is also why the panel
/// gets a profile of its own below — a debugging port drives every page in
/// its environment, and the page with the app's capabilities must not be one
/// of them. What is reachable here is the site the user is browsing, and
/// only from this machine.
fn panel_port() -> u16 {
    static PORT: OnceLock<u16> = OnceLock::new();
    *PORT.get_or_init(|| {
        TcpListener::bind("127.0.0.1:0")
            .ok()
            .and_then(|l| l.local_addr().ok())
            .map(|a| a.port())
            .unwrap_or(0)
    })
}

/// The panel's own WebView2 profile, beside the app's data. Separate from the
/// app's: it carries the cookies and the cache of whatever is browsed, and it
/// is the environment the debugging port above belongs to.
fn panel_profile(app: &AppHandle) -> PathBuf {
    app.path()
        .app_local_data_dir()
        .unwrap_or_else(|_| std::env::temp_dir().join("orchestra"))
        .join("browser-profile")
}

/// The arguments the panel and its devtools are both built with — identical,
/// or WebView2 refuses the second one.
///
/// `--remote-allow-origins` is what lets the developer tools attach at all:
/// their frontend is served from the debugging port, so its WebSocket carries
/// an `Origin`, and the protocol server answers 403 to any origin it was not
/// told about. The one named here is that same server's own address.
fn panel_args() -> String {
    let port = panel_port();
    format!(
        "{DEFAULT_ARGS} --remote-debugging-port={port} --remote-allow-origins=http://127.0.0.1:{port}"
    )
}

/// Asks the panel's browser process which pages it is hosting, and returns
/// the one that is the browsed page: its own devtools are a page too.
fn devtools_target(port: u16) -> Result<String, String> {
    let at = std::net::SocketAddr::from(([127, 0, 0, 1], port));
    let mut socket = TcpStream::connect_timeout(&at, ANSWER_WITHIN)
        .map_err(|e| format!("the panel's devtools are not listening: {e}"))?;
    let _ = socket.set_read_timeout(Some(ANSWER_WITHIN));
    // CRLF, spelled out: this server closes on a request with bare newlines.
    socket
        .write_all(CRLF_REQUEST.as_bytes())
        .map_err(|e| e.to_string())?;
    // The devtools' own HTTP server holds the connection open whatever the
    // request asked for, so the answer is read until its body is as long as
    // its Content-Length says — never until end of stream.
    let mut answer = Vec::new();
    let mut chunk = [0u8; 4096];
    loop {
        match socket.read(&mut chunk) {
            Ok(0) => break,
            Ok(n) => answer.extend_from_slice(&chunk[..n]),
            Err(e) => return Err(format!("the devtools answer was cut short: {e}")),
        }
        if let Some((head, body)) = split_http(&answer) {
            if content_length(head).is_some_and(|want| body.len() >= want) {
                break;
            }
        }
    }
    let (_, body) = split_http(&answer).ok_or_else(|| {
        format!(
            "the devtools answer had no body: port {port}, {} byte(s): {}",
            answer.len(),
            String::from_utf8_lossy(&answer[..answer.len().min(120)])
        )
    })?;
    let text = std::str::from_utf8(body).map_err(|e| e.to_string())?;
    let targets: Vec<serde_json::Value> =
        serde_json::from_str(text.trim()).map_err(|e| format!("unreadable target list: {e}"))?;
    targets
        .iter()
        .find(|t| {
            t.get("type").and_then(|v| v.as_str()) == Some("page")
                && !t
                    .get("url")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .contains("/devtools/")
        })
        .and_then(|t| t.get("id").and_then(|v| v.as_str()))
        .map(|id| id.to_string())
        .ok_or_else(|| "the panel has no page to inspect".to_string())
}

/// The one request this module makes, with the line endings HTTP insists on.
const CRLF_REQUEST: &str =
    "GET /json/list HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n";

/// An HTTP answer split at its blank line.
fn split_http(answer: &[u8]) -> Option<(&str, &[u8])> {
    let at = answer.windows(4).position(|w| w == b"\r\n\r\n")?;
    Some((std::str::from_utf8(&answer[..at]).ok()?, &answer[at + 4..]))
}

fn content_length(head: &str) -> Option<usize> {
    head.lines()
        .find(|l| l.to_ascii_lowercase().starts_with("content-length:"))
        .and_then(|l| l.split(':').nth(1))
        .and_then(|v| v.trim().parse().ok())
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
        .data_directory(panel_profile(&app))
        .additional_browser_args(&panel_args())
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
    for label in [LABEL, DEVTOOLS] {
        if let Some(webview) = app.get_webview(label) {
            let _ = webview.hide();
        }
    }
}

/// Closes the panel for good.
#[tauri::command(async)]
pub fn browser_close(app: AppHandle) {
    if let Ok(mut armed) = app.state::<PickerState>().0.lock() {
        *armed = false;
    }
    for label in [LABEL, DEVTOOLS] {
        if let Some(webview) = app.get_webview(label) {
            let _ = webview.close();
        }
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

/// The element the panel's colours are written into, and the function that
/// writes them. Named so a second call replaces the first rather than piling
/// stylesheets up.
const STYLE_ID: &str = "orchestra-palette";
const PAINT: &str = "__orchestraPaint";

/// What the tools are told before they boot, all of it their own page's.
///
/// Three settings first. Edge's welcome tab, which opens beside the panels on
/// a profile that has not seen this version — and since the panel's port, and
/// so its origin, is chosen afresh at every launch, that is every launch. The
/// one key names only that tab; the tools fill their other tabs in around it.
///
/// Then the screencast — a picture of the page beside the
/// panels, for driving a phone from a desktop — is on by default whenever the
/// target is a remote one, which ours is; here the page is right there to the
/// left, so the picture would only be the width it costs. And the light or
/// dark they start in, which is the app's, so the colours below land on the
/// ground they were mixed for. Both only where the user has not answered for
/// themselves: their own click, in the session it is made, stands.
///
/// Then the colours. `css` names their own tokens with this app's values, and
/// every declaration in it carries `!important` — their stylesheet is an
/// adopted one, and those cascade after anything this page adds, so a plain
/// declaration loses. It reaches only colour; the type, the icons and the
/// arrangement of the panels are Chromium's, and chasing those would be
/// signing up to chase them again at every update.
///
/// Both settings and stylesheet are data from the page, so both are encoded
/// rather than pasted: a palette cannot become a statement.
fn devtools_boot(theme: &str, css: &str) -> String {
    // Their settings are JSON in storage, so a string setting is stored with
    // its quotes — the value is encoded once to store it, and again to be a
    // literal in this script.
    let stored = serde_json::to_string(theme).unwrap_or_else(|_| "\"dark\"".into());
    let theme = serde_json::to_string(&stored).unwrap_or_else(|_| "\"\\\"dark\\\"\"".into());
    let css = serde_json::to_string(css).unwrap_or_else(|_| "\"\"".into());
    format!(
        "try {{
           for (const key of ['screencast-enabled', 'screencastEnabled']) {{
             if (localStorage.getItem(key) === null) localStorage.setItem(key, 'false');
           }}
           for (const key of ['ui-theme', 'uiTheme']) {{
             if (localStorage.getItem(key) === null) localStorage.setItem(key, {theme});
           }}
           const tabs = JSON.parse(localStorage.getItem('closeable-tabs') || '{{}}');
           tabs.welcome = false;
           localStorage.setItem('closeable-tabs', JSON.stringify(tabs));
         }} catch (e) {{}}
         globalThis.{PAINT} = (css) => {{
           try {{ localStorage.setItem('{STYLE_ID}', css); }} catch (e) {{}}
           const put = () => {{
             if (!document.head) return false;
             let el = document.getElementById('{STYLE_ID}');
             if (!el) {{
               el = document.createElement('style');
               el.id = '{STYLE_ID}';
               document.head.appendChild(el);
             }}
             el.textContent = css;
             return true;
           }};
           if (!put()) document.addEventListener('DOMContentLoaded', put);
         }};
         let start = {css};
         try {{ start = localStorage.getItem('{STYLE_ID}') ?? start; }} catch (e) {{}}
         globalThis.{PAINT}(start);
"
    )
}

/// Told to the tools when the app's light or dark changes under them. Their
/// own colours — the syntax of the markup, the shades of the panels — are
/// chosen when they boot, so the palette alone would leave one theme's text
/// on the other theme's ground. The setting is forced here rather than
/// offered, because the user just changed the app's, and the page is read
/// again with the colours it was last painted with.
fn devtools_retheme(theme: &str) -> String {
    let stored = serde_json::to_string(theme).unwrap_or_else(|_| "\"dark\"".into());
    let theme = serde_json::to_string(&stored).unwrap_or_else(|_| "\"\\\"dark\\\"\"".into());
    format!(
        "try {{
           for (const key of ['ui-theme', 'uiTheme']) localStorage.setItem(key, {theme});
         }} catch (e) {{}}
         location.reload();
"
    )
}

/// The page's own developer tools — elements, console, network, sources, the
/// lot — docked over `rect`, which is a pane of the app's own page. Not the
/// window WebView2 opens by itself: their frontend is a page like any other,
/// served by the browser process the panel runs in, so it is shown in a
/// webview of ours beside the site.
#[tauri::command(async)]
pub fn browser_devtools(
    app: AppHandle,
    rect: Rect,
    on: bool,
    theme: String,
    css: String,
) -> Result<(), String> {
    if !on {
        if let Some(webview) = app.get_webview(DEVTOOLS) {
            let _ = webview.hide();
        }
        return Ok(());
    }
    if app.get_webview(LABEL).is_none() {
        return Err("the browser panel is not open".into());
    }
    // One caller at a time past here. Placement runs on every layout change,
    // so two calls can arrive while the first is still building its webview,
    // and the second would find none and build it again. Whoever holds this
    // also holds the page these tools are attached to.
    static ATTACHED: Mutex<String> = Mutex::new(String::new());
    let mut attached = ATTACHED.lock().map_err(|_| "devtools state poisoned")?;
    // What the panel is painted with. Placement runs on every layout change,
    // and repainting an unchanged palette on each of them would be work for
    // nothing.
    static PAINTED: Mutex<String> = Mutex::new(String::new());
    let mut painted = PAINTED.lock().map_err(|_| "devtools state poisoned")?;
    // And which light or dark they were booted into.
    static THEMED: Mutex<String> = Mutex::new(String::new());
    let mut themed = THEMED.lock().map_err(|_| "devtools state poisoned")?;

    let port = panel_port();
    let target = devtools_target(port)?;
    // Elements first: it is the page's own DOM, which is what this panel is
    // for. Everything else is a tab away.
    let url = format!(
        "http://127.0.0.1:{port}/devtools/inspector.html\
         ?ws=127.0.0.1:{port}/devtools/page/{target}&panel=elements"
    );
    let parsed = Url::parse(&url).map_err(|e| e.to_string())?;

    if let Some(webview) = app.get_webview(DEVTOOLS) {
        // A page the user browsed to is a new target, and the tools attached
        // to the old one only say the connection closed.
        if *attached != target {
            webview.navigate(parsed).map_err(|e| e.to_string())?;
            *attached = target;
            // A new document starts from the script it was built with, which
            // carries the palette of the day it was built.
            painted.clear();
        }
        if *painted != css {
            let call = serde_json::to_string(&css).unwrap_or_else(|_| "''".into());
            let _ = webview.eval(format!("globalThis.{PAINT} && globalThis.{PAINT}({call})"));
            painted.clone_from(&css);
        }
        // The colours are painted first, so the page that comes back from the
        // reload comes back wearing them.
        if *themed != theme {
            let _ = webview.eval(devtools_retheme(&theme));
            themed.clone_from(&theme);
        }
        let _ = webview.set_bounds(tauri::Rect {
            position: rect.position().into(),
            size: rect.size().into(),
        });
        return webview.show().map_err(|e| e.to_string());
    }

    let window = app
        .get_window(MAIN)
        .ok_or_else(|| "the main window is gone".to_string())?;
    // The same profile and the same arguments as the panel: one WebView2
    // environment, which is also what lets this page speak to that one.
    let builder = WebviewBuilder::new(DEVTOOLS, WebviewUrl::External(parsed))
        .data_directory(panel_profile(&app))
        .additional_browser_args(&panel_args())
        .initialization_script(devtools_boot(&theme, &css));
    window
        .add_child(builder, rect.position(), rect.size())
        .map_err(|e| e.to_string())?;
    *attached = target;
    painted.clone_from(&css);
    themed.clone_from(&theme);
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

    // The palette and the theme come from the page, so they are values in the
    // script the tools boot with, never words of it.
    #[test]
    fn the_palette_reaches_the_tools_as_data() {
        let script = devtools_boot(
            "light",
            ":root{--sys-color-base:#fff \"'</script> !important;}",
        );
        assert!(
            script.contains("localStorage.setItem(key, \"\\\"light\\\"\")"),
            "a string setting is stored with its own quotes: {script}"
        );
        assert!(
            script.contains("#fff \\\"'"),
            "a quote in the palette is escaped, not the end of the literal: {script}"
        );
        assert_eq!(
            script.matches("__orchestraPaint").count(),
            2,
            "the painter is defined once and called once"
        );
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
        assert_eq!(shown.position().x, 12);
        assert_eq!(shown.size().height, 600);

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
        assert_eq!(off.position().x, 0);
        assert_eq!(off.position().y, 0);
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
