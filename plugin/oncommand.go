package plugin

// oncommand.go — SetupContext 命令注册便利方法

import (
	"github.com/KomeiDiSanXian/remilia/command"
	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// OnCommand 注册一个命令匹配器并绑定处理函数的快捷方式。
//
// 等价于 ctx.Reg.RegisterCommand(eventType, cmdPattern).Handle(handler)，
// 但省去了 .Reg 和 .Handle 的链式调用。
//
// cmdPattern 是包含触发前缀的完整命令模式，如 "/help" 或 "!!admin"。
// handler 是事件处理函数。extraRules 为额外的匹配规则（如权限检查）。
//
// 使用示例：
//
//	ctx.OnCommand("", "/ping", func(c *corectx.Context) error {
//	    return c.ReplyText("pong")
//	})
//
//	// 带权限规则：
//	ctx.OnCommand("", "/admin", adminHandler, OnHasPermission("admin", "manage"))
func (ctx *SetupContext) OnCommand(eventType, cmdPattern string, handler corectx.Handler, extraRules ...corectx.Rule) *engine.Matcher {
	m := ctx.Reg.RegisterCommand(eventType, cmdPattern, extraRules...)
	m.Handle(handler)
	return m
}

// OnCommandDef 注册一个增强命令定义（带参数/标志/子命令）的快捷方式。
//
// 等价于 ctx.Reg.RegisterCommand(eventType, trigger).SetDefinition(def).Handle(corectx.ExecuteCommandDefinition)，
// 但更简洁。trigger 为包含前缀的完整命令模式如 "/search"。
//
// 使用示例：
//
//	def := command.NewDef("search").
//	    Description("搜索内容").
//	    Arg("keyword", "搜索关键词", true).
//	    Build()
//	ctx.OnCommandDef("", "/search", def)
func (ctx *SetupContext) OnCommandDef(eventType, trigger string, def *command.Definition, extraRules ...corectx.Rule) *engine.Matcher {
	m := ctx.Reg.RegisterCommand(eventType, trigger, extraRules...)
	m.SetDefinition(def)
	m.Handle(corectx.ExecuteCommandDefinition)
	return m
}
