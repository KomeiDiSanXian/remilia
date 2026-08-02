// Package about 提供机器人自我介绍插件。
//
// 通过 /about（别名 /botinfo）展示机器人名称、框架版本、
// Git 提交、构建时间、仓库地址、运行时长等信息。
// 支持平台时使用 Markdown 渲染，并附带"查看命令列表"快捷按钮。
package about

import (
	"fmt"
	"runtime"
	"strings"
	"time"

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
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "查看机器人自身信息（版本、仓库、构建信息等）",
			Category:    "系统",
			Tags:        []string{"关于", "信息", "版本"},
			Repository:  RepositoryURL,
			Homepage:    RepositoryURL,
			HelpText: `查看机器人自身信息：
  /about             — 查看机器人基本信息
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
	md, text := p.buildInfo()
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
	if ctx.GetPlatformEvent().Content() != helpButtonID {
		return nil
	}
	ctx.Reply(platform.TextMessage("💡 使用 /help 查看所有可用命令"))
	return nil
}

// buildInfo 生成 Markdown 与纯文本两种形式的机器人介绍。
func (p *Plugin) buildInfo() (md, text string) {
	commit, buildDate := api.GetBuildInfo()
	if commit != "" && len(commit) > 7 {
		commit = commit[:7]
	}

	var pluginCount int
	if p.info != nil {
		pluginCount = p.info.Count()
	}
	uptime := time.Since(p.startTime)

	md = fmt.Sprintf("**🤖 Remilia**\n"+
		"开源的多平台聊天机器人框架\n\n"+
		"- **框架版本**: `%s`\n"+
		"- **Git 提交**: `%s`\n"+
		"- **构建时间**: %s\n"+
		"- **Go 版本**: %s\n"+
		"- **仓库**: [%s](%s)\n"+
		"- **已加载插件**: %d 个\n"+
		"- **运行时长**: %s\n\n"+
		"💡 使用 /help 查看所有可用命令",
		remilia.Version, orDash(commit), orDash(buildDate),
		runtime.Version(), RepositoryURL, RepositoryURL,
		pluginCount, formatDuration(uptime))

	text = fmt.Sprintf("🤖 Remilia\n"+
		"开源的多平台聊天机器人框架\n\n"+
		"框架版本: %s\n"+
		"Git 提交: %s\n"+
		"构建时间: %s\n"+
		"Go 版本: %s\n"+
		"仓库: %s\n"+
		"已加载插件: %d 个\n"+
		"运行时长: %s\n\n"+
		"💡 使用 /help 查看所有可用命令",
		remilia.Version, orDash(commit), orDash(buildDate),
		runtime.Version(), RepositoryURL,
		pluginCount, formatDuration(uptime))

	return md, text
}

// orDash 空字符串返回占位符 "—"。
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
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
