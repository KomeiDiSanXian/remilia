// Package about 提供机器人自我介绍插件。
//
// 通过 /about（别名 /botinfo）展示机器人名称、框架版本、
// Git 提交、构建时间、仓库地址、运行状态、命令统计及系统资源等信息。
// 支持平台时使用 Markdown 渲染，并附带"查看命令列表"快捷按钮。
package about

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/mem"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/api"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// RepositoryURL 是 Remilia 的开源仓库地址。
const RepositoryURL = "https://github.com/KomeiDiSanXian/remilia"

// helpButtonID 是"查看命令列表"按钮的回调标识符。
//
// 已验证 QQ webhook 模式下回调按钮（type=1）不可靠：互动事件可能
// 不被投递、PUT 成功后客户端仍显示"请求第三方失败"。因此 QQ 平台
// 使用 Command 字段（指令按钮 type=2，点击后插入 /help 发送），
// 不产生互动回调；ID 保留给 Discord/Telegram 等支持回调的平台。
const helpButtonID = "about:help"

// Plugin 自我介绍插件。
type Plugin struct {
	info      plugin.Info
	startTime time.Time
}

// New 创建自我介绍插件描述符。
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "about",
		Version: "1.1.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "查看机器人自身信息（版本、仓库、运行状态、系统资源等）",
			Category:    "系统",
			Tags:        []string{"关于", "信息", "版本"},
			Repository:  RepositoryURL,
			Homepage:    RepositoryURL,
			HelpText: `查看机器人自身信息：
  /about             — 查看机器人基本信息（版本、构建、运行状态、系统资源）
  /botinfo           — /about 的别名`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.info = ctx.Info
			p.startTime = time.Now()
			aboutDef := command.NewDef("about").
				Description("查看机器人基本信息").
				Alias("botinfo").
				Build()
			ctx.OnCommandDefWith("", "/about", aboutDef, p.handleAbout, eventctx.OnMentionedBotOrNoMentions())
			ctx.Reg.RegisterMatcher(string(platform.EventKindInteraction)).Handle(p.handleButtonClick)
			return p, nil
		},
	}
}

// handleAbout 处理 /about（及别名 /botinfo）命令。
func (p *Plugin) handleAbout(ctx *eventctx.Context) error {
	caps := ctx.GetPlatformCapabilities()
	md, text := p.buildInfo(ctx.GetBotName(), ctx.GetEventPlatform())
	msg := platform.OutboundMessage{Markdown: md, Text: text}
	if caps.Has(platform.CapButtons) {
		msg = msg.WithButtons(platform.Button{
			ID:      helpButtonID,
			Label:   "查看命令列表",
			Command: "/help",
			Style:   platform.ButtonStyleSecondary,
		})
	}
	ctx.Reply(msg)
	return nil
}

// handleButtonClick 处理"查看命令列表"按钮回调。
func (p *Plugin) handleButtonClick(ctx *eventctx.Context) error {
	if platform.Content(ctx.GetPlatformEvent()) != helpButtonID {
		return nil
	}
	ctx.Reply(platform.TextMessage("💡 使用 /help 查看所有可用命令"))
	return nil
}

// buildInfo 生成 Markdown 与纯文本两种形式的机器人介绍。
//
// botName 为机器人显示名称，platformName 为当前事件来源平台，
// 均由 handler 从事件上下文获取，保持本函数可独立测试。
func (p *Plugin) buildInfo(botName, platformName string) (md, text string) {
	commit, buildDate := api.GetBuildInfo()
	if commit != "" && len(commit) > 7 {
		commit = commit[:7]
	}

	var pluginCount, commandCount, matcherCount int
	if p.info != nil {
		pluginCount = p.info.Count()
		if coord := p.info.Coordinator(); coord != nil {
			commandCount = len(coord.GetAllCommands())
			matcherCount = coord.GetMatcherCount()
		}
	}
	uptime := time.Since(p.startTime)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	sysMem := "—"
	procMem := formatBytes(ms.Sys)
	if used, total, pct, ok := systemMemory(); ok {
		sysMem = fmt.Sprintf("%s / %s（%.1f%%）", formatBytes(used), formatBytes(total), pct)
		procMem = fmt.Sprintf("%s（占系统内存 %.2f%%）", formatBytes(ms.Sys),
			float64(ms.Sys)/float64(total)*100)
	}

	md = fmt.Sprintf("**🤖 Remilia**\n"+
		"开源的多平台聊天机器人框架\n\n"+
		"**框架信息**\n"+
		"- **框架版本**: `%s`\n"+
		"- **Git 提交**: `%s`\n"+
		"- **构建时间**: %s\n"+
		"- **Go 版本**: %s\n"+
		"- **仓库**: [%s](%s)\n\n"+
		"**运行状态**\n"+
		"- **机器人名称**: %s\n"+
		"- **当前平台**: %s\n"+
		"- **运行时长**: %s\n"+
		"- **已加载插件**: %d 个\n"+
		"- **注册命令**: %d 个\n"+
		"- **Matcher**: %d 个\n\n"+
		"**系统信息**\n"+
		"- **操作系统**: %s\n"+
		"- **CPU 核心**: %d 核\n"+
		"- **系统内存**: %s\n"+
		"- **进程内存**: %s\n"+
		"- **Goroutine**: %d\n\n"+
		"💡 使用 /help 查看所有可用命令",
		remilia.Version, orDash(commit), orDash(buildDate),
		runtime.Version(), RepositoryURL, RepositoryURL,
		orDash(botName), orDash(platformName), formatDuration(uptime),
		pluginCount, commandCount, matcherCount,
		runtime.GOOS+"/"+runtime.GOARCH, runtime.NumCPU(),
		sysMem, procMem, runtime.NumGoroutine())

	text = fmt.Sprintf("🤖 Remilia\n"+
		"开源的多平台聊天机器人框架\n\n"+
		"框架信息:\n"+
		"框架版本: %s\n"+
		"Git 提交: %s\n"+
		"构建时间: %s\n"+
		"Go 版本: %s\n"+
		"仓库: %s\n\n"+
		"运行状态:\n"+
		"机器人名称: %s\n"+
		"当前平台: %s\n"+
		"运行时长: %s\n"+
		"已加载插件: %d 个\n"+
		"注册命令: %d 个\n"+
		"Matcher: %d 个\n\n"+
		"系统信息:\n"+
		"操作系统: %s\n"+
		"CPU 核心: %d 核\n"+
		"系统内存: %s\n"+
		"进程内存: %s\n"+
		"Goroutine: %d\n\n"+
		"💡 使用 /help 查看所有可用命令",
		remilia.Version, orDash(commit), orDash(buildDate),
		runtime.Version(), RepositoryURL,
		orDash(botName), orDash(platformName), formatDuration(uptime),
		pluginCount, commandCount, matcherCount,
		runtime.GOOS+"/"+runtime.GOARCH, runtime.NumCPU(),
		sysMem, procMem, runtime.NumGoroutine())

	return md, text
}

// systemMemory 返回宿主机内存使用情况：已用、总量、占用百分比。
// 平台不支持或获取失败时 ok 为 false。
func systemMemory() (used, total uint64, percent float64, ok bool) {
	v, err := mem.VirtualMemory()
	if err != nil || v == nil || v.Total == 0 {
		return 0, 0, 0, false
	}
	return v.Used, v.Total, v.UsedPercent, true
}

// orDash 空字符串返回占位符 "—"。
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// formatBytes 将字节数格式化为人类可读的二进制单位（B/KB/MB/GB/TB）。
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatDuration 将时长格式化为 "Xd Xh Xm Xs"（0 值部分省略）。
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "刚刚启动"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d天", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d小时", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d分钟", minutes))
	}
	if seconds > 0 {
		parts = append(parts, fmt.Sprintf("%d秒", seconds))
	}
	return strings.Join(parts, " ")
}
