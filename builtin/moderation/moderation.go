package moderation

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

type WarningEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
	Moderator string    `json:"moderator"`
}

type Plugin struct {
	mu       sync.RWMutex
	warnData map[string][]WarningEntry
	dataFile string
}

func NewPlugin(opts ...Option) *Plugin {
	p := &Plugin{
		warnData: make(map[string][]WarningEntry),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

type Option func(*Plugin)

func WithDataFile(path string) Option {
	return func(p *Plugin) { p.dataFile = path }
}

func New(opts ...Option) *plugin.Descriptor {
	return NewPlugin(opts...).Descriptor()
}

func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:         "moderation",
		Version:      "1.0.0",
		Privileged:   true,
		OptionalDeps: []string{"permission"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "群组管理插件：禁言、踢出、警告、清屏",
			Category:    "管理",
			Tags:        []string{"管理", "审核", "群管"},
			HelpText: `群组管理命令：
  /mute <用户> [时长]   — 禁言用户（默认5分钟）
  /kick <用户> [原因]   — 踢出用户
  /warn <用户> [原因]    — 警告用户
  /warnings [用户]      — 查看警告记录
  /clean <数量>         — 批量删除消息`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.load()
			p.registerCommands(ctx)
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			p.save()
			return nil
		},
	}
}

func (p *Plugin) registerCommands(ctx *plugin.SetupContext) {
	muteCmd := &command.Definition{
		Name:        "mute",
		Description: "禁言用户",
		Usage:       "/mute <用户> [时长]",
		Category:    "管理",
		Examples:    []string{"/mute @user", "/mute @user 10m"},
	}
	ctx.Reg.RegisterCommand("", "/mute").SetDefinition(muteCmd).Handle(p.handleMute)

	kickCmd := &command.Definition{
		Name:        "kick",
		Description: "踢出用户",
		Usage:       "/kick <用户> [原因]",
		Category:    "管理",
		Examples:    []string{"/kick @user", "/kick @user 广告"},
	}
	ctx.Reg.RegisterCommand("", "/kick").SetDefinition(kickCmd).Handle(p.handleKick)

	warnCmd := &command.Definition{
		Name:        "warn",
		Description: "警告用户",
		Usage:       "/warn <用户> [原因]",
		Category:    "管理",
		Examples:    []string{"/warn @user", "/warn @user 刷屏"},
	}
	ctx.Reg.RegisterCommand("", "/warn").SetDefinition(warnCmd).Handle(p.handleWarn)

	warningsCmd := &command.Definition{
		Name:        "warnings",
		Description: "查看用户警告记录",
		Usage:       "/warnings [用户]",
		Category:    "管理",
		Examples:    []string{"/warnings", "/warnings @user"},
	}
	ctx.Reg.RegisterCommand("", "/warnings").SetDefinition(warningsCmd).Handle(p.handleWarnings)

	cleanCmd := &command.Definition{
		Name:        "clean",
		Description: "批量删除消息",
		Usage:       "/clean <数量>",
		Category:    "管理",
		Examples:    []string{"/clean 10"},
	}
	ctx.Reg.RegisterCommand("", "/clean").SetDefinition(cleanCmd).Handle(p.handleClean)
}

func (p *Plugin) handleMute(ctx *eventctx.Context) error {
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /mute <用户> [时长]"))
		return nil
	}
	target := args[1]
	duration := 5 * time.Minute
	if len(args) >= 3 {
		if d, err := time.ParseDuration(args[2]); err == nil {
			duration = d
		}
	}
	chat := ctx.GetPlatformEvent().Chat()
	if !chat.IsGroup {
		ctx.Reply(platform.TextMessage("该命令仅支持群聊"))
		return nil
	}
	if gm, ok := ctx.GetPlatformSender().(platform.GroupManager); ok {
		if err := gm.BanMember(ctx.Context(), chat.ID, target, duration); err != nil {
			ctx.Reply(platform.TextMessage(fmt.Sprintf("禁言失败: %v", err)))
			return nil
		}
		ctx.Reply(platform.TextMessage(fmt.Sprintf("已禁言 %s %v", target, duration)))
		return nil
	}
	ctx.Reply(platform.TextMessage("当前平台不支持禁言操作"))
	return nil
}

func (p *Plugin) handleKick(ctx *eventctx.Context) error {
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /kick <用户> [原因]"))
		return nil
	}
	target := args[1]
	chat := ctx.GetPlatformEvent().Chat()
	if !chat.IsGroup {
		ctx.Reply(platform.TextMessage("该命令仅支持群聊"))
		return nil
	}
	if gm, ok := ctx.GetPlatformSender().(platform.GroupManager); ok {
		permanent := len(args) >= 3
		if err := gm.KickMember(ctx.Context(), chat.ID, target, permanent); err != nil {
			ctx.Reply(platform.TextMessage(fmt.Sprintf("踢出失败: %v", err)))
			return nil
		}
		msg := fmt.Sprintf("已踢出 %s", target)
		if permanent {
			msg += " (拉黑)"
		}
		ctx.Reply(platform.TextMessage(msg))
		return nil
	}
	ctx.Reply(platform.TextMessage("当前平台不支持踢出操作"))
	return nil
}

func (p *Plugin) handleWarn(ctx *eventctx.Context) error {
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /warn <用户> [原因]"))
		return nil
	}
	target := args[1]
	reason := ""
	if len(args) >= 3 {
		reason = strings.Join(args[2:], " ")
	}
	sender := ctx.GetSenderInfo()

	p.mu.Lock()
	p.warnData[target] = append(p.warnData[target], WarningEntry{
		Timestamp: time.Now(),
		Reason:    reason,
		Moderator: sender.ID,
	})
	count := len(p.warnData[target])
	p.mu.Unlock()

	go p.save()
	logger.Infof("[Moderation] User %s warned by %s: %s (total: %d)", target, sender.ID, reason, count)
	msg := fmt.Sprintf("已警告 %s (共 %d 次警告)", target, count)
	if reason != "" {
		msg += fmt.Sprintf(" (原因: %s)", reason)
	}
	ctx.Reply(platform.TextMessage(msg))
	return nil
}

func (p *Plugin) handleWarnings(ctx *eventctx.Context) error {
	args := strings.Fields(ctx.GetMessageContent())
	target := ctx.GetSenderInfo().ID
	if len(args) >= 2 {
		target = args[1]
	}
	p.mu.RLock()
	warnings := p.warnData[target]
	p.mu.RUnlock()

	if len(warnings) == 0 {
		ctx.Reply(platform.TextMessage(fmt.Sprintf("%s 暂无警告记录", target)))
		return nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s 的警告记录 (共 %d 次):\n", target, len(warnings)))
	for i, w := range warnings {
		sb.WriteString(fmt.Sprintf("%d. [%s]", i+1, w.Timestamp.Format("01-02 15:04")))
		if w.Reason != "" {
			sb.WriteString(fmt.Sprintf(" %s", w.Reason))
		}
		sb.WriteString(fmt.Sprintf(" (由 %s)\n", w.Moderator))
	}
	ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

func (p *Plugin) handleClean(ctx *eventctx.Context) error {
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /clean <数量>"))
		return nil
	}
	chat := ctx.GetPlatformEvent().Chat()
	if am, ok := ctx.GetPlatformSender().(platform.AutoModerator); ok {
		var count int
		fmt.Sscanf(args[1], "%d", &count)
		if count <= 0 || count > 100 {
			ctx.Reply(platform.TextMessage("数量应在 1-100 之间"))
			return nil
		}
		for i := 0; i < count; i++ {
			_ = am.DeleteMemberMessage(ctx.Context(), chat.ID, "")
		}
		ctx.Reply(platform.TextMessage(fmt.Sprintf("已尝试清理 %d 条消息", count)))
		return nil
	}
	ctx.Reply(platform.TextMessage("当前平台不支持消息清理"))
	return nil
}

func (p *Plugin) save() {
	if p.dataFile == "" {
		return
	}
	p.mu.RLock()
	data := make(map[string][]WarningEntry, len(p.warnData))
	for k, v := range p.warnData {
		entries := make([]WarningEntry, len(v))
		copy(entries, v)
		data[k] = entries
	}
	p.mu.RUnlock()
	if err := jsonfile.Write(p.dataFile, data); err != nil {
		logger.WithError(err).Warn("[Moderation] Failed to save")
	}
}

func (p *Plugin) load() {
	if p.dataFile == "" {
		return
	}
	data, err := jsonfile.Read[map[string][]WarningEntry](p.dataFile)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.warnData = data
	p.mu.Unlock()
	logger.Infof("[Moderation] Loaded %d warning records", len(data))
}
