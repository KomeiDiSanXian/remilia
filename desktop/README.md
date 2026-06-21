# Remilia Desktop

基于 Tauri v2 的 Remilia Bot 桌面管理客户端。

## 系统要求

### Windows
- **Visual Studio 2022 Build Tools**（含 C++ workload）
  下载地址：https://visualstudio.microsoft.com/visual-cpp-build-tools/
  安装时勾选"使用 C++ 的桌面开发"
- **WebView2**（Windows 11 自带，Windows 10 需手动安装）
- **Rust**（rustup 安装）
- **Node.js 18+**

### macOS
- Xcode Command Line Tools: `xcode-select --install`
- Rust + Node.js 18+

### Linux
- WebKitGTK: `sudo apt install libwebkit2gtk-4.1-dev build-essential`
- Rust + Node.js 18+

## 开发

```bash
# 1. 确保 Go API 服务器在运行
cd .. && go run ./cmd/bot

# 2. 启动 Tauri 开发模式（新终端）
cd desktop
npm install
npm run tauri dev
```

## 构建

```bash
# 构建 release 包（.msi / .dmg / .AppImage）
npm run tauri build

# 或使用顶层 Makefile
make desktop-build
```

## 架构说明

```
desktop/src/           ← React 前端（引用 dashboard/src 的组件）
desktop/src-tauri/     ← Rust 后端（系统托盘、窗口管理、API 命令）
dashboard/src/         ← 共享的仪表盘组件（BotStatus, PluginList 等）
```

- 前端通过 Vite alias `@dashboard` 引用 `../dashboard/src` 的组件
- Rust 后端提供系统托盘、自定义标题栏、窗口隐藏到托盘等功能
- 通过 REST API 与 Go 后端通信（默认 http://localhost:9002）
