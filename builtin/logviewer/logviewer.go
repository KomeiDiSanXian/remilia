package logviewer

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/auditlog"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

type Plugin struct {
	auditLogSvc *plugin.ServiceProxy[*auditlog.Plugin]
}

func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:         "logviewer",
		Version:      "1.0.0",
		Deps:         []string{"auditlog"},
		OptionalDeps: []string{"permission"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "审计日志查询工具，在聊天中搜索和查看审计日志",
			Category:    "管理",
			Tags:        []string{"审计", "日志", "管理"},
			HelpText: `审计日志查询命令：
  /logs search <关键词>      — 搜索日志
  /logs user <用户ID> [数量]  — 查看用户操作记录
  /logs action <类型> [数量]  — 按操作类型过滤
  /logs stats                — 查看日志统计
  /logs recent [数量]        — 查看最近日志`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			svc, ok := plugin.TryService[*auditlog.Plugin](ctx, "auditlog")
			if !ok {
				return nil, fmt.Errorf("auditlog plugin not found")
			}
			p.auditLogSvc = svc
			p.registerCommands(ctx)
			return p, nil
		},
	}
}

// auditLog 返回当前 auditlog 插件实例（防过期的延迟解析）。
func (p *Plugin) auditLog() *auditlog.Plugin {
	if p.auditLogSvc == nil {
		return nil
	}
	a, _ := p.auditLogSvc.Get()
	return a
}

func (p *Plugin) registerCommands(ctx *plugin.SetupContext) {
	logsCmd := &command.Definition{
		Name:        "logs",
		Description: "审计日志查询",
		Usage:       "/logs <search|user|action|stats|recent> [参数]",
		Category:    "管理",
		SubCommands: []*command.Definition{
			{Name: "search", Description: "搜索日志", Usage: "/logs search <关键词>", Examples: []string{"/logs search plugin"}},
			{Name: "user", Description: "查看用户操作", Usage: "/logs user <用户ID> [数量]", Examples: []string{"/logs user 123456", "/logs user 123456 10"}},
			{Name: "action", Description: "按操作类型过滤", Usage: "/logs action <类型> [数量]", Examples: []string{"/logs action command", "/logs action perm.grant 5"}},
			{Name: "stats", Description: "查看日志统计", Usage: "/logs stats", Examples: []string{"/logs stats"}},
			{Name: "recent", Description: "查看最近日志", Usage: "/logs recent [数量]", Examples: []string{"/logs recent", "/logs recent 20"}},
		},
	}
	ctx.Reg.RegisterCommand("", "/logs").SetDefinition(logsCmd).Handle(p.handleLogs)
}

func (p *Plugin) handleLogs(ctx *eventctx.Context) error {
	args := strings.Fields(ctx.GetMessageContent())
	if len(args) < 2 {
		ctx.Reply(platform.TextMessage("用法: /logs search|user|action|stats|recent [参数]"))
		return nil
	}
	switch args[1] {
	case "search":
		return p.handleSearch(ctx, args[2:])
	case "user":
		return p.handleUser(ctx, args[2:])
	case "action":
		return p.handleAction(ctx, args[2:])
	case "stats":
		return p.handleStats(ctx)
	case "recent":
		return p.handleRecent(ctx, args[2:])
	default:
		ctx.Reply(platform.TextMessage("未知子命令，可用: search, user, action, stats, recent"))
		return nil
	}
}

func (p *Plugin) handleSearch(ctx *eventctx.Context, args []string) error {
	if len(args) < 1 {
		ctx.Reply(platform.TextMessage("用法: /logs search <关键词>"))
		return nil
	}
	query := strings.Join(args, " ")
	entries := p.auditLog().Recent(50)

	var results []auditlog.LogEntry
	for _, e := range entries {
		if strings.Contains(e.Content, query) || strings.Contains(e.Action, query) {
			results = append(results, e)
		}
	}
	if len(results) == 0 {
		ctx.Reply(platform.TextMessage(fmt.Sprintf("未找到包含 %q 的日志", query)))
		return nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("搜索 %q 结果 (共 %d 条):\n", query, len(results)))
	for i, e := range results {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("... 还有 %d 条\n", len(results)-i))
			break
		}
		sb.WriteString(fmt.Sprintf("#%d [%s] %s: %s\n", e.ID, e.Timestamp.Format("01-02 15:04"), e.Action, truncate(e.Content, 30)))
	}
	ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

func (p *Plugin) handleUser(ctx *eventctx.Context, args []string) error {
	if len(args) < 1 {
		ctx.Reply(platform.TextMessage("用法: /logs user <用户ID> [数量]"))
		return nil
	}
	userID := args[0]
	n := 10
	if len(args) >= 2 {
		fmt.Sscanf(args[1], "%d", &n)
	}
	entries := p.auditLog().ByUser(userID, n)
	if len(entries) == 0 {
		ctx.Reply(platform.TextMessage(fmt.Sprintf("用户 %s 暂无操作记录", userID)))
		return nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("用户 %s 最近 %d 条操作:\n", userID, len(entries)))
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("#%d [%s] %s\n", e.ID, e.Timestamp.Format("01-02 15:04"), e.Action))
	}
	ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

func (p *Plugin) handleAction(ctx *eventctx.Context, args []string) error {
	if len(args) < 1 {
		ctx.Reply(platform.TextMessage("用法: /logs action <类型> [数量]"))
		return nil
	}
	action := args[0]
	n := 10
	if len(args) >= 2 {
		fmt.Sscanf(args[1], "%d", &n)
	}
	entries := p.auditLog().ByAction(action, n)
	if len(entries) == 0 {
		ctx.Reply(platform.TextMessage(fmt.Sprintf("暂无 %s 类型的操作记录", action)))
		return nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("操作 %s 最近 %d 条记录:\n", action, len(entries)))
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("#%d [%s] 用户:%s %s\n", e.ID, e.Timestamp.Format("01-02 15:04"), e.UserID, truncate(e.Content, 30)))
	}
	ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

func (p *Plugin) handleStats(ctx *eventctx.Context) error {
	entries := p.auditLog().Recent(1000)
	total := len(entries)
	if total == 0 {
		ctx.Reply(platform.TextMessage("暂无日志记录"))
		return nil
	}
	actionCount := make(map[string]int)
	for _, e := range entries {
		actionCount[e.Action]++
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("审计日志统计 (最近 %d 条):\n", total))
	for action, count := range actionCount {
		bar := strings.Repeat("█", count*20/total)
		if len(bar) == 0 {
			bar = "▏"
		}
		sb.WriteString(fmt.Sprintf("  %s: %d %s\n", action, count, bar))
	}
	ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

func (p *Plugin) handleRecent(ctx *eventctx.Context, args []string) error {
	n := 10
	if len(args) >= 1 {
		fmt.Sscanf(args[0], "%d", &n)
	}
	entries := p.auditLog().Recent(n)
	if len(entries) == 0 {
		ctx.Reply(platform.TextMessage("暂无日志记录"))
		return nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("最近 %d 条操作日志:\n", len(entries)))
	for _, e := range entries {
		userShort := e.UserID
		if len(userShort) > 8 {
			userShort = userShort[:8] + ".."
		}
		sb.WriteString(fmt.Sprintf("#%d [%s] %s by %s\n", e.ID, e.Timestamp.Format("01-02 15:04"), e.Action, userShort))
	}
	ctx.Reply(platform.TextMessage(sb.String()))
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
