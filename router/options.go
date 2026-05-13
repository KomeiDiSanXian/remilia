package router

import corectx "github.com/KomeiDiSanXian/remilia/core/context"

// WithCommandPrefix 创建一条规则，将带有命令前缀的消息（如 "/help"、"!!admin"）
// 路由到标准 Engine。
//
// Handle 为 nil，由 Route() 自动设置为 dispatchToEngine。
func WithCommandPrefix() *RouteRule {
	return &RouteRule{
		Name:     "command_prefix",
		Priority: 0,
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

// WithCustom 创建一条规则，匹配时执行 handle。handle 返回 true 表示已处理。
func WithCustom(name string, match func(ctx *corectx.Context) bool, handle func(ctx *corectx.Context) bool) *RouteRule {
	return &RouteRule{
		Name:     name,
		Priority: 100,
		Match:    match,
		Handle:   handle,
	}
}
