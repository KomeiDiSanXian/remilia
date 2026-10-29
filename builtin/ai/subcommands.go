// subcommands.go — AI 子命令的单一注册表。
//
// 每个子命令只在这里声明一次：canonical 名（"/ai <name>" 与 Parsed.CommandPath
// 用的名字）、别名（@机器人 / 私聊路径接受的自然语言写法）、以及处理函数。
// 两条入口都从这张表派生：
//
//	命令路径   /ai <name>        → parsed.CommandPath[1] → table.run
//	自然语言   @机器人 <alias>    → 整词匹配（含中文别名）→ table.run
//
// 历史上有两份手抄的 switch，结果 trace/skill/tools 在两条路径上可达性不一致
// （"@机器人 trace" 能解析却永远进不了处理器）。把"有哪些子命令"收成一处后，
// 新增子命令只需要加一行，帮助文案也随之自动跟随（见 renderSubCommandList）。
package ai

import (
	"slices"
	"strings"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// subCommand 一条子命令的分派定义。
type subCommand struct {
	// name canonical 名称，与 buildAIDefinition 里的 command.NewDef(name) 一致。
	name string
	// aliases 自然语言入口接受的其他写法（小写，整词匹配）。
	aliases []string
	// run 执行该子命令。rest 为去掉触发前缀与命令名之后的剩余正文，
	// 供需要参数的子命令（memory/todo/plan/task/remind 等）自行解析。
	run func(p *Plugin, ctx *eventctx.Context, rest string) error
	// naturalPath 是否允许从自然语言入口（@机器人 / 私聊兜底）进入。
	//
	// skill 走了自己的整词前缀特判（要支持 "skill add ..." 这类带参形式），
	// 因此不在这里重复登记。
	naturalPath bool
}

// subCommands 子命令注册表。顺序不影响匹配（两条入口都按名字查表）。
func subCommands() []subCommand {
	return []subCommand{
		{name: "reset", aliases: []string{"重置"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "reset")
			}},
		{name: "undo", naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "undo")
			}},
		{name: "retry", aliases: []string{"重试"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "retry")
			}},
		{name: "stop", aliases: []string{"停止"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "stop")
			}},
		{name: "summary", aliases: []string{"总结"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "summary")
			}},
		{name: "status", naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "status")
			}},
		{name: "stats", naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "stats")
			}},
		{name: "trace", aliases: []string{"追踪"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "trace")
			}},
		{name: "tools", aliases: []string{"工具", "help", "帮助"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.execSubCommand(ctx, "tools")
			}},
		{name: "memory", aliases: []string{"记忆"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, rest string) error {
				return p.handleMemoryCommand(ctx, rest)
			}},
		{name: "todo", aliases: []string{"待办"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, rest string) error {
				return p.handleTodoCommand(ctx, rest)
			}},
		{name: "plan", aliases: []string{"计划"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, rest string) error {
				return p.handlePlanCommand(ctx, rest)
			}},
		{name: "task", aliases: []string{"任务"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, rest string) error {
				return p.handleTaskCommand(ctx, rest)
			}},
		{name: "remind", aliases: []string{"提醒"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, rest string) error {
				return p.handleRemindCommand(ctx, rest)
			}},
		{name: "approve", aliases: []string{"允许", "批准", "同意"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.handleApprovalCommand(ctx, true)
			}},
		{name: "deny", aliases: []string{"拒绝", "驳回", "不同意"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.handleApprovalCommand(ctx, false)
			}},
		{name: "group", aliases: []string{"群配置"}, naturalPath: true,
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.handleGroupCommand(ctx)
			}},
		// skill 由自然语言入口的整词前缀特判处理，这里只登记命令路径。
		{name: "skill",
			run: func(p *Plugin, ctx *eventctx.Context, _ string) error {
				return p.handleSkillCommand(ctx)
			}},
	}
}

// lookupSubCommand 按 canonical 名或别名查表（大小写不敏感）。
func lookupSubCommand(name string) (subCommand, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return subCommand{}, false
	}
	for _, sc := range subCommands() {
		if sc.name == name {
			return sc, true
		}
		if slices.Contains(sc.aliases, name) {
			return sc, true
		}
	}
	return subCommand{}, false
}

// dispatchNatural 处理自然语言入口的一条正文，返回是否已由子命令接管。
//
// rest 是去掉触发前缀、命令词与 @ 之后的部分；带参数的子命令（memory/todo/
// plan/task/remind）由各自的 handler 继续解析。
func (p *Plugin) dispatchNatural(ctx *eventctx.Context, cmd string) bool {
	sc, ok := lookupSubCommand(cmd)
	if !ok || !sc.naturalPath {
		return false
	}
	if err := sc.run(p, ctx, p.subCommandRest(ctx, sc.name)); err != nil {
		logger.Errorf("exec subcommand %q: %v", cmd, err)
	}
	return true
}
