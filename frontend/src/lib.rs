use serde::{Deserialize, Serialize};
use std::{fs, path::PathBuf};
use tauri_plugin_autostart::MacosLauncher;
use std::net::TcpListener;

#[derive(Serialize, Deserialize, Clone)]
struct ServerConfig {
    name: String,
    http_port: u16,
    p2p_port: u16,
    files_path: String,
    autostart: bool,
}

fn config_path() -> PathBuf {
    let mut p = dirs::config_dir().expect("no config dir");
    p.push("syne-server");
    fs::create_dir_all(&p).ok();
    p.push("config.json");
    p
}
fn d_interval() -> u32 { 1 }
fn d_id_digits() -> u8 { 8 }
fn d_claim_len() -> u8 { 8 }

#[derive(Serialize, Deserialize, Clone)]
struct ServerConfig {
    name: String,
    http_port: u16,
    p2p_port: u16,
    files_path: String,
    autostart: bool,
    #[serde(default = "d_interval")]
    monitor_interval_sec: u32,
    #[serde(default = "d_id_digits")]
    id_digits: u8,
    #[serde(default = "d_claim_len")]
    claim_code_length: u8,
}
.invoke_handler(tauri::generate_handler![
    is_configured, load_config, save_config, write_text_file, read_text_file
])
// пути приходят только из системных диалогов выбора файла
#[tauri::command]
fn write_text_file(path: String, content: String) -> Result<(), String> {
    fs::write(path, content).map_err(|e| e.to_string())
}

#[tauri::command]
fn read_text_file(path: String) -> Result<String, String> {
    fs::read_to_string(path).map_err(|e| e.to_string())
}
/// Есть ли уже конфиг (т.е. первый запуск уже был)
#[tauri::command]
fn is_configured() -> bool {
    config_path().exists()
}
fn port_free(p: u16) -> bool {
    TcpListener::bind(("0.0.0.0", p)).is_ok()
}
#[tauri::command]
fn load_config() -> Option<ServerConfig> {
    let s = fs::read_to_string(config_path()).ok()?;
    serde_json::from_str(&s).ok()
}

#[tauri::command]
fn save_config(config: ServerConfig) -> Result<(), String> {
    if !port_free(config.http_port) {
    return Err(format!("HTTP порт {} уже занят", config.http_port));
    }
    if !port_free(config.p2p_port) {
    return Err(format!("P2P порт {} уже занят", config.p2p_port));
    }
    if config.name.trim().is_empty() {
        return Err("Название не может быть пустым".into());
    }
    if config.http_port == config.p2p_port {
        return Err("HTTP и P2P порты должны отличаться".into());
    }
    fs::create_dir_all(&config.files_path).map_err(|e| format!("Папка недоступна: {e}"))?;

    let json = serde_json::to_string_pretty(&config).map_err(|e| e.to_string())?;
    fs::write(config_path(), json).map_err(|e| e.to_string())?;

    // TODO: здесь запустить Go-сервер (sidecar / std::process::Command)
    // с флагами --name --http-port --p2p-port --data-dir
    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_autostart::init(MacosLauncher::LaunchAgent, None))
        .invoke_handler(tauri::generate_handler![is_configured, load_config, save_config])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}