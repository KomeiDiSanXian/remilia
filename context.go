package remilia

import (
	stdctx "context"
	"errors"
	"strings"
	"sync"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// state is the V2 user state extension container.
//
// It is intentionally unexported. Access is via ctx.Set/ctx.Get/ctx.All.
type state struct {
	mu sync.RWMutex
	m  map[string]any
}

// retryAttempt stores retry attempt as a typed extension (V2 Phase 2).
//
// It replaces the legacy internalState key "_remilia_internal_retry_attempt".
// During progressive migration we:
//   - write only to this extension
//   - read from this extension first, then fallback to legacy internalState
//
// This keeps the migration safe while allowing old code paths to be updated gradually.
type retryAttempt struct {
	Attempt int
}

// middlewareTrace stores executed named middleware trace as a typed extension.
//
// It replaces legacy internalState key internalStateKeyMiddlewareTrace.
// Migration rule:
//   - write only to this extension (via SetMiddlewareTrace)
//   - read this extension first, then fallback to legacy internalState
//
// Note: slice is treated as immutable snapshot per write.
type middlewareTrace struct {
	Trace []string
}

// parsedCommand stores parsed command as a typed extension.
//
// It replaces legacy internalState key stateKeyParsedCommand.
// Migration rule:
//   - write only to this extension
//   - read this extension first, then fallback to legacy internalState
//
// Note: it stores pointer as-is; caller should treat it as immutable.
type parsedCommand struct {
	Cmd *command.ParsedCommand
}

func newStateExt() *state {
	return &state{m: make(map[string]any)}
}

// Context 上下文
type Context struct {
	ctxMu   sync.RWMutex
	ctx     stdctx.Context // 标准库 context，用于超时控制、取消传播等
	matcher *Matcher
	event   *dto.Payload

	// --- V2 extensions store ---
	extOnce sync.Once
	ext     *Extensions

	api openapi.OpenAPI
}

// NewContext 创建一个新的上下文
func NewContext(event *dto.Payload, api openapi.OpenAPI) *Context {
	return &Context{
		ctx:   stdctx.Background(),
		event: event,
		api:   api,
	}
}

// NewContextWithContext 创建带自定义标准库 context 的上下文
// 用于中间件需要注入自定义 context 的场景（如注入 trace context）
//
// 使用示例：
//
//	// 在中间件中注入带 trace 信息的 context
//	func Tracing() HandlerMiddleware {
//	    return func(next HandlerE) HandlerE {
//	        return func(ctx *Context) error {
//	            span, stdCtx := tracer.Start(ctx.Context(), "handler")
//	            defer span.End()
//
//	            // 替换为带 trace 的 context
//	            ctx.ctx = stdCtx
//	            return next(ctx)
//	        }
//	    }
//	}
func NewContextWithContext(ctx stdctx.Context, event *dto.Payload, api openapi.OpenAPI) *Context {
	c := NewContext(event, api)
	c.SetStdContext(ctx)
	return c
}

// Context 返回标准库 context.Context
// 用于传递给标准库和第三方库的函数（如 database/sql, http.Client, grpc 等）
//
// 使用示例：
//
//	// 数据库查询超时
//	dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
//	defer cancel()
//	result, err := db.QueryContext(dbCtx, "SELECT * FROM users")
//
//	// HTTP 请求超时
//	httpCtx, cancel := context.WithTimeout(ctx.Context(), 3*time.Second)
//	defer cancel()
//	req, _ := http.NewRequestWithContext(httpCtx, "GET", url, nil)
//	resp, err := http.DefaultClient.Do(req)
//
// 注意：
//   - 不要封装 WithTimeout, WithCancel 等标准库方法，直接使用标准库 API
//   - 如果在 goroutine 中只使用 stdCtx（不访问 Context 其他字段），不需要 Retain
//   - 如果需要访问 Context.State 等字段，必须使用 Retain/Release 或 WithRetainAsync
func (ctx *Context) Context() stdctx.Context {
	if ctx == nil {
		return stdctx.Background()
	}
	ctx.ctxMu.RLock()
	c := ctx.ctx
	ctx.ctxMu.RUnlock()
	if c == nil {
		return stdctx.Background()
	}
	return c
}

// SetStdContext 设置标准库 context
// 用于中间件注入自定义 context（如注入 tracing context、超时控制等）
//
// 使用场景：
//   - 分布式追踪：注入带 trace span 的 context
//   - 超时控制：替换为带超时的 context
//   - 取消传播：注入可取消的 context
//
// 使用示例：
//
//	// 在中间件中注入 tracing context
//	func Tracing() HandlerMiddleware {
//	    return func(next HandlerE) HandlerE {
//	        return func(ctx *Context) error {
//	            tracer := otel.Tracer("remilia")
//	            stdCtx, span := tracer.Start(ctx.Context(), "handle_event")
//	            defer span.End()
//
//	            // 注入带 trace 的 context
//	            originalCtx := ctx.Context()
//	            ctx.SetStdContext(stdCtx)
//	            defer ctx.SetStdContext(originalCtx)
//
//	            return next(ctx)
//	        }
//	    }
//	}
//
// 注意：
//   - 通常在中间件中使用，应该在处理完成后恢复原始 context
//   - 建议使用 defer 确保 context 被正确恢复
//   - 不要在多个 goroutine 中并发调用此方法
func (ctx *Context) SetStdContext(stdCtx stdctx.Context) {
	if ctx == nil {
		return
	}
	ctx.ctxMu.Lock()
	ctx.ctx = stdCtx
	ctx.ctxMu.Unlock()
}

// Clone 克隆 Context 用于异步操作
//
// Clone 会创建一个新的 Context 实例，复制当前 Context 的所有字段。
//
// 使用场景：
//   - 在 goroutine 中使用 Context
//   - 需要修改 Context 状态而不影响原 Context
//   - 长时间运行的异步任务
//
// 注意：
//   - 克隆的 Context 拥有独立的 State map（深拷贝）
//   - 克隆的 Context 共享相同的 event、api 和 stdCtx（浅拷贝）
func (ctx *Context) Clone() *Context {
	newCtx := &Context{
		// ctx is shared (shallow copy) but protected by ctxMu for concurrent access
		ctx:     ctx.Context(),
		matcher: ctx.matcher,
		event:   ctx.event,
		api:     ctx.api,
	}

	// Copy typed extensions snapshot.
	// Values are treated as immutable; we copy the map entries.
	if ex := ctx.Ext(); ex != nil {
		dst := newCtx.Ext() // ensure initialized
		for k, v := range ex.Snapshot() {
			dst.Set(k, v)
		}
	}

	// Deep copy V2 store (state) so clone mutations don't affect original.
	if s, ok := ExtGet[*state](ctx.Ext()); ok && s != nil {
		s.mu.RLock()
		cp := make(map[string]any, len(s.m))
		for k, v := range s.m {
			cp[k] = v
		}
		s.mu.RUnlock()
		ExtSet(newCtx.Ext(), &state{m: cp})
	}

	return newCtx
}

// const (
//	// legacyUserStateKeyMiddlewareTrace is a legacy key previously written into user state.
//	// It is now forbidden to avoid user key collisions.
//	legacyUserStateKeyMiddlewareTrace = "mw_trace"
// )

// isReservedUserStateKey reports whether key is reserved for framework internal use.
// Reserved keys are forbidden in user-facing state.
func isReservedUserStateKey(key string) bool {
	k := strings.TrimSpace(key)
	if k == "" {
		return false
	}

	// legacy user-state key previously used for middleware trace; keep forbidden to avoid collisions.
	if k == "mw_trace" {
		return true
	}
	if k == "retry_attempt" {
		return true
	}
	// NOTE: do NOT reserve generic user keys like "user_id" / "request_id".
	// They are commonly used by middleware and user code.
	return strings.HasPrefix(k, "_remilia_internal_")
}

// GetParsedCommand 获取增强版命令解析结果（如果之前已解析）
func (ctx *Context) GetParsedCommand() *command.ParsedCommand {
	if ctx == nil {
		return nil
	}
	if pc, ok := ExtGet[parsedCommand](ctx.Ext()); ok {
		return pc.Cmd
	}
	return nil
}

// SetParsedCommand 设置增强版命令解析结果（通常由中间件或规则设置）
func (ctx *Context) SetParsedCommand(cmd *command.ParsedCommand) {
	if ctx == nil {
		return
	}
	ExtSet(ctx.Ext(), parsedCommand{Cmd: cmd})
}

// MatchCommand 使用给定的解析器匹配命令
// 如果匹配成功，将结果缓存到 internalState，并返回 true
func (ctx *Context) MatchCommand(parser *command.CommandParser) bool {
	content := ctx.GetMessageContent()
	parsed, err := parser.Parse(content)
	if err != nil {
		return false
	}
	ctx.SetParsedCommand(parsed)
	return true
}

// GetMiddlewareTrace returns the executed named middleware trace recorded by Engine.Named tracing.
//
// V2 migration: read typed extension first, then fallback to legacy internalState.
func (ctx *Context) GetMiddlewareTrace() ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	if mt, ok := ExtGet[middlewareTrace](ctx.Ext()); ok {
		return mt.Trace, true
	}
	return nil, false
}

// SetMiddlewareTrace sets the executed named middleware trace (framework internal).
//
// It writes only to typed extension store.
func (ctx *Context) SetMiddlewareTrace(trace []string) {
	if ctx == nil {
		return
	}
	// Take a copy to prevent accidental external mutation.
	cp := append([]string(nil), trace...)
	ExtSet(ctx.Ext(), middlewareTrace{Trace: cp})
}

// SetRetryAttempt sets the current retry attempt (framework internal).
//
// V2 migration:
//   - prefer storing in typed extensions
//   - do not write legacy internalState key anymore
func (ctx *Context) SetRetryAttempt(attempt int) {
	if ctx == nil {
		return
	}
	ExtSet(ctx.Ext(), retryAttempt{Attempt: attempt})
}

// GetRetryAttempt returns the current retry attempt set by Retry middleware.
// If not present, it returns (0, false).
//
// V2 migration: read typed extension first, then fallback to legacy internalState.
func (ctx *Context) GetRetryAttempt() (int, bool) {
	if ctx == nil {
		return 0, false
	}
	if ra, ok := ExtGet[retryAttempt](ctx.Ext()); ok {
		return ra.Attempt, true
	}
	return 0, false
}

// Ext returns the typed-key extensions store.
//
// This is a V2 primitive introduced for progressive migration.
func (ctx *Context) Ext() *Extensions {
	if ctx == nil {
		return nil
	}
	ctx.extOnce.Do(func() {
		ctx.ext = newExtensions()
	})
	return ctx.ext
}

// Set sets a user state value (V2 sugar).
//
// It uses the State extension stored in ctx.Ext() and enforces the reserved key policy.
// This is introduced for progressive migration and will replace SetState over time.
func (ctx *Context) Set(key string, value any) {
	if ctx == nil {
		return
	}
	if isReservedUserStateKey(key) {
		logrus.WithField("key", key).Warn("[Context] set reserved state key is forbidden")
		return
	}

	// Treat nil as delete to match common map semantics and simplify migrations.
	if value == nil {
		ctx.Delete(key)
		return
	}

	s := ExtGetOrInit(ctx.Ext(), func() *state { return newStateExt() })
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

// Delete deletes a user state value stored via ctx.Set.
func (ctx *Context) Delete(key string) {
	if ctx == nil {
		return
	}
	if isReservedUserStateKey(key) {
		logrus.WithField("key", key).Warn("[Context] delete reserved state key is forbidden")
		return
	}
	s, ok := ExtGet[*state](ctx.Ext())
	if !ok || s == nil {
		return
	}
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}

// Get gets a user state value (V2 sugar).
func (ctx *Context) Get(key string) (any, bool) {
	if ctx == nil {
		return nil, false
	}
	s, ok := ExtGet[*state](ctx.Ext())
	if !ok || s == nil {
		return nil, false
	}
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

// All returns a copy of all user state values stored via ctx.Set.
func (ctx *Context) All() map[string]any {
	if ctx == nil {
		return nil
	}
	s, ok := ExtGet[*state](ctx.Ext())
	if !ok || s == nil {
		return map[string]any{}
	}
	s.mu.RLock()
	out := make(map[string]any, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	s.mu.RUnlock()
	return out
}

// ErrNilAPI 表示 OpenAPI 未初始化
var ErrNilAPI = errors.New("openAPI is nil")

// SendGroupMessage 发送群聊消息
func (ctx *Context) SendGroupMessage(groupID string, msg *dto.Message) (gjson.Result, error) {
	if ctx == nil || ctx.api == nil {
		logrus.Error("[Context] OpenAPI is nil")
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.GroupChat(groupID, msg)
}

// SendSingleMessage 发送私聊消息
func (ctx *Context) SendSingleMessage(openID string, msg *dto.Message) (gjson.Result, error) {
	if ctx == nil || ctx.api == nil {
		logrus.Error("[Context] OpenAPI is nil")
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.SingleChat(openID, msg)
}

// ReplyGroup 回复群聊消息（自动获取 group_openid）
func (ctx *Context) ReplyGroup(msg *dto.Message) (gjson.Result, error) {
	var event dto.GroupAtMessageCreateEvent
	if err := ctx.DecodeEvent(&event); err != nil {
		logrus.WithError(err).Error("[Context] Failed to decode group event")
		return gjson.Result{}, err
	}

	if msg.MessageID == "" {
		msg.MessageID = event.ID
	}

	return ctx.SendGroupMessage(event.GroupOpenID, msg)
}

// ReplyPrivate 回复私聊消息（自动获取 openid）
func (ctx *Context) ReplyPrivate(msg *dto.Message) (gjson.Result, error) {
	var event dto.C2CMessageCreateEvent
	if err := ctx.DecodeEvent(&event); err != nil {
		logrus.WithError(err).Error("[Context] Failed to decode c2c event")
		return gjson.Result{}, err
	}

	if msg.MessageID == "" {
		msg.MessageID = event.ID
	}

	return ctx.SendSingleMessage(event.Author.UserOpenID, msg)
}

// GetMessageContent 获取消息内容（零拷贝优化）
// 使用 gjson 直接提取字段，避免完整 JSON 解析
func (ctx *Context) GetMessageContent() string {
	if ctx == nil || ctx.event == nil {
		return ""
	}
	result := gjson.GetBytes(ctx.event.Detail, "content")
	return result.String()
}

// GetAuthor 获取消息作者信息（零拷贝优化）
// 使用 gjson 直接提取 author 字段，避免完整结构体解析
// 返回临时 Author 对象，调用者不应保存指针
func (ctx *Context) GetAuthor() *dto.Author {
	if ctx == nil || ctx.event == nil {
		return nil
	}

	res := gjson.GetBytes(ctx.event.Detail, "author")
	if !res.Exists() {
		return nil
	}

	return &dto.Author{
		ID:           res.Get("id").String(),
		MemberOpenID: res.Get("member_openid").String(),
		UnionOpenID:  res.Get("union_openid").String(),
		UserOpenID:   res.Get("user_openid").String(),
	}
}

// GetEvent 获取事件
func (ctx *Context) GetEvent() *dto.Payload {
	if ctx == nil {
		return nil
	}
	return ctx.event
}

// GetMatcherSource 返回当前命中的 matcher 来源（例如 "global" 或 "plugin:<name>"）。
//
// 注意：该值由 Engine 在事件匹配命中后设置；如果在 matcher 命中前调用（或当前事件未命中任何 matcher），将返回空字符串。
func (ctx *Context) GetMatcherSource() string {
	if ctx == nil || ctx.matcher == nil {
		return ""
	}
	return ctx.matcher.Source
}

// GetEventType 获取事件类型
func (ctx *Context) GetEventType() dto.EventType {
	if ctx == nil || ctx.event == nil {
		return ""
	}
	return ctx.event.Type
}

// DecodeEvent 解码事件详情
func (ctx *Context) DecodeEvent(v any) error {
	if ctx == nil || ctx.event == nil {
		return errors.New("event is nil")
	}
	return ctx.event.Decode(v)
}

// MustGetString 获取字符串类型的状态值，如果不存在或类型不匹配返回错误
func (ctx *Context) MustGetString(key string) (string, error) {
	if val, ok := ctx.Get(key); ok {
		if str, ok := val.(string); ok {
			return str, nil
		}
		return "", errors.New("state key '" + key + "' is not a string")
	}
	return "", errors.New("state key '" + key + "' not found")
}

// MustGetInt 获取整数类型的状态值，如果不存在或类型不匹配返回错误
func (ctx *Context) MustGetInt(key string) (int, error) {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int); ok {
			return i, nil
		}
		return 0, errors.New("state key '" + key + "' is not an int")
	}
	return 0, errors.New("state key '" + key + "' not found")
}

// GetString 获取字符串类型的状态值，如果不存在或类型不匹配返回空字符串
func (ctx *Context) GetString(key string) string {
	if val, ok := ctx.Get(key); ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// GetInt 获取整数类型的状态值，如果不存在或类型不匹配返回 0
func (ctx *Context) GetInt(key string) int {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int); ok {
			return i
		}
	}
	return 0
}

// GetInt64 获取 int64 类型的状态值，如果不存在或类型不匹配返回 0
func (ctx *Context) GetInt64(key string) int64 {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int64); ok {
			return i
		}
	}
	return 0
}

// GetBool 获取布尔类型的状态值，如果不存在或类型不匹配返回 false
func (ctx *Context) GetBool(key string) bool {
	if val, ok := ctx.Get(key); ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// GetFloat64 获取 float64 类型的状态值，如果不存在或类型不匹配返回 0.0
func (ctx *Context) GetFloat64(key string) float64 {
	if val, ok := ctx.Get(key); ok {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return 0.0
}
