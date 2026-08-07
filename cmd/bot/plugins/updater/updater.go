// Package updater 提供机器人自动更新插件（应用级，仅随 cmd/bot 分发）。
//
// 命令:
//   - /update check     检查 GitHub Releases 是否有新版本
//   - /update status    查看更新器状态（版本/环境/上次检查/备份）
//   - /update now       立即下载 → 校验 → 替换 → 重启（--force 允许重装同版本）
//   - /update auto on|off  切换自动检查（默认开启）
//   - /update rollback  回滚到上一个备份版本并重启
//
// 流程安全说明：
//   - 下载资产必须通过 goreleaser 的 checksums.txt 校验
//   - 先完整下载校验、后替换；任何失败路径都不破坏正在运行的二进制
//   - 替换前备份旧二进制；新进程启动时校验版本，不匹配自动回滚
//   - 容器环境（/.dockerenv）自动禁用自更新，提示拉取新镜像
package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/permission"
	"github.com/KomeiDiSanXian/remilia/builtin/permission/permcheck"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// defaultRepo 默认更新源仓库（owner/repo）。
const defaultRepo = "KomeiDiSanXian/remilia"

// Plugin 自动更新插件实例。
type Plugin struct {
	cfg     plugin.ConfigReader
	dataDir string

	permSvc *permission.Plugin

	client    *githubClient
	state     *stateStore
	updating  atomic.Bool // 单飞：同一时间只允许一个更新/回滚流程
	autoCheck atomic.Bool // 后台自动检查开关（运行时状态，持久化）
}

// Option 是 updater 插件的构造选项。
type Option func(*Plugin)

// shutdownHook 由 cmd/bot 在启动时注入（SetShutdownHook）：
// 触发主进程的优雅关闭（bot.Shutdown → 插件 Teardown → LevelDB 等数据文件句柄
// 确定性释放）。Windows 上 os.Exit(0) 是粗暴退出，内核关闭句柄的时序不可依赖——
// 子进程检测到退出代码时父进程的文件可能仍未释放（实测：子进程"等待 0s"继续
// 启动，父进程数秒后才完成退出，LevelDB 打开报"文件被另一个进程占用"）。
// 父进程先优雅关闭再退出，子进程无需任何时序猜测即可安全打开数据文件。
var shutdownHook func()

// SetShutdownHook 注入"更新后退出前"的优雅关闭回调（cmd/bot 在构建 Bot 后调用）。
// 回调在 triggerSelfShutdown 内、进程退出前执行一次；未注入时退化为直接退出
// （此时子进程侧的等待宽限作为兜底）。
func SetShutdownHook(fn func()) {
	shutdownHook = fn
}

// WithDataDir 设置状态数据目录（标记文件、状态文件）。
func WithDataDir(dir string) Option {
	return func(p *Plugin) { p.dataDir = dir }
}

// New 创建自动更新插件的 Descriptor。
func New(opts ...Option) *plugin.Descriptor {
	p := &Plugin{}
	for _, o := range opts {
		o(p)
	}

	return &plugin.Descriptor{
		Name:         "updater",
		Version:      "1.0.0",
		Privileged:   true,
		OptionalDeps: []string{"permission"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "自动更新：从 GitHub Releases 检查、下载并替换自身二进制",
			Category:    "系统",
			Tags:        []string{"更新", "升级", "自更新"},
			HelpText: `自动更新管理：
  /update check           — 检查 GitHub Releases 是否有新版本
  /update status          — 查看更新器状态（版本/环境/上次检查/备份）
  /update now [--force]   — 立即更新并重启（--force 允许重装同版本）
  /update auto on|off     — 切换自动检查
  /update rollback        — 回滚到上一个备份版本并重启

权限：superadmin 角色或 updater.manage 权限。
容器环境（Docker）自动禁用自更新。`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.cfg = ctx.Config
			if svc, ok := plugin.TryService[*permission.Plugin](ctx, "permission"); ok {
				p.permSvc = svc
			}
			if p.dataDir == "" {
				p.dataDir = "data/updater"
			}

			owner, repo := splitRepo(p.cfg.GetString("repo", defaultRepo))
			timeout := p.cfg.GetDuration("timeout", 10*time.Minute)
			p.client = newGitHubClient(owner, repo, p.cfg.GetString("proxy", ""), timeout)
			p.state = newStateStore(p.dataDir)

			// 拉起子进程的控制台策略（""=安全默认 NUL 输出；"new"=独立控制台窗口；"file"=重定向到文件）
			childConsoleMode = p.cfg.GetString("child_console", "")
			childLogPath = filepath.Join(p.dataDir, "child.log")

			st := p.state.load()
			p.autoCheck.Store(st.AutoCheck)

			p.registerCommands(ctx)

			if !ctx.DryRun {
				ctx.Spawn(p.autoCheckLoop)
			}
			return p, nil
		},
	}
}

func (p *Plugin) registerCommands(ctx *plugin.SetupContext) {
	updateCmd := &command.Definition{
		Name:        "update",
		Description: "自动更新管理",
		Usage:       "/update <check|status|now|auto|rollback>",
		Category:    "系统",
		SubCommands: []*command.Definition{
			{Name: "check", Description: "检查 GitHub Releases 是否有新版本", Usage: "/update check", Examples: []string{"/update check"}},
			{Name: "status", Description: "查看更新器状态", Usage: "/update status", Examples: []string{"/update status"}},
			{Name: "now", Description: "立即更新并重启", Usage: "/update now [--force]", Examples: []string{"/update now", "/update now --force"}},
			{Name: "auto", Description: "切换自动检查", Usage: "/update auto on|off", Examples: []string{"/update auto on"}},
			{Name: "rollback", Description: "回滚到上一个备份版本", Usage: "/update rollback", Examples: []string{"/update rollback"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/update").SetDefinition(updateCmd).Handle(p.handleUpdate)
}

// handleUpdate 处理 /update 命令。
func (p *Plugin) handleUpdate(ctx *eventctx.Context) error {
	if !p.checkPermission(ctx) {
		ctx.ReplyText("权限不足：需要 superadmin 角色或 updater.manage 权限")
		return nil
	}

	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.ReplyText("用法: /update check|status|now [--force]|auto on|off|rollback")
		return nil
	}

	switch strings.ToLower(args[1]) {
	case "check":
		p.handleCheck(ctx)
	case "status":
		p.handleStatus(ctx)
	case "now":
		force := false
		for _, a := range args[2:] {
			if strings.EqualFold(a, "--force") || strings.EqualFold(a, "-f") {
				force = true
				break
			}
		}
		p.handleNow(ctx, force)
	case "auto":
		p.handleAuto(ctx, args)
	case "rollback":
		p.handleRollback(ctx)
	default:
		ctx.ReplyText("用法: /update check|status|now [--force]|auto on|off|rollback")
	}
	return nil
}

// checkPermission 更新相关命令要求 superadmin 角色或 updater.manage 权限。
func (p *Plugin) checkPermission(ctx *eventctx.Context) bool {
	if p.permSvc == nil {
		return true
	}
	if permcheck.HasPermission(p.permSvc, ctx, "updater.manage") {
		return true
	}
	return slices.Contains(p.permSvc.GetUserRoles(ctx.GetUserID()), "superadmin")
}

// ── /update check ──────────────────────────────────────────────────────

func (p *Plugin) handleCheck(ctx *eventctx.Context) {
	ctx.ReplyText("🔍 正在检查更新...")
	rel, err := p.fetchLatest(ctx.Context())
	if err != nil {
		ctx.ReplyError("检查失败: " + err.Error())
		return
	}
	toVer, err := parseVersion(rel.TagName)
	if err != nil {
		ctx.ReplyError("远端版本号格式异常: " + err.Error())
		return
	}
	p.state.update(func(st *updaterState) {
		st.LastCheck = time.Now()
		st.LastVersion = toVer.String()
	})

	current := CurrentVersion()
	cmp := 0
	if cur, err := parseVersion(current); err == nil {
		cmp = toVer.Compare(cur)
	}
	switch {
	case cmp > 0:
		ctx.ReplyText(fmt.Sprintf("发现新版本：当前 v%s → 最新 v%s。发送 /update now 开始更新。",
			current, toVer))
	case cmp == 0:
		ctx.ReplyText(fmt.Sprintf("已是最新版本 v%s", current))
	default:
		ctx.ReplyText(fmt.Sprintf("远端版本 v%s 不高于当前 v%s（--force 可强制重装）", toVer, current))
	}
}

// ── /update status ─────────────────────────────────────────────────────

func (p *Plugin) handleStatus(ctx *eventctx.Context) {
	st := p.state.load()

	var sb strings.Builder
	fmt.Fprintf(&sb, "📦 更新器状态\n")
	fmt.Fprintf(&sb, "当前版本: v%s\n", CurrentVersion())
	if p.client != nil {
		fmt.Fprintf(&sb, "更新源: %s/%s\n", p.client.owner, p.client.repo)
	}
	fmt.Fprintf(&sb, "自动检查: %s\n", onOff(p.autoCheck.Load()))
	if !st.LastCheck.IsZero() {
		fmt.Fprintf(&sb, "上次检查: %s（发现 v%s）\n", st.LastCheck.Format("2006-01-02 15:04:05"), st.LastVersion)
	} else {
		fmt.Fprintf(&sb, "上次检查: 尚未检查\n")
	}
	if st.Applied != "" {
		fmt.Fprintf(&sb, "最近更新: v%s\n", st.Applied)
	}

	container := inContainer()
	fmt.Fprintf(&sb, "运行环境: %s/%s%s\n", runtime.GOOS, runtime.GOARCH, func() string {
		if container {
			return "（容器，自更新已禁用）"
		}
		return ""
	}())

	backup := p.latestBackup()
	if backup != "" {
		fmt.Fprintf(&sb, "备份: %s\n", filepath.Base(backup))
	} else {
		fmt.Fprintf(&sb, "备份: 无\n")
	}

	if container && p.cfg.GetBool("disable_in_container", true) {
		sb.WriteString("⚠️ 容器环境：请拉取新镜像（docker pull / docker-compose pull）更新\n")
	}
	ctx.ReplyText(sb.String())
}

// ── /update now ────────────────────────────────────────────────────────

func (p *Plugin) handleNow(ctx *eventctx.Context, force bool) {
	if container := inContainer(); container && p.cfg.GetBool("disable_in_container", true) {
		ctx.ReplyText("⚠️ 检测到容器环境，自更新已禁用。请使用 docker pull / docker-compose pull 拉取新镜像。")
		return
	}
	if !p.updating.CompareAndSwap(false, true) {
		ctx.ReplyText("已有更新流程正在进行，请稍候")
		return
	}
	defer p.updating.Store(false)

	reply := func(msg string) {
		ctx.ReplyText(msg)
	}

	if err := p.applyUpdate(ctx.Context(), force, reply); err != nil {
		ctx.ReplyError(err.Error())
		return
	}
	// 最终消息必须等待发送完成后再关闭进程。
	// 用独立超时 ctx（事件 ctx 可能被中间件注入短 deadline，导致消息未发出就重启）
	waitCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	ctx.ReplyText("🚀 更新完成，机器人即将重启（约 30 秒）...").Wait(waitCtx)
	cancel()
	logger.Infof("[updater] 旧进程退出中（pid=%d，T=%s）", os.Getpid(), time.Now().Format("15:04:05.000"))
	triggerSelfShutdown()
}

// ── /update auto ───────────────────────────────────────────────────────

func (p *Plugin) handleAuto(ctx *eventctx.Context, args []string) {
	if len(args) < 3 {
		ctx.ReplyText("用法: /update auto on|off")
		return
	}
	var on bool
	switch strings.ToLower(args[2]) {
	case "on":
		on = true
	case "off":
		on = false
	default:
		ctx.ReplyText("用法: /update auto on|off")
		return
	}
	p.autoCheck.Store(on)
	p.state.update(func(st *updaterState) { st.AutoCheck = on })
	ctx.ReplyText(fmt.Sprintf("自动检查已%s", onOff(on)))
}

// ── /update rollback ───────────────────────────────────────────────────

func (p *Plugin) handleRollback(ctx *eventctx.Context) {
	if container := inContainer(); container && p.cfg.GetBool("disable_in_container", true) {
		ctx.ReplyText("⚠️ 检测到容器环境，自更新已禁用。请使用 docker pull 拉取历史镜像。")
		return
	}
	if !p.updating.CompareAndSwap(false, true) {
		ctx.ReplyText("已有更新流程正在进行，请稍候")
		return
	}
	defer p.updating.Store(false)

	exePath, err := os.Executable()
	if err != nil {
		ctx.ReplyError("无法定位可执行文件: " + err.Error())
		return
	}
	backup := p.latestBackup()
	if backup == "" {
		ctx.ReplyText("没有可用的备份（当前未进行过更新）")
		return
	}
	backupVer := backupVersion(backup)

	sw := &swapper{backup: true}
	ctx.ReplyText("🔄 正在回滚到 v" + backupVer + "...")
	if err := sw.restore(exePath, backup); err != nil {
		ctx.ReplyError("回滚失败: " + err.Error())
		return
	}

	markerPath := filepath.Join(p.dataDir, pendingFileName)
	pending := &PendingUpdate{
		FromVersion: CurrentVersion(),
		ToVersion:   backupVer,
		BackupPath:  backup,
		ExePath:     exePath,
		At:          time.Now(),
	}
	if err := writePending(markerPath, pending); err != nil {
		// 注意：restore 已完成，磁盘上是旧版本；标记缺失仅影响下次启动的确认流程
		ctx.ReplyError("二进制已回滚到 v" + backupVer + "，但写入更新标记失败（下次启动将直接运行旧版本）: " + err.Error())
		return
	}
	if err := spawnNewProcess(exePath, markerPath); err != nil {
		_ = removePending(markerPath)
		ctx.ReplyError("启动新进程失败: " + err.Error())
		return
	}

	p.state.update(func(st *updaterState) { st.Applied = backupVer })
	ctx.ReplyText("✅ 已回滚到 v" + backupVer + "，机器人即将重启...").Wait(ctx.Context())
	triggerSelfShutdown()
}

// ── 核心更新流程 ───────────────────────────────────────────────────────

// applyUpdate 执行完整更新流程：检查 → 下载 → 校验 → 替换 → 标记 → 拉起新进程。
// reply 用于向用户逐步汇报进度（命令路径回复消息，后台路径记日志）。
// 任何失败都会让当前进程保持原样运行，绝不破坏在用的二进制。
func (p *Plugin) applyUpdate(ctx context.Context, force bool, reply func(string)) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位可执行文件: %w", err)
	}
	return p.applyUpdateTo(ctx, exePath, force, reply)
}

// applyUpdateTo 是 applyUpdate 的核心（exePath 可注入，便于测试）。
func (p *Plugin) applyUpdateTo(ctx context.Context, exePath string, force bool, reply func(string)) error {
	// 体检：其他同名实例仍在运行时，更新后的新进程会因数据文件（LevelDB 排他锁）
	// 被占用而启动失败（"文件被另一个进程占用"）。警告但继续——用户可能确有多实例诉求。
	if others := otherInstances(filepath.Base(exePath)); len(others) > 0 {
		msg := fmt.Sprintf("⚠️ 检测到其他 remilia 实例（pid=%v）正在运行。多实例共用 data 目录时，更新后的新进程可能因数据文件被占用而无法启动，建议先停止其他实例", others)
		reply(msg)
		logger.Warnf("[updater] %s", msg)
	}

	reply("🔍 正在检查更新...")
	rel, err := p.fetchLatest(ctx)
	if err != nil {
		return fmt.Errorf("检查更新失败: %w", err)
	}
	toVer, err := parseVersion(rel.TagName)
	if err != nil {
		return fmt.Errorf("远端版本号格式异常: %w", err)
	}
	current := CurrentVersion()
	curVer, curErr := parseVersion(current)
	if curErr == nil && toVer.Compare(curVer) <= 0 && !force {
		return fmt.Errorf("已是最新版本 v%s（如需重装请使用 --force）", current)
	}

	p.state.update(func(st *updaterState) {
		st.LastCheck = time.Now()
		st.LastVersion = toVer.String()
	})

	exeDir := filepath.Dir(exePath)
	tmpDir := filepath.Join(exeDir, ".updater-tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	assetName := expectedAssetName(runtime.GOOS, runtime.GOARCH, goarm())
	asset, ok := rel.Asset(assetName)
	if !ok {
		return fmt.Errorf("该版本没有当前平台的发布包（%s）", assetName)
	}
	sumAsset, sumOK := rel.Asset("checksums.txt")
	if !sumOK {
		return fmt.Errorf("该版本缺少 checksums.txt，已中止（安全策略）")
	}

	reply("⬇️ 正在下载 v" + toVer.String() + " ...")
	sumsPath := filepath.Join(tmpDir, "checksums.txt")
	if _, err := downloadFile(ctx, p.client.hc, sumAsset.BrowserDownloadURL, sumsPath); err != nil {
		return err
	}
	f, err := os.Open(sumsPath)
	if err != nil {
		return fmt.Errorf("打开校验文件失败: %w", err)
	}
	sums, err := parseChecksums(f)
	f.Close()
	if err != nil {
		return err
	}
	want, ok := sums[assetName]
	if !ok {
		return fmt.Errorf("checksums.txt 中没有 %s 的条目，已中止", assetName)
	}

	archivePath := filepath.Join(tmpDir, assetName)
	if _, err := downloadFile(ctx, p.client.hc, asset.BrowserDownloadURL, archivePath); err != nil {
		return err
	}
	if err := verifyFileSHA256(archivePath, want); err != nil {
		return fmt.Errorf("完整性校验失败: %w", err)
	}
	reply("✅ 下载完成，sha256 校验通过")

	wantName := "remilia"
	if runtime.GOOS == "windows" {
		wantName = "remilia.exe"
	}
	newBinary, err := extractBinary(archivePath, tmpDir, wantName)
	if err != nil {
		return err
	}

	reply("🔄 正在替换二进制...")
	sw := &swapper{backup: p.cfg.GetBool("backup", true)}

	// 先写标记、后替换：任何失败路径要么二进制未动，要么标记在场
	// （新进程启动时校验版本，不匹配自动回滚），不存在"无标记的新二进制"状态。
	// 注意：BackupPath 由 backupPathFor 计算，与 swap 内部生成的路径公式一致，
	// 只需写入一次（备份未启用时置空）。
	markerPath := filepath.Join(p.dataDir, pendingFileName)
	pending := &PendingUpdate{
		FromVersion: current,
		ToVersion:   toVer.String(),
		ExePath:     exePath,
		At:          time.Now(),
	}
	if sw.backup {
		pending.BackupPath = backupPathFor(exePath, current)
	}
	if err := writePending(markerPath, pending); err != nil {
		return fmt.Errorf("写入更新标记失败: %w", err)
	}

	backupPath, err := sw.swap(exePath, newBinary, current)
	if err != nil {
		_ = removePending(markerPath)
		return fmt.Errorf("替换二进制失败: %w", err)
	}

	if err := spawnNewProcess(exePath, markerPath); err != nil {
		_ = removePending(markerPath)
		if backupPath != "" {
			_ = sw.restore(exePath, backupPath)
			return fmt.Errorf("启动新进程失败，已回滚: %w", err)
		}
		return fmt.Errorf("启动新进程失败（未启用备份，二进制已替换，下次启动生效）: %w", err)
	}

	p.state.update(func(st *updaterState) { st.Applied = toVer.String() })
	logger.Infof("[updater] 更新到 v%s，新进程已启动", toVer)
	return nil
}

// fetchLatest 获取最新 Release（按配置决定是否接受预发布版）。
func (p *Plugin) fetchLatest(ctx context.Context) (*Release, error) {
	return p.client.latestRelease(ctx, p.cfg.GetBool("allow_prerelease", false))
}

// ── 后台自动检查 ───────────────────────────────────────────────────────

// minCheckInterval 自动检查的最小间隔：GitHub 匿名 API 限流 60 次/小时，
// 更小的间隔必然触发限流导致检查永久失败。
const minCheckInterval = 10 * time.Minute

// autoCheckLoop 按 check_interval 周期检查更新（0 或关闭时休眠到生命周期结束）。
// 发现新版本时记日志；auto_apply 开启时后台自动执行完整更新。
// interval 与开关每次循环热读（支持配置热更新与 /update auto 运行时切换）。
func (p *Plugin) autoCheckLoop(lctx context.Context) {
	for {
		interval := p.cfg.GetDuration("check_interval", 6*time.Hour)
		if interval > 0 && interval < minCheckInterval {
			logger.Warnf("[updater] check_interval %s 低于下限 %s，已钳制", interval, minCheckInterval)
			interval = minCheckInterval
		}
		if interval <= 0 {
			select {
			case <-lctx.Done():
				return
			case <-time.After(time.Hour):
				continue
			}
		}

		timer := time.NewTimer(interval)
		select {
		case <-lctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		timer.Stop()

		if !p.autoCheck.Load() {
			continue
		}
		p.runAutoCheck(lctx)
	}
}

// runAutoCheck 执行一次后台检查。lctx 为插件生命周期上下文，
// 机器人关闭/插件卸载时自动取消进行中的检查与下载。
func (p *Plugin) runAutoCheck(lctx context.Context) {
	rel, err := p.fetchLatest(lctx)
	if err != nil {
		if lctx.Err() != nil {
			return // 生命周期结束导致的取消，不算失败
		}
		logger.Warnf("[updater] 自动检查失败: %v", err)
		return
	}
	toVer, err := parseVersion(rel.TagName)
	if err != nil {
		return
	}
	current := CurrentVersion()
	curVer, curErr := parseVersion(current)
	if curErr == nil && toVer.Compare(curVer) <= 0 {
		return
	}
	p.state.update(func(st *updaterState) {
		st.LastCheck = time.Now()
		st.LastVersion = toVer.String()
	})

	logger.Infof("[updater] 发现新版本 v%s（当前 v%s）", toVer, current)
	if !p.cfg.GetBool("auto_apply", false) {
		return
	}
	if container := inContainer(); container && p.cfg.GetBool("disable_in_container", true) {
		logger.Warnf("[updater] 容器环境跳过自动更新")
		return
	}
	if !p.updating.CompareAndSwap(false, true) {
		return
	}
	defer p.updating.Store(false)
	logReply := func(msg string) { logger.Infof("[updater] %s", msg) }
	if err := p.applyUpdate(lctx, false, logReply); err != nil {
		if lctx.Err() == nil {
			logger.WithError(err).Error("[updater] 自动更新失败")
		}
		return
	}
	triggerSelfShutdown()
}

// ── 备份定位 ───────────────────────────────────────────────────────────

// latestBackup 返回 exe 同目录下版本号最新的 .old.* 备份路径（不存在返回空串）。
func (p *Plugin) latestBackup() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	base := filepath.Base(exePath)
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(exePath), base+".old.*"))
	if len(matches) == 0 {
		return ""
	}
	// 按版本号排序取最大（1.10.0 > 1.9.0，不能按文件名字典序）
	best := ""
	var bestVer version
	for _, m := range matches {
		v, err := parseVersion(backupVersion(m))
		if err != nil {
			continue
		}
		if best == "" || v.Compare(bestVer) > 0 {
			best, bestVer = m, v
		}
	}
	if best == "" {
		// 全部无法解析时兜底取字典序最后一个
		slices.Sort(matches)
		return matches[len(matches)-1]
	}
	return best
}

// backupVersion 从备份文件名（remilia.old.v1.30.0）解析版本号。
func backupVersion(backupPath string) string {
	base := filepath.Base(backupPath)
	_, after, ok := strings.Cut(base, ".old.")
	if !ok {
		return "unknown"
	}
	return strings.TrimPrefix(after, "v")
}

// splitRepo 将 "owner/repo" 拆分为 owner 与 repo。
func splitRepo(s string) (owner, repo string) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1]
	}
	return defaultRepo[:strings.Index(defaultRepo, "/")], defaultRepo[strings.Index(defaultRepo, "/")+1:]
}

// expectedAssetName 按 goreleaser name_template 计算当前平台的资产文件名：
//
//	{{ .ProjectName }}_{{ title .Os }}_{{ .Arch }}（windows 为 .zip，其余 .tar.gz）
//
// 注意：项目发布产物必须是 release 构建（linux/amd64 等）；dev 直跑不会命中。
func expectedAssetName(goos, goarch, goarm string) string {
	osPart := strings.ToUpper(goos[:1]) + goos[1:] // title: linux→Linux
	arch := goarch
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "386":
		arch = "i386"
	case "arm":
		if goarm == "" {
			goarm = "7"
		}
		arch = "armv" + goarm
	}
	name := "remilia_" + osPart + "_" + arch
	if goos == "windows" {
		return name + ".zip"
	}
	return name + ".tar.gz"
}

// goarm 返回构建时的 GOARM 值（runtime.GOARM 未导出，从 build info 读取）。
// 非 ARM 构建或信息缺失时返回空串，由调用方按默认值处理。
func goarm() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "GOARM" {
				return s.Value
			}
		}
	}
	return ""
}

func onOff(b bool) string {
	if b {
		return "✅ 开启"
	}
	return "❌ 关闭"
}
