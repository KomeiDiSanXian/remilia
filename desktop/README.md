# Remilia Desktop

基于 Tauri v2 的 Remilia Bot 桌面管理客户端。

## 系统要求

### Windows
- **Visual Studio 2022 Build Tools**（含 C++ workload）
  下载地址：https://visualstudio.microsoft.com/visual-cpp-build-tools/
  安装时勾选"使用 C++ 的桌面开发"
- **WebView2**（Windows 11 自带，Windows 10 需手动安装）
- **Rust 1.90+**（rustup 安装；Tauri 2.12 起的版本要求）
- **Node.js 18+**

### macOS
- Xcode Command Line Tools: `xcode-select --install`
- Rust 1.90+ + Node.js 18+

### Linux
- WebKitGTK: `sudo apt install libwebkit2gtk-4.1-dev build-essential`
- Rust 1.90+ + Node.js 18+

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

> release 构建会生成签名更新包（`bundle.createUpdaterArtifacts`），需要更新签名私钥，
> 见下方「自动更新」。仅调试前端时用 `npm run dev`，不触发签名。

## 自动更新

桌面端通过 Tauri updater 从 GitHub Releases 拉取签名更新：

- 更新源：`https://github.com/KomeiDiSanXian/remilia/releases/latest/download/latest.json`
- 校验公钥：`src-tauri/tauri.conf.json` 的 `plugins.updater.pubkey`
- 用户入口：托盘菜单「关于」→「检查更新」→「下载并安装」→「立即重启」

### 签名密钥（首次发布前准备一次）

```powershell
npx tauri signer generate -w "$env:USERPROFILE\.tauri\remilia-desktop.key"
```

- 打印出的公钥填进 `src-tauri/tauri.conf.json`（仓库已配置，换密钥时才需要改）；
- 私钥内容写入仓库 Secret `TAURI_SIGNING_PRIVATE_KEY`
  （生成时若设置了密码，再加 `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`）：

  ```powershell
  gh secret set TAURI_SIGNING_PRIVATE_KEY --body (Get-Content -Raw "$env:USERPROFILE\.tauri\remilia-desktop.key")
  ```

- 本地构建 release 包时同样要提供私钥，可直接指向文件：

  ```powershell
  $env:TAURI_SIGNING_PRIVATE_KEY_PATH = "$env:USERPROFILE\.tauri\remilia-desktop.key"
  make desktop-build
  ```

私钥丢失后旧版本将无法再校验新更新，只能让用户手动重装，请妥善保管。

### 发布流程

推送 `v*` 标签后（`.github/workflows/release.yml`）：

1. GoReleaser 创建 Release 并上传机器人二进制；
2. `Desktop (Windows)` job 重建 dashboard 与 sidecar、把桌面端版本号同步为标签版本
   （`desktop/scripts/sync-version.mjs`），再用 tauri-action 生成 NSIS/MSI 安装包、
   `*-setup.nsis.zip` 更新包、`.sig` 签名与 `latest.json`，上传到同一个 Release。

未配置 `TAURI_SIGNING_PRIVATE_KEY` 时桌面端 job 自动跳过，普通发布不受影响。
本地开发时版本号保持 `0.1.0`，只有 CI 会改写版本号（不提交）。

## 架构说明

```
desktop/src/           ← React 前端（引用 dashboard/src 的组件）
desktop/src-tauri/     ← Rust 后端（系统托盘、窗口管理、API 命令）
dashboard/src/         ← 共享的仪表盘组件（BotStatus, PluginList 等）
```

- 前端通过 Vite alias `@dashboard` 引用 `../dashboard/src` 的组件
- Rust 后端提供系统托盘、自定义标题栏、窗口隐藏到托盘等功能
- 通过 REST API 与 Go 后端通信（默认 http://localhost:9002）
