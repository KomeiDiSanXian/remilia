package context

import "github.com/KomeiDiSanXian/remilia/command"

// metadata.go — 框架内部元数据（重试、中间件追踪、Matcher 来源、命令解析）
//
// 包含：
//   - retryMetadata / SetRetryAttempt / GetRetryAttempt
//   - middlewareTrace / SetMiddlewareTrace / GetMiddlewareTrace
//   - parsedCommand / SetParsedCommand / GetParsedCommand / MatchCommand
//   - GetMatcherSource

// retryMetadata stores the current retry attempt as a typed extension.
// Set by the Retry middleware; read by GetRetryAttempt.
type retryMetadata struct {
	Attempt int
}

// middlewareTrace stores the executed named middleware trace as a typed extension.
// Set by the engine's Named middleware tracing; read by GetMiddlewareTrace.
// The slice is treated as an immutable snapshot per write.
type middlewareTrace struct {
	Trace []string
}

// parsedCommand stores the parsed command as a typed extension.
// Set by SetParsedCommand; read by GetParsedCommand.
// The pointer is stored as-is; callers should treat it as immutable.
type parsedCommand struct {
	Cmd *command.Parsed
}

// SetRetryAttempt sets the current retry attempt (framework internal).
func (ctx *Context) SetRetryAttempt(attempt int) {
	if ctx == nil {
		return
	}
	ctx.Ext().SetTyped(retryMetadata{Attempt: attempt})
}

// GetRetryAttempt returns the current retry attempt set by Retry middleware.
func (ctx *Context) GetRetryAttempt() (int, bool) {
	if ctx == nil {
		return 0, false
	}
	if ra, ok := ctx.Ext().GetTyped[retryMetadata](); ok {
		return ra.Attempt, true
	}
	return 0, false
}

// SetMiddlewareTrace sets the executed named middleware trace (framework internal).
func (ctx *Context) SetMiddlewareTrace(trace []string) {
	if ctx == nil {
		return
	}
	cp := append([]string(nil), trace...)
	ctx.Ext().SetTyped(middlewareTrace{Trace: cp})
}

// GetMiddlewareTrace returns the executed named middleware trace recorded by engine.Named tracing.
// Returns a copy of the trace to prevent external modification.
func (ctx *Context) GetMiddlewareTrace() ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	if mt, ok := ctx.Ext().GetTyped[middlewareTrace](); ok {
		cp := make([]string, len(mt.Trace))
		copy(cp, mt.Trace)
		return cp, true
	}
	return nil, false
}

// GetParsedCommand 获取增强版命令解析结果（如果之前已解析）
func (ctx *Context) GetParsedCommand() *command.Parsed {
	if ctx == nil {
		return nil
	}
	if pc, ok := ctx.Ext().GetTyped[parsedCommand](); ok {
		return pc.Cmd
	}
	return nil
}

// SetParsedCommand 设置增强版命令解析结果（通常由中间件或规则设置）
func (ctx *Context) SetParsedCommand(cmd *command.Parsed) {
	if ctx == nil {
		return
	}
	ctx.Ext().SetTyped(parsedCommand{Cmd: cmd})
}

// MatchCommand 使用给定的解析器匹配命令
func (ctx *Context) MatchCommand(parser *command.Parser) bool {
	content := ctx.GetMessageContent()
	parsed, err := parser.Parse(content)
	if err != nil {
		return false
	}
	ctx.SetParsedCommand(parsed)
	return true
}

// GetMatcherSource 返回当前命中的 matcher 来源
func (ctx *Context) GetMatcherSource() string {
	if ctx == nil || ctx.matcher == nil {
		return ""
	}
	return ctx.matcher.GetSource()
}
