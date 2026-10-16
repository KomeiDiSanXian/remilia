use serde::{Deserialize, Serialize};
use std::fs;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;
use tauri::{AppHandle, Manager, RunEvent, WindowEvent};
// 托盘、菜单与 sidecar 属于桌面端能力：tauri 的 menu/tray 模块在移动端根本不存在，
// 必须整体置于 cfg(desktop) 之下，否则 Android/iOS 目标无法编译。
#[cfg(desktop)]
use tauri::{
    image::Image,
    menu::{MenuBuilder, MenuItemBuilder},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Emitter,
};
#[cfg(desktop)]
use tauri_plugin_shell::ShellExt;

// --- 应用状态 ---

#[derive(Default)]
struct AppState {
    api_url: Mutex<String>,
    api_key: Mutex<String>,
    bot_child: Mutex<Option<tauri_plugin_shell::process::CommandChild>>,
    /// 是否已确认真正退出应用。ExitRequested 处理器默认拦截退出以保留托盘，
    /// 只有标题栏/托盘的"完全关闭"会先置位该标志。
    exit_allowed: AtomicBool,
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
fn set_connection(state: tauri::State<'_, AppState>, config: ConnectionConfig) {
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
    // 最小化是桌面端专属能力：移动端 WebviewWindow 没有 minimize 方法
    #[cfg(desktop)]
    {
        if let Some(window) = app.get_webview_window("main") {
            let _ = window.minimize();
        }
    }
    #[cfg(not(desktop))]
    let _ = app;
}

#[tauri::command]
fn close_window(app: AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.hide();
    }
}

#[tauri::command]
fn quit_app(app: AppHandle) {
    quit_app_now(&app);
}

/// 请求真正退出应用：置位放行标志、停止 sidecar，再触发退出。
///
/// RuntimeEvent 处理器会对所有未放行的 ExitRequested 调用 prevent_exit()
/// （包括 app.exit() 触发的程序化退出），因此必须先置位再调用 exit。
fn quit_app_now(app: &AppHandle) {
    let state = app.state::<AppState>();
    state.exit_allowed.store(true, Ordering::SeqCst);
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
        *state.api_url.lock().expect("api_url lock poisoned") = "http://localhost:9002".to_string();
        log::info!("[Desktop] Go backend sidecar started on demand");
        Ok(true)
    } else {
        log::warn!("[Desktop] Failed to start sidecar on demand");
        Ok(false)
    }
}

// --- 进程管理 ---

/// 启动随包携带的 Go 后端 sidecar（仅桌面端）。
///
/// Android/iOS 不允许执行随包携带的外部可执行文件，移动端没有 sidecar 概念，
/// 因此移动端固定返回 None，由前端改为连接远程后端。
#[cfg(desktop)]
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
        .plugin(tauri_plugin_process::init())
        // 更新源（GitHub Releases 的 latest.json）与公钥配置见 tauri.conf.json 的 plugins.updater
        .plugin(tauri_plugin_updater::Builder::new().build())
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
            // 托盘/菜单是桌面端专属能力，移动端跳过
            #[cfg(desktop)]
            setup_tray(app)?;
            #[cfg(not(desktop))]
            let _ = app;

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
                let state = app_handle.state::<AppState>();
                if state.exit_allowed.load(Ordering::SeqCst) {
                    // 标题栏/托盘明确要求退出：放行。
                    return;
                }
                // 其他退出请求（例如所有窗口被销毁）：停掉 sidecar 并保持托盘常驻。
                kill_bot_child(&state);
                api.prevent_exit();
            }
        });
}

/// 桌面端：创建系统托盘与右键菜单，并确保主窗口可见。
#[cfg(desktop)]
fn setup_tray(app: &mut tauri::App) -> tauri::Result<()> {
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
    let icon = Image::from_bytes(include_bytes!("../icons/32x32.png")).expect("valid tray icon");

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
                quit_app_now(app);
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
}

/// 移动端没有 sidecar：固定返回 None，前端只提供"连接远程后端"。
#[cfg(mobile)]
fn spawn_bot_child(_app: &AppHandle) -> Option<tauri_plugin_shell::process::CommandChild> {
    log::warn!("[Desktop] sidecar is unavailable on mobile, connect to a remote backend instead");
    None
}
