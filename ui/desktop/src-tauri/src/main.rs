#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use orchestra_desktop::sidecar;

fn main() {
    if sidecar::maybe_run_fake_sidecar() {
        return;
    }
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .run(tauri::generate_context!())
        .expect("error while running Orchestra desktop");
}
