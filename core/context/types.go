package context

// Rule 定义规则函数类型
// 规则函数接收具体的 *Context 类型并返回是否匹配
type Rule func(ctx *Context) bool

// Handler 事件处理函数类型（带错误返回）
type Handler func(*Context) error

// Middleware 中间件函数类型
type Middleware func(Handler) Handler

// Option Context 配置选项函数类型
type Option func(*Context)
