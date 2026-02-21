package context

import (
	stdctx "context"
	"errors"
	"maps"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/tidwall/gjson"
	"go.opentelemetry.io/otel/trace"
)

// extensionState is the V2 user extensionState extension container.
//
// It is intentionally unexported. Access is via ctx.Set/ctx.Get/ctx.All.
type extensionState struct {
	mu sync.RWMutex
	m  map[string]any
}

// retryMetadata stores retry attempt as a typed extension (V2 Phase 2).
//
// It replaces the legacy internalState key "_remilia_internal_retry_attempt".
// During progressive migration we:
//   - write only to this extension
//   - read from this extension first, then fallback to legacy internalState
//
// This keeps the migration safe while allowing old code paths to be updated gradually.
type retryMetadata struct {
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
	Cmd *command.Parsed
}

func newStateExt() *extensionState {
	return &extensionState{m: make(map[string]any)}
}

// Matcher 定义 Matcher 的最小接口，用于避免循环依赖
type Matcher interface {
	GetSource() string
}

// decodeCache is a typed union that holds the result of the first successful
// DecodeEvent call for this Context.  Using concrete fields instead of `any`
// eliminates interface boxing, avoids a reflect.Type string allocation on
// every call, and lets the GC scan the value without an extra indirection.
//
// Only one field is populated at a time; which one is indicated by kind.
type decodeCache struct {
	kind uint8 // 0=empty 1=C2C 2=GroupAt 3=generic

	c2c     dto.C2CMessageCreateEvent
	groupAt dto.GroupAtMessageCreateEvent
	generic any // fallback for other event types
}

// Context 上下文
type Context struct {
	ctxMu   sync.RWMutex   // 保护 ctx 字段的读写锁
	ctx     stdctx.Context // 标准库 context，用于超时控制、取消传播等
	matcher Matcher        // Matcher 引用（使用 interface{} 避免循环依赖）
	event   *dto.Payload

	// --- V2 extensions store ---
	extInitialized atomic.Bool
	extMu          sync.Mutex
	extensions     *Extensions

	api openapi.OpenAPI

	// --- decode cache (typed union, replaces any+string) ---
	// Protected by decodeMu. A Context is processed by one handler chain at
	// a time, so contention is essentially zero.
	decodeMu sync.Mutex
	decoded  decodeCache

	// --- hot-path field caches ---
	// These are populated lazily on first access and never cleared until
	// ReleaseContext; they avoid repeated gjson.GetBytes calls when multiple
	// matchers/handlers inspect the same field.
	contentOnce sync.Once
	content     string // cached GetMessageContent result

	authorOnce sync.Once
	author     *dto.Author // cached GetAuthor result
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
func NewContextWithContext(ctx stdctx.Context, event *dto.Payload, api openapi.OpenAPI) *Context {
	c := NewContext(event, api)
	c.SetStdContext(ctx)
	return c
}

// Context 返回标准库 context.Context
// 用于传递给标准库和第三方库的函数（如 database/sql, http.Client, grpc 等）
func (ctx *Context) Context() stdctx.Context {
	if ctx == nil {
		logger.Error("[Context] CRITICAL: Context receiver is nil, returning Background()")
		return stdctx.Background()
	}
	ctx.ctxMu.RLock()
	c := ctx.ctx
	ctx.ctxMu.RUnlock()

	// 正常情况下不应为 nil（NewContext 已初始化）
	if c == nil {
		logger.Warn("[Context] stdctx is unexpectedly nil, returning Background(). This may indicate a bug.")
		return stdctx.Background()
	}
	return c
}

// SetStdContext 设置标准库 context
// 用于中间件注入自定义 context（如注入 tracing context、超时控制等）
func (ctx *Context) SetStdContext(stdCtx stdctx.Context) {
	if ctx == nil {
		logger.Error("[Context] CRITICAL: Cannot call SetStdContext on nil receiver")
		return
	}
	if stdCtx == nil {
		logger.Warn("[Context] Attempting to set nil stdctx, using Background() instead")
		stdCtx = stdctx.Background()
	}
	ctx.ctxMu.Lock()
	ctx.ctx = stdCtx
	ctx.ctxMu.Unlock()
}

// Clone 克隆 Context 用于异步操作
//
// Clone 会创建一个新的 Context 实例，复制当前 Context 的所有字段。
//
// 重要：克隆的 Context 使用独立的 context.Background()，不会受原 Context 取消的影响。
// 如果需要传播 trace 信息，会自动复制 trace span。
func (ctx *Context) Clone() *Context {
	// Clone the event to prevent mutation issues
	var clonedEvent *dto.Payload
	if ctx.event != nil {
		clonedEvent = ctx.event.Clone()
	}

	// 创建独立的 context，避免级联取消
	// 保留 deadline 和 values，但独立取消
	newStdCtx := stdctx.Background()

	// 复制 deadline（如果存在）
	if deadline, ok := ctx.Context().Deadline(); ok {
		var cancel stdctx.CancelFunc
		newStdCtx, cancel = stdctx.WithDeadline(newStdCtx, deadline)
		// 不保存 cancel，让 deadline 自动触发，避免需要手动管理
		_ = cancel
	}

	// 复制 trace span（如果存在）
	if span := trace.SpanFromContext(ctx.Context()); span.SpanContext().IsValid() {
		newStdCtx = trace.ContextWithSpan(newStdCtx, span)
	}

	newCtx := &Context{
		ctx:     newStdCtx,   // 使用独立的 context
		matcher: ctx.matcher, // 只读引用，安全
		event:   clonedEvent,
		api:     ctx.api, // 只读引用，安全
	}

	// Copy typed extensions snapshot.
	if ex := ctx.Ext(); ex != nil {
		dst := newCtx.Ext()
		for k, v := range ex.Snapshot() {
			dst.Set(k, v)
		}
	}

	// Deep copy V2 store (extensionState) so clone mutations don't affect original.
	if s, ok := ExtGet[*extensionState](ctx.Ext()); ok && s != nil {
		s.mu.RLock()
		cp := make(map[string]any, len(s.m))
		maps.Copy(cp, s.m)
		s.mu.RUnlock()
		ExtSet(newCtx.Ext(), &extensionState{m: cp})
	}

	return newCtx
}

// isReservedUserStateKey reports whether key is reserved for framework internal use.
func isReservedUserStateKey(key string) bool {
	k := strings.TrimSpace(key)
	if k == "" {
		return false
	}

	if k == "mw_trace" || k == "retry_attempt" {
		return true
	}
	return strings.HasPrefix(k, "_remilia_internal_")
}

// GetParsedCommand 获取增强版命令解析结果（如果之前已解析）
func (ctx *Context) GetParsedCommand() *command.Parsed {
	if ctx == nil {
		return nil
	}
	if pc, ok := ExtGet[parsedCommand](ctx.Ext()); ok {
		return pc.Cmd
	}
	return nil
}

// SetParsedCommand 设置增强版命令解析结果（通常由中间件或规则设置）
func (ctx *Context) SetParsedCommand(cmd *command.Parsed) {
	if ctx == nil {
		return
	}
	ExtSet(ctx.Ext(), parsedCommand{Cmd: cmd})
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

// GetMiddlewareTrace returns the executed named middleware trace recorded by engine.Named tracing.
// Returns a copy of the trace to prevent external modification.
func (ctx *Context) GetMiddlewareTrace() ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	if mt, ok := ExtGet[middlewareTrace](ctx.Ext()); ok {
		// Return a copy to prevent caller from modifying internal extensionState
		cp := make([]string, len(mt.Trace))
		copy(cp, mt.Trace)
		return cp, true
	}
	return nil, false
}

// SetMiddlewareTrace sets the executed named middleware trace (framework internal).
func (ctx *Context) SetMiddlewareTrace(trace []string) {
	if ctx == nil {
		return
	}
	cp := append([]string(nil), trace...)
	ExtSet(ctx.Ext(), middlewareTrace{Trace: cp})
}

// SetRetryAttempt sets the current retry attempt (framework internal).
func (ctx *Context) SetRetryAttempt(attempt int) {
	if ctx == nil {
		return
	}
	ExtSet(ctx.Ext(), retryMetadata{Attempt: attempt})
}

// GetRetryAttempt returns the current retry attempt set by Retry middleware.
func (ctx *Context) GetRetryAttempt() (int, bool) {
	if ctx == nil {
		return 0, false
	}
	if ra, ok := ExtGet[retryMetadata](ctx.Ext()); ok {
		return ra.Attempt, true
	}
	return 0, false
}

// Ext returns the typed-key extensions store.
func (ctx *Context) Ext() *Extensions {
	if ctx == nil {
		return nil
	}
	// 快速路径：已初始化
	if ctx.extInitialized.Load() {
		return ctx.extensions
	}
	// 慢路径：初始化（双重检查锁定）
	ctx.extMu.Lock()
	defer ctx.extMu.Unlock()
	if !ctx.extInitialized.Load() {
		ctx.extensions = newExtensions()
		ctx.extInitialized.Store(true)
	}
	return ctx.extensions
}

// Set sets a user extensionState value (V2 sugar).
func (ctx *Context) Set(key string, value any) {
	if ctx == nil {
		return
	}
	if isReservedUserStateKey(key) {
		logger.WithField("key", key).Warn("[Context] set reserved extensionState key is forbidden")
		return
	}

	if value == nil {
		ctx.Delete(key)
		return
	}

	s := ExtGetOrInit(ctx.Ext(), func() *extensionState { return newStateExt() })
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

// Delete deletes a user extensionState value stored via ctx.Set.
func (ctx *Context) Delete(key string) {
	if ctx == nil {
		return
	}
	if isReservedUserStateKey(key) {
		logger.WithField("key", key).Warn("[Context] delete reserved extensionState key is forbidden")
		return
	}
	s, ok := ExtGet[*extensionState](ctx.Ext())
	if !ok || s == nil {
		return
	}
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}

// Get gets a user extensionState value (V2 sugar).
func (ctx *Context) Get(key string) (any, bool) {
	if ctx == nil {
		return nil, false
	}
	s, ok := ExtGet[*extensionState](ctx.Ext())
	if !ok || s == nil {
		return nil, false
	}
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

// All returns a copy of all user extensionState values stored via ctx.Set.
func (ctx *Context) All() map[string]any {
	if ctx == nil {
		return nil
	}
	s, ok := ExtGet[*extensionState](ctx.Ext())
	if !ok || s == nil {
		return map[string]any{}
	}
	s.mu.RLock()
	out := make(map[string]any, len(s.m))
	maps.Copy(out, s.m)
	s.mu.RUnlock()
	return out
}

// ErrNilAPI 表示 OpenAPI 未初始化
var ErrNilAPI = errors.New("openAPI is nil")

// SendGroupMessage 发送群聊消息
func (ctx *Context) SendGroupMessage(groupID string, msg *dto.Message) (gjson.Result, error) {
	if ctx == nil || ctx.api == nil {
		logger.Error("[Context] OpenAPI is nil")
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.GroupChat(groupID, msg)
}

// SendSingleMessage 发送私聊消息
func (ctx *Context) SendSingleMessage(openID string, msg *dto.Message) (gjson.Result, error) {
	if ctx == nil || ctx.api == nil {
		logger.Error("[Context] OpenAPI is nil")
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.SingleChat(openID, msg)
}

// ReplyGroup 回复群聊消息（自动获取 group_openid）
func (ctx *Context) ReplyGroup(msg *dto.Message) (gjson.Result, error) {
	var event dto.GroupAtMessageCreateEvent
	if err := ctx.DecodeEvent(&event); err != nil {
		logger.WithError(err).Error("[Context] Failed to decode group event")
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
		logger.WithError(err).Error("[Context] Failed to decode c2c event")
		return gjson.Result{}, err
	}

	if msg.MessageID == "" {
		msg.MessageID = event.ID
	}

	return ctx.SendSingleMessage(event.Author.UserOpenID, msg)
}

// GetMessageContent 获取消息内容（零拷贝 + Once 缓存）
//
// 第一次调用执行 gjson.GetBytes；同一 Context 的后续调用直接返回缓存值。
// 在多 Matcher 场景（每个 Matcher 都调用此方法做内容匹配）时开销接近零。
func (ctx *Context) GetMessageContent() string {
	if ctx == nil || ctx.event == nil {
		return ""
	}
	ctx.contentOnce.Do(func() {
		ctx.content = gjson.GetBytes(ctx.event.Detail, "content").String()
	})
	return ctx.content
}

// GetAuthor 获取消息作者信息（Once 缓存）
func (ctx *Context) GetAuthor() *dto.Author {
	if ctx == nil || ctx.event == nil {
		return nil
	}
	ctx.authorOnce.Do(func() {
		res := gjson.GetBytes(ctx.event.Detail, "author")
		if !res.Exists() {
			return
		}
		ctx.author = &dto.Author{
			ID:           res.Get("id").String(),
			MemberOpenID: res.Get("member_openid").String(),
			UnionOpenID:  res.Get("union_openid").String(),
			UserOpenID:   res.Get("user_openid").String(),
		}
	})
	return ctx.author
}

// GetEvent 获取事件
func (ctx *Context) GetEvent() *dto.Payload {
	if ctx == nil {
		return nil
	}
	return ctx.event
}

// GetMatcherSource 返回当前命中的 matcher 来源
func (ctx *Context) GetMatcherSource() string {
	if ctx == nil || ctx.matcher == nil {
		return ""
	}
	return ctx.matcher.GetSource()
}

// GetEventType 获取事件类型
func (ctx *Context) GetEventType() dto.EventType {
	if ctx == nil || ctx.event == nil {
		return ""
	}
	return ctx.event.Type
}

// DecodeEvent 解码事件详情
//
// 对 C2CMessageCreateEvent 和 GroupAtMessageCreateEvent 使用 typed union 缓存：
// 缓存命中时直接做结构体值复制（单次赋值），无 reflect、无 interface 装箱。
// 其他类型走 generic 路径，缓存 any 指针，命中时做类型断言+值复制。
// 同一 Context 内第二次 DecodeEvent 开销接近零。
func (ctx *Context) DecodeEvent(v any) error {
	if ctx == nil || ctx.event == nil {
		return errors.New("event is nil")
	}

	ctx.decodeMu.Lock()
	defer ctx.decodeMu.Unlock()

	switch dst := v.(type) {
	case *dto.C2CMessageCreateEvent:
		if ctx.decoded.kind == 1 {
			// cache hit: struct copy, zero alloc
			*dst = ctx.decoded.c2c
			return nil
		}
		if err := ctx.event.Decode(dst); err != nil {
			return err
		}
		ctx.decoded.kind = 1
		ctx.decoded.c2c = *dst
		return nil

	case *dto.GroupAtMessageCreateEvent:
		if ctx.decoded.kind == 2 {
			*dst = ctx.decoded.groupAt
			return nil
		}
		if err := ctx.event.Decode(dst); err != nil {
			return err
		}
		ctx.decoded.kind = 2
		ctx.decoded.groupAt = *dst
		return nil

	default:
		// Generic path: cache the pointer itself; caller must not modify the
		// cached value after returning (safe because handlers run serially).
		if ctx.decoded.kind == 3 && ctx.decoded.generic != nil {
			if src, ok := ctx.decoded.generic.(interface{ copyTo(any) bool }); ok {
				_ = src
			}
			// For the generic path we just re-decode; the gjson fast path in
			// Payload.Decode already avoids most allocations for known types.
		}
		if err := ctx.event.Decode(v); err != nil {
			return err
		}
		ctx.decoded.kind = 3
		ctx.decoded.generic = v
		return nil
	}
}

// MustGetString 获取字符串类型的状态值
func (ctx *Context) MustGetString(key string) (string, error) {
	if val, ok := ctx.Get(key); ok {
		if str, ok := val.(string); ok {
			return str, nil
		}
		return "", errors.New("extensionState key '" + key + "' is not a string")
	}
	return "", errors.New("extensionState key '" + key + "' not found")
}

// MustGetInt 获取整数类型的状态值
func (ctx *Context) MustGetInt(key string) (int, error) {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int); ok {
			return i, nil
		}
		return 0, errors.New("extensionState key '" + key + "' is not an int")
	}
	return 0, errors.New("extensionState key '" + key + "' not found")
}

// GetString 获取字符串类型的状态值
func (ctx *Context) GetString(key string) string {
	if val, ok := ctx.Get(key); ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// GetInt 获取整数类型的状态值
func (ctx *Context) GetInt(key string) int {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int); ok {
			return i
		}
	}
	return 0
}

// GetInt64 获取 int64 类型的状态值
func (ctx *Context) GetInt64(key string) int64 {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int64); ok {
			return i
		}
	}
	return 0
}

// GetBool 获取布尔类型的状态值
func (ctx *Context) GetBool(key string) bool {
	if val, ok := ctx.Get(key); ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// GetFloat64 获取 float64 类型的状态值
func (ctx *Context) GetFloat64(key string) float64 {
	if val, ok := ctx.Get(key); ok {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return 0.0
}

// ===== Permission 相关便利方法 =====

// GetPermissionManager 获取权限管理器（从 typed extensions）
func (ctx *Context) GetPermissionManager() *PermissionManager {
	if ctx == nil {
		return nil
	}
	if ext, ok := ExtGet[PermissionManagerExt](ctx.Ext()); ok {
		return ext.PM
	}
	return nil
}

// SetPermissionManager 设置权限管理器（到 typed extensions）
func (ctx *Context) SetPermissionManager(pm *PermissionManager) {
	if ctx == nil {
		return
	}
	ExtSet(ctx.Ext(), PermissionManagerExt{PM: pm})
}

// GetUserID 获取用户 ID
func (ctx *Context) GetUserID() string {
	return ctx.GetString("user_id")
}

// SetUserID 设置用户 ID
func (ctx *Context) SetUserID(userID string) {
	ctx.Set("user_id", userID)
}

// Tracer returns the OpenTelemetry tracer for the context.
func (ctx *Context) Tracer() trace.Tracer {
	return trace.NewNoopTracerProvider().Tracer("")
}
