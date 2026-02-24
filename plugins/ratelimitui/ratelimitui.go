// Package ratelimitui 提供限流状态查询插件。
//
// 聚合 antispam（用户/群限速 + 封禁）和 cooldown（命令冷却）的运行时状态，
// 通过管理命令暴露给运营人员，无需查看日志即可识别异常用户。
//
// 功能：
//   - 查看封禁用户列表（antispam）
//   - 查看冷却中的用户（cooldown）
//   - 查看限流统计摘要
//   - 支持按用户 ID 查询状态
//
// 使用示例:
//
//	pm.RegisterV2(ratelimitui.New())
//
//	// 或持有引用：
//	p := ratelimitui.NewPlugin()
//	pm.RegisterV2(ratelimitui.Descriptor(p))
package ratelimitui

import (
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/antispam"
	"github.com/KomeiDiSanXian/remilia/plugins/cooldown"
)

// Plugin 限流状态查询插件
type Plugin struct {
	antispam *antispam.Plugin
	cooldown *cooldown.Plugin
	setupCtx *plugin.SetupContext
}

// NewPlugin 创建 Plugin 实例
func NewPlugin() *Plugin {
	return &Plugin{}
}

// New 创建限流状态查询插件描述符（便捷入口）
func New() *plugin.PluginDescriptor {
	return Descriptor(NewPlugin())
}

// Descriptor 从已有 Plugin 实例创建描述符
func Descriptor(p *Plugin) *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:    "ratelimitui",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.PluginMeta{
			Author:      "Remilia Team",
			Description: "限流状态查询插件，聚合 antispam 和 cooldown 的运行时状态",
			Category:    "运营",
			Tags:        []string{"限流", "监控", "查询", "运营"},
			HelpText: `限流状态查询插件使用说明：
  /rl status [用户ID]
  /rl bans
  /rl stats
  /rl unban <用户ID>
  /rl reset <用户ID> <命令>`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			p.setupCtx = ctx
			if raw, ok := ctx.Get("antispam"); ok {
				if ap, ok := raw.(*antispam.Plugin); ok {
					p.antispam = ap
					ctx.Log.Info("Bound to antispam plugin")
				}
			}
			if raw, ok := ctx.Get("cooldown"); ok {
				if cp, ok := raw.(*cooldown.Plugin); ok {
					p.cooldown = cp
					ctx.Log.Info("Bound to cooldown plugin")
				}
			}
			p.registerCommands(ctx)
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Plugin unloaded")
			return nil
		},
	}
}

func (p *Plugin) registerCommands(ctx *plugin.SetupContext) {
	rlCmd := &command.Definition{
		Name:        "rl",
		Description: "限流状态查询",
		Usage:       "/rl <子命令> [参数]",
		Category:    "运营",
		SubCommands: []*command.Definition{
			{
				Name:        "status",
				Description: "查询用户限流状态",
				Usage:       "/rl status [用户ID]",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID（可选，不填则查询自身）", Required: false},
				},
				Examples: []string{"/rl status", "/rl status USER123"},
			},
			{
				Name:        "bans",
				Description: "列出所有封禁用户",
				Usage:       "/rl bans",
				Examples:    []string{"/rl bans"},
			},
			{
				Name:        "stats",
				Description: "查看限流统计摘要",
				Usage:       "/rl stats",
				Examples:    []string{"/rl stats"},
			},
			{
				Name:        "unban",
				Description: "解封用户",
				Usage:       "/rl unban <用户ID>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
				},
				Examples: []string{"/rl unban USER123"},
			},
			{
				Name:        "reset",
				Description: "重置用户命令冷却时间",
				Usage:       "/rl reset <用户ID> <命令>",
				Arguments: []*command.Argument{
					{Name: "userID", Type: command.ArgTypeString, Description: "用户ID", Required: true},
					{Name: "cmd", Type: command.ArgTypeString, Description: "命令名", Required: true},
				},
				Examples: []string{"/rl reset USER123 daily"},
			},
		},
	}

	ctx.Reg.RegisterCommand(dto.C2CMessageCreate, "/rl").
		SetDefinition(rlCmd).
		Handle(p.handleRLCommand)
}

func (p *Plugin) handleRLCommand(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	args, err := command.ParseCommandLine(content)
	if err != nil {
		return p.reply(ctx, "❌ 命令解析失败: "+err.Error())
	}

	sub := args.Get(0)
	if sub == "" {
		return p.showHelp(ctx)
	}

	switch sub {
	case "status":
		return p.handleStatus(ctx, args)
	case "bans":
		return p.handleBans(ctx)
	case "stats":
		return p.handleStats(ctx)
	case "unban":
		return p.handleUnban(ctx, args)
	case "reset":
		return p.handleReset(ctx, args)
	default:
		return p.reply(ctx, fmt.Sprintf("❌ 未知子命令: %s\n使用 /rl 查看帮助", sub))
	}
}

func (p *Plugin) showHelp(ctx *eventctx.Context) error {
	var msg strings.Builder
	msg.WriteString("📊 限流状态查询\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	msg.WriteString("可用命令:\n")
	msg.WriteString("  /rl status [用户ID]  - 查询用户限流状态\n")
	msg.WriteString("  /rl bans             - 列出所有封禁用户\n")
	msg.WriteString("  /rl stats            - 查看统计摘要\n")
	msg.WriteString("  /rl unban <用户ID>   - 解封用户\n")
	msg.WriteString("  /rl reset <用户ID> <命令> - 重置冷却\n")
	return p.reply(ctx, msg.String())
}

// handleStatus 查询指定用户的限流状态
func (p *Plugin) handleStatus(ctx *eventctx.Context, args *command.Args) error {
	userID := args.Get(1)
	if userID == "" {
		userID = ctx.GetUserID()
	}
	if userID == "" {
		return p.reply(ctx, "❌ 无法获取用户ID，请手动指定: /rl status <用户ID>")
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("📊 用户 %s 的限流状态\n", userID))
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	// antispam 封禁状态
	if p.antispam != nil {
		banned := p.antispam.IsBanned(userID)
		if banned {
			msg.WriteString("🚫 封禁状态: 已封禁\n")
		} else {
			msg.WriteString("✅ 封禁状态: 正常\n")
		}
	} else {
		msg.WriteString("⚠️ antispam 插件未加载\n")
	}

	msg.WriteString("\n")

	// cooldown 信息（展示所有关联的冷却记录）
	if p.cooldown != nil {
		records := p.cooldown.QueryUser(userID)
		if len(records) == 0 {
			msg.WriteString("⏱ 冷却记录: 无\n")
		} else {
			msg.WriteString(fmt.Sprintf("⏱ 冷却记录 (%d 条):\n", len(records)))
			for _, r := range records {
				msg.WriteString(fmt.Sprintf("  • %s: 最后使用 %s 前\n",
					r.Command, time.Since(r.LastUsed).Round(time.Second)))
			}
		}
	} else {
		msg.WriteString("⚠️ cooldown 插件未加载\n")
	}

	return p.reply(ctx, msg.String())
}

// handleBans 列出所有封禁用户
func (p *Plugin) handleBans(ctx *eventctx.Context) error {
	if p.antispam == nil {
		return p.reply(ctx, "❌ antispam 插件未加载，无法查询封禁列表")
	}

	bans := p.antispam.ListBans()
	if len(bans) == 0 {
		return p.reply(ctx, "✅ 当前没有封禁用户")
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🚫 封禁用户列表 (共 %d 人)\n", len(bans)))
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")
	for i, b := range bans {
		msg.WriteString(fmt.Sprintf("%d. %s", i+1, b.UserID))
		if b.Permanent {
			msg.WriteString(" (永久)")
		} else {
			remaining := time.Until(b.Until).Round(time.Second)
			if remaining > 0 {
				msg.WriteString(fmt.Sprintf(" (剩余 %s)", remaining))
			} else {
				msg.WriteString(" (即将解除)")
			}
		}
		msg.WriteString("\n")
	}
	msg.WriteString(fmt.Sprintf("\n💡 使用 /rl unban <用户ID> 解封"))
	return p.reply(ctx, msg.String())
}

// handleStats 查看限流统计摘要
func (p *Plugin) handleStats(ctx *eventctx.Context) error {
	var msg strings.Builder
	msg.WriteString("📊 限流统计摘要\n")
	msg.WriteString(strings.Repeat("=", 40) + "\n\n")

	if p.antispam != nil {
		stats := p.antispam.Stats()
		msg.WriteString("🛡️ AntiSpam:\n")
		msg.WriteString(fmt.Sprintf("  • 封禁用户: %d 人\n", stats.BanCount))
		msg.WriteString(fmt.Sprintf("  • 限速桶 (用户): %d 个\n", stats.UserLimiterCount))
		msg.WriteString(fmt.Sprintf("  • 限速桶 (群组): %d 个\n", stats.GroupLimiterCount))
	} else {
		msg.WriteString("⚠️ antispam 插件未加载\n")
	}

	msg.WriteString("\n")

	if p.cooldown != nil {
		count := p.cooldown.ActiveCount()
		msg.WriteString("⏱️ Cooldown:\n")
		msg.WriteString(fmt.Sprintf("  • 活跃冷却记录: %d 条\n", count))
	} else {
		msg.WriteString("⚠️ cooldown 插件未加载\n")
	}

	return p.reply(ctx, msg.String())
}

// handleUnban 解封用户
func (p *Plugin) handleUnban(ctx *eventctx.Context, args *command.Args) error {
	if p.antispam == nil {
		return p.reply(ctx, "❌ antispam 插件未加载")
	}
	userID := args.Get(1)
	if userID == "" {
		return p.reply(ctx, "用法: /rl unban <用户ID>")
	}
	p.antispam.Unban(userID)
	return p.reply(ctx, fmt.Sprintf("✅ 已解封用户: %s", userID))
}

// handleReset 重置用户命令冷却时间
func (p *Plugin) handleReset(ctx *eventctx.Context, args *command.Args) error {
	if p.cooldown == nil {
		return p.reply(ctx, "❌ cooldown 插件未加载")
	}
	userID := args.Get(1)
	cmdName := args.Get(2)
	if userID == "" || cmdName == "" {
		return p.reply(ctx, "用法: /rl reset <用户ID> <命令>")
	}
	p.cooldown.Reset(userID, cmdName)
	return p.reply(ctx, fmt.Sprintf("✅ 已重置用户 %s 对命令 %s 的冷却时间", userID, cmdName))
}

func (p *Plugin) reply(ctx *eventctx.Context, content string) error {
	msg := &dto.Message{Type: dto.TextMessage, Content: content}
	_, err := ctx.ReplyPrivate(msg)
	return err
}

// ---- Public API (also useful for tests and other plugins) ------------------

// BindAntispam manually binds an antispam plugin (called by Setup automatically,
// or manually in tests / programmatic usage).
func (p *Plugin) BindAntispam(ap *antispam.Plugin) {
	p.antispam = ap
}

// BindCooldown manually binds a cooldown plugin.
func (p *Plugin) BindCooldown(cp *cooldown.Plugin) {
	p.cooldown = cp
}

// HasAntispam returns true if an antispam plugin is bound.
func (p *Plugin) HasAntispam() bool { return p.antispam != nil }

// HasCooldown returns true if a cooldown plugin is bound.
func (p *Plugin) HasCooldown() bool { return p.cooldown != nil }

// BanSummary holds a ban record returned by ListBanSummary.
type BanSummary struct {
	UserID    string
	Permanent bool
	Until     time.Time
}

// ListBanSummary returns all active bans from the antispam plugin.
// Returns nil and no error if antispam is not bound.
func (p *Plugin) ListBanSummary() []BanSummary {
	if p.antispam == nil {
		return nil
	}
	raw := p.antispam.ListBans()
	out := make([]BanSummary, len(raw))
	for i, b := range raw {
		out[i] = BanSummary{UserID: b.UserID, Permanent: b.Permanent, Until: b.Until}
	}
	return out
}

// Stats holds the aggregated rate-limit statistics.
type Stats struct {
	BanCount          int
	UserLimiterCount  int
	GroupLimiterCount int
	ActiveCooldowns   int
}

// GetStats returns the aggregated rate-limit statistics.
func (p *Plugin) GetStats() Stats {
	s := Stats{}
	if p.antispam != nil {
		as := p.antispam.Stats()
		s.BanCount = as.BanCount
		s.UserLimiterCount = as.UserLimiterCount
		s.GroupLimiterCount = as.GroupLimiterCount
	}
	if p.cooldown != nil {
		s.ActiveCooldowns = p.cooldown.ActiveCount()
	}
	return s
}

// Unban removes a ban from the antispam plugin. Returns error if antispam not bound.
func (p *Plugin) Unban(userID string) error {
	if p.antispam == nil {
		return fmt.Errorf("ratelimitui: antispam plugin not bound")
	}
	p.antispam.Unban(userID)
	return nil
}

// ResetCooldown resets a user's cooldown for a command. Returns error if cooldown not bound.
func (p *Plugin) ResetCooldown(userID, command string) error {
	if p.cooldown == nil {
		return fmt.Errorf("ratelimitui: cooldown plugin not bound")
	}
	p.cooldown.Reset(userID, command)
	return nil
}
