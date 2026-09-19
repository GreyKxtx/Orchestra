fn main() {
    // Commands the chat page calls. The page is served from loopback, so it
    // is a remote origin to Tauri and every command it may reach has to be
    // named here and granted in capabilities/core-page.json; without the
    // manifest the ACL rejects them all.
    tauri_build::try_build(tauri_build::Attributes::new().app_manifest(
        tauri_build::AppManifest::new().commands(&[
            "browser_open",
            "browser_bounds",
            "browser_hide",
            "browser_close",
            "browser_navigate",
            "browser_pick",
            "browser_eval",
            "browser_zoom",
            "browser_cdp",
            "browser_external",
            "browser_installed",
        ]),
    ))
    .expect("failed to run tauri-build");
}
