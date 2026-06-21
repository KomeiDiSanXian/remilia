use serde::{Deserialize, Serialize};
use std::sync::Mutex;
use std::fs;
use tauri::{
    image::Image,
    menu::{MenuBuilder, MenuItemBuilder},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Emitter, Manager, RunEvent, WindowEvent,
};
use tauri_plugin_shell::ShellExt;

// --- 应用状态 ---

#[derive(Default)]
struct AppState {
    api_url: Mutex<String>,
    api_key: Mutex<String>,
    bot_child: Mutex<Option<tauri_plugin_shell::process::CommandChild>>,
}

#[derive(Serialize, Deserialize)]
struct ConnectionConfig {
    url: String,
    key: String,
}

#[derive(Serialize, Deserialize)]
struct WindowState {
    x: i32,
    y: i32,
    width: u32,
    height: u32,
}

fn window_state_path(app: &AppHandle) -> Option<std::path::PathBuf> {
    let dir = app.path().app_config_dir().ok()?;
    Some(dir.join("window_state.json"))
}

// --- Tauri 命令 ---

#[tauri::command]
fn get_connection(state: tauri::State<'_, AppState>) -> ConnectionConfig {
    ConnectionConfig {
        url: state.api_url.lock().expect("api_url lock poisoned").clone(),
        key: state.api_key.lock().expect("api_key lock poisoned").clone(),
    }
}

#[tauri::command]
fn set_connection(
    state: tauri::State<'_, AppState>,
    config: ConnectionConfig,
) {
    *state.api_url.lock().expect("api_url lock poisoned") = config.url;
    *state.api_key.lock().expect("api_key lock poisoned") = config.key;
}

#[tauri::command]
fn save_window_state(app: AppHandle, state: WindowState) {
    if let Some(path) = window_state_path(&app) {
        if let Some(dir) = path.parent() {
            let _ = fs::create_dir_all(dir);
        }
        if let Ok(json) = serde_json::to_string(&state) {
            let _ = fs::write(path, json);
        }
    }
}

#[tauri::command]
fn load_window_state(app: AppHandle) -> Option<WindowState> {
    let path = window_state_path(&app)?;
    let data = fs::read_to_string(path).ok()?;
    serde_json::from_str(&data).ok()
}

#[tauri::command]
fn minimize_window(app: AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.minimize();
    }
}

#[tauri::command]
fn close_window(app: AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.hide();
    }
}

#[tauri::command]
fn quit_app(app: AppHandle) {
    let state = app.state::<AppState>();
    kill_bot_child(&state);
    app.exit(0);
}

/// 按需启动 Go 后端 sidecar，返回是否成功。
#[tauri::command]
fn start_bot(app: AppHandle) -> Result<bool, String> {
    // 先杀掉可能残留的旧进程
    let state = app.state::<AppState>();
    kill_bot_child(&state);

    if let Some(child) = spawn_bot_child(&app) {
        let state = app.state::<AppState>();
        *state.bot_child.lock().expect("bot_child lock poisoned") = Some(child);
        *state.api_url.lock().expect("api_url lock poisoned") =
            "http://localhost:9002".to_string();
        log::info!("[Desktop] Go backend sidecar started on demand");
        Ok(true)
    } else {
        log::warn!("[Desktop] Failed to start sidecar on demand");
        Ok(false)
    }
}

// --- 进程管理 ---

fn spawn_bot_child(app: &AppHandle) -> Option<tauri_plugin_shell::process::CommandChild> {
    let sidecar = app.shell().sidecar("remilia-bot");
    match sidecar {
        Err(e) => {
            log::error!("[Desktop] Failed to create sidecar command: {}", e);
            return None;
        }
        Ok(cmd) => {
            // 将 sidecar 工作目录设为 AppData，使 data/、config.yaml 位于标准位置
            let data_dir = app.path().app_data_dir().ok()?;
            if let Err(e) = fs::create_dir_all(&data_dir) {
                log::warn!("[Desktop] Failed to create app data dir: {}", e);
            }
            let cmd = cmd.current_dir(data_dir);
            match cmd.spawn() {
                Err(e) => {
                    log::error!("[Desktop] Failed to spawn sidecar: {}", e);
                    None
                }
                Ok((_rx, child)) => {
                    log::info!("[Desktop] Go backend sidecar started");
                    Some(child)
                }
            }
        }
    }
}

fn kill_bot_child(state: &AppState) {
    if let Ok(mut guard) = state.bot_child.lock() {
        if let Some(child) = guard.take() {
            let _ = child.kill();
            log::info!("[Desktop] Go backend sidecar stopped");
        }
    }
}

// --- 应用入口 ---

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .manage(AppState::default())
        .invoke_handler(tauri::generate_handler![
            get_connection,
            set_connection,
            minimize_window,
            close_window,
            quit_app,
            save_window_state,
            load_window_state,
            start_bot,
        ])
        .setup(|app| {
            // 系统托盘右键菜单
            let show = MenuItemBuilder::with_id("show", "显示窗口").build(app)?;
            let reconnect = MenuItemBuilder::with_id("reconnect", "重新连接").build(app)?;
            let about = MenuItemBuilder::with_id("about", "关于").build(app)?;
            let quit = MenuItemBuilder::with_id("quit", "退出").build(app)?;

            let tray_menu = MenuBuilder::new(app)
                .item(&show)
                .item(&reconnect)
                .item(&about)
                .separator()
                .item(&quit)
                .build()?;

            // 系统托盘
            // 确保窗口显示（TrayIconBuilder 可能导致窗口初始隐藏）
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.set_focus();
            }

            // 从嵌入式 PNG 字节加载托盘图标
            let icon = Image::from_bytes(include_bytes!("../icons/32x32.png"))
                .expect("valid tray icon");

            TrayIconBuilder::new()
                .icon(icon)
                .menu(&tray_menu)
                .tooltip("Remilia Desktop")
                .on_menu_event(|app, event| match event.id().as_ref() {
                    "show" => {
                        if let Some(window) = app.get_webview_window("main") {
                            let _ = window.show();
                            let _ = window.set_focus();
                        }
                    }
                    "reconnect" => {
                        // 重新连接：关闭设置页重新打开
                        if let Some(window) = app.get_webview_window("main") {
                            let _ = window.show();
                            let _ = window.set_focus();
                            let _ = window.emit("navigate", "settings");
                        }
                    }
                    "about" => {
                        if let Some(window) = app.get_webview_window("main") {
                            let _ = window.show();
                            let _ = window.set_focus();
                            let _ = window.emit("navigate", "about");
                        }
                    }
                    "quit" => {
                        let state = app.state::<AppState>();
                        kill_bot_child(&state);
                        app.exit(0);
                    }
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        let app = tray.app_handle();
                        if let Some(window) = app.get_webview_window("main") {
                            let _ = window.show();
                            let _ = window.set_focus();
                        }
                    }
                })
                .build(app)?;

            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .build(tauri::generate_context!())
        .expect("failed to build app")
        .run(|app_handle, event| {
            if let RunEvent::ExitRequested { api, .. } = event {
                // 应用退出时杀掉 sidecar
                let state = app_handle.state::<AppState>();
                kill_bot_child(&state);
                api.prevent_exit();
            }
        });
}
