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
//	    c.ReplyText("pong"); return nil
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
// 和 OnCommand 的区别：OnCommandDef 会自动设置 ParseFromDefinition 解析规则，
// 使得 def 中定义的 Arguments/Flags 能被正确解析，通过 ctx.GetParsedCommand() 获取。
//
// def.Handler 为命令处理函数，签名为 func(any)，运行时传入的是 *Context：
//
//	def.Handler = func(ctx any) {
//	    c := ctx.(*corectx.Context)
//	    keyword := c.GetParsedCommand().GetString("keyword")
//	    c.ReplyText("搜索: " + keyword); return nil
//	}
//	ctx.OnCommandDef("", "/search", def)
//
// trigger 为包含前缀的完整命令模式如 "/search"。
func (ctx *SetupContext) OnCommandDef(eventType, trigger string, def *command.Definition, extraRules ...corectx.Rule) *engine.Matcher {
	allRules := make([]corectx.Rule, 0, 1+len(extraRules))
	allRules = append(allRules, corectx.OnParseCommand(def))
	allRules = append(allRules, extraRules...)

	m := ctx.Reg.RegisterCommand(eventType, trigger, allRules...)
	if m == nil {
		return nil
	}
	m.SetDefinition(def)

	if def.Handler != nil {
		m.Handle(func(c *corectx.Context) error {
			def.Handler(c)
			return nil
		})
	}
	return m
}

// OnCommandDefWith 注册增强命令定义，直接绑定 corectx.Handler（无需通过 def.Handler 做类型断言）。
//
// 和 OnCommandDef 的区别：handler 是直接传入的 corectx.Handler，签名是 func(*Context) error，
// 无需将 any 类型断言为 *Context。
//
// 使用示例：
//
//	ctx.OnCommandDefWith("", "/search",
//	    command.NewDef("search").
//	        Arg("keyword", "搜索关键词", true).
//	        Build(),
//	    func(c *corectx.Context) error {
//	        keyword := c.GetParsedCommand().GetString("keyword")
//	        c.ReplyText("搜索结果: " + keyword); return nil
//	    },
//	)
func (ctx *SetupContext) OnCommandDefWith(eventType, trigger string, def *command.Definition, handler corectx.Handler, extraRules ...corectx.Rule) *engine.Matcher {
	m := ctx.OnCommandDef(eventType, trigger, def, extraRules...)
	if m != nil {
		m.Handle(handler)
	}
	return m
}
