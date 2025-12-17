package remilia

import (
	stdctx "context" // 标准库 context，使用别名避免命名冲突
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// State 上下文状态
type State map[string]any

// Context 上下文
type Context struct {
	ctx     stdctx.Context // 标准库 context，用于超时控制、取消传播等
	matcher *Matcher
	event   *dto.Payload
	state   State
	stateMu sync.RWMutex
	api     openapi.OpenAPI
}

// NewContext 创建一个新的上下文
func NewContext(event *dto.Payload, api openapi.OpenAPI) *Context {
	return &Context{
		ctx:   stdctx.Background(),
		event: event,
		api:   api,
		state: make(State),
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
	c.ctx = ctx
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
	if ctx.ctx == nil {
		ctx.ctx = stdctx.Background()
	}
	return ctx.ctx
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
	ctx.ctx = stdCtx
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
		ctx:     ctx.ctx,
		matcher: ctx.matcher,
		event:   ctx.event,
		api:     ctx.api,
		state:   make(State),
		stateMu: sync.RWMutex{},
	}

	// 深拷贝 State map
	ctx.stateMu.RLock()
	for k, v := range ctx.state {
		newCtx.state[k] = v
	}
	ctx.stateMu.RUnlock()

	return newCtx
}

// SetState 设置状态值（线程安全）
func (ctx *Context) SetState(key string, value any) {
	ctx.stateMu.Lock()
	defer ctx.stateMu.Unlock()
	ctx.state[key] = value
}

// GetState 获取状态值（线程安全）
func (ctx *Context) GetState(key string) (any, bool) {
	ctx.stateMu.RLock()
	defer ctx.stateMu.RUnlock()
	val, ok := ctx.state[key]
	return val, ok
}

// GetAllState 获取所有状态的副本（线程安全）
func (ctx *Context) GetAllState() State {
	ctx.stateMu.RLock()
	defer ctx.stateMu.RUnlock()

	stateCopy := make(State, len(ctx.state))
	for k, v := range ctx.state {
		stateCopy[k] = v
	}
	return stateCopy
}

// DeleteState 删除状态值（线程安全）
func (ctx *Context) DeleteState(key string) {
	ctx.stateMu.Lock()
	defer ctx.stateMu.Unlock()
	delete(ctx.state, key)
}

// GetString 获取字符串类型的状态值，如果不存在或类型不匹配返回空字符串
func (ctx *Context) GetString(key string) string {
	if val, ok := ctx.GetState(key); ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// GetInt 获取整数类型的状态值，如果不存在或类型不匹配返回 0
func (ctx *Context) GetInt(key string) int {
	if val, ok := ctx.GetState(key); ok {
		if i, ok := val.(int); ok {
			return i
		}
	}
	return 0
}

// GetInt64 获取 int64 类型的状态值，如果不存在或类型不匹配返回 0
func (ctx *Context) GetInt64(key string) int64 {
	if val, ok := ctx.GetState(key); ok {
		if i, ok := val.(int64); ok {
			return i
		}
	}
	return 0
}

// GetBool 获取布尔类型的状态值，如果不存在或类型不匹配返回 false
func (ctx *Context) GetBool(key string) bool {
	if val, ok := ctx.GetState(key); ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// GetFloat64 获取 float64 类型的状态值，如果不存在或类型不匹配返回 0.0
func (ctx *Context) GetFloat64(key string) float64 {
	if val, ok := ctx.GetState(key); ok {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return 0.0
}

// MustGetString 获取字符串类型的状态值，如果不存在或类型不匹配返回错误
func (ctx *Context) MustGetString(key string) (string, error) {
	if val, ok := ctx.GetState(key); ok {
		if str, ok := val.(string); ok {
			return str, nil
		}
		return "", fmt.Errorf("state key '%s' is not a string", key)
	}
	return "", fmt.Errorf("state key '%s' not found", key)
}

// MustGetInt 获取整数类型的状态值，如果不存在或类型不匹配返回错误
func (ctx *Context) MustGetInt(key string) (int, error) {
	if val, ok := ctx.GetState(key); ok {
		if i, ok := val.(int); ok {
			return i, nil
		}
		return 0, fmt.Errorf("state key '%s' is not an int", key)
	}
	return 0, fmt.Errorf("state key '%s' not found", key)
}

// SetStateMap 批量设置状态值（线程安全）
func (ctx *Context) SetStateMap(data State) {
	ctx.stateMu.Lock()
	defer ctx.stateMu.Unlock()
	for k, v := range data {
		ctx.state[k] = v
	}
}

// GetStateKeys 获取多个状态值（线程安全）
func (ctx *Context) GetStateKeys(keys ...string) State {
	ctx.stateMu.RLock()
	defer ctx.stateMu.RUnlock()

	result := make(State, len(keys))
	for _, key := range keys {
		if val, ok := ctx.state[key]; ok {
			result[key] = val
		}
	}
	return result
}

// GetEvent 获取事件
func (ctx *Context) GetEvent() *dto.Payload {
	return ctx.event
}

// GetEventType 获取事件类型
func (ctx *Context) GetEventType() dto.EventType {
	return ctx.event.Type
}

// DecodeEvent 解码事件详情
func (ctx *Context) DecodeEvent(v any) error {
	return ctx.event.Decode(v)
}

// ErrNilAPI 表示 OpenAPI 未初始化
var ErrNilAPI = errors.New("openAPI is nil")

// SendGroupMessage 发送群聊消息
func (ctx *Context) SendGroupMessage(groupID string, msg *dto.Message) (gjson.Result, error) {
	if ctx.api == nil {
		logrus.Error("[Context] OpenAPI is nil")
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.GroupChat(groupID, msg)
}

// SendSingleMessage 发送私聊消息
func (ctx *Context) SendSingleMessage(openID string, msg *dto.Message) (gjson.Result, error) {
	if ctx.api == nil {
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

	// 设置 message_id 用于消息引用
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

	// 设置 message_id 用于消息引用
	if msg.MessageID == "" {
		msg.MessageID = event.ID
	}

	return ctx.SendSingleMessage(event.Author.UserOpenID, msg)
}

// SendPrivateFile 私聊发送富媒体消息
func (ctx *Context) SendPrivateFile(file *dto.Media) (gjson.Result, error) {
	if ctx.api == nil {
		logrus.Error("[Context] OpenAPI is nil")
		return gjson.Result{}, ErrNilAPI
	}
	author := ctx.GetAuthor()
	if author == nil {
		return gjson.Result{}, fmt.Errorf("author not available for private file send")
	}
	return ctx.api.SingleRichMedia(author.UserOpenID, file)
}

// SendGroupFile 群聊发送富媒体消息
func (ctx *Context) SendGroupFile(file *dto.Media) (gjson.Result, error) {
	var event dto.GroupAtMessageCreateEvent
	if err := ctx.DecodeEvent(&event); err != nil {
		logrus.WithError(err).Error("[Context] Failed to decode group event")
		return gjson.Result{}, err
	}
	return ctx.api.GroupRichMedia(event.GroupOpenID, file)
}

// ReplyPrivatePicture 私聊发送图片消息
func (ctx *Context) ReplyPrivatePicture(content string, pictureURL string) (gjson.Result, error) {
	r, err := ctx.SendPrivateFile(&dto.Media{
		Type:       dto.ImageFile,
		URL:        pictureURL,
		ActiveSend: false,
	})
	if err != nil {
		return gjson.Result{}, err
	}
	return ctx.ReplyPrivate(&dto.Message{
		Content: content,
		Type:    dto.MediaMessage,
		Media: &dto.MediaResponse{
			FileInfo: r.Get("file_info").Str,
		},
	})
}

// ReplyGroupPicture 群聊发送图片消息
func (ctx *Context) ReplyGroupPicture(content string, pictureURL string) (gjson.Result, error) {
	r, err := ctx.SendGroupFile(&dto.Media{
		Type:       dto.ImageFile,
		URL:        pictureURL,
		ActiveSend: false,
	})
	if err != nil {
		return gjson.Result{}, err
	}
	return ctx.ReplyGroup(&dto.Message{
		Content: content,
		Type:    dto.MediaMessage,
		Media: &dto.MediaResponse{
			FileInfo: r.Get("file_info").Str,
		},
	})
}

// CancelPrivateMessage 撤回私聊消息
func (ctx *Context) CancelPrivateMessage(userOpenID string, messageID string) (gjson.Result, error) {
	if ctx.api == nil {
		logrus.Error("[Context] OpenAPI is nil")
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.SingleReset(userOpenID, messageID)
}

// CancelGroupMessage 撤回群聊消息
func (ctx *Context) CancelGroupMessage(groupOpenID string, messageID string) (gjson.Result, error) {
	if ctx.api == nil {
		logrus.Error("[Context] OpenAPI is nil")
		return gjson.Result{}, ErrNilAPI
	}
	return ctx.api.GroupReset(groupOpenID, messageID)
}

// GetMessageContent 获取消息内容（零拷贝优化）
// 使用 gjson 直接提取字段，避免完整 JSON 解析
func (ctx *Context) GetMessageContent() string {
	result := gjson.GetBytes(ctx.event.Detail, "content")
	return result.String()
}

// GetAuthor 获取消息作者信息（零拷贝优化）
// 使用 gjson 直接提取 author 字段，避免完整结构体解析
// 返回临时 Author 对象，调用者不应保存指针
func (ctx *Context) GetAuthor() *dto.Author {
	result := gjson.GetBytes(ctx.event.Detail, "author")
	if !result.Exists() {
		return nil
	}

	var author dto.Author
	if err := json.Unmarshal([]byte(result.Raw), &author); err != nil {
		logrus.WithError(err).Warn("[Context] Failed to unmarshal author")
		return nil
	}
	return &author
}

// At 生成 @ 用户字符串
//
// Caution: 在markdown消息中才能使用, 普通消息无需调用此方法
func (ctx *Context) At() string {
	author := ctx.GetAuthor()
	if author == nil {
		logrus.Warn("[Context] Author not available for At()")
		return ""
	}
	return dto.At(author.ID)
}

// AtAll 生成 @ 全体成员字符串
//
// Caution: 在markdown消息中才能使用, 普通消息无需调用此方法
func (ctx *Context) AtAll() string {
	return dto.AtAll()
}
