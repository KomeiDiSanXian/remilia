package router

import corectx "github.com/KomeiDiSanXian/remilia/core/context"

// WithCommandPrefix 创建一条规则，将带有命令前缀的消息（如 "/help"、"!!admin"）
// 路由到标准 Engine。
//
// 使用 [corectx.SplitCommandPattern] 检测前缀：
//   - "/help"   → prefix="/", name="help"   → 匹配
//   - "!!admin" → prefix="!!", name="admin" → 匹配
//   - "hello"   → prefix="", name="hello"   → 不匹配
//   - "帮助"     → prefix="", name="帮助"     → 不匹配
//
// 先提取第一个空白分隔的 token，然后检查是否有非字母数字前缀。
// 这可以避免对自然语言的误匹配。
func WithCommandPrefix() *RouteRule {
	return &RouteRule{
		Name:     "command_prefix",
		Strategy: StrategyEngine,
		Priority: 10,
		Match: func(ctx *corectx.Context) bool {
			content := ctx.GetMessageContent()
			firstWord := extractCommand(content)
			if firstWord == "" {
				return false
			}
			prefix, _ := corectx.SplitCommandPattern(firstWord)
			return prefix != ""
		},
	}
}

// WithFSMRoute 保留用于向后兼容。FSM 现在始终最先检查，无需声明此规则。
func WithFSMRoute() *RouteRule {
	return nil
}

// WithCustom 创建一个具有任意匹配函数和给定策略的规则。name 用于调试。
func WithCustom(name string, strategy Strategy, match func(ctx *corectx.Context) bool) *RouteRule {
	return &RouteRule{
		Name:     name,
		Strategy: strategy,
		Priority: 100,
		Match:    match,
	}
}


