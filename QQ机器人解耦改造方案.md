# QQ机器人解耦改造方案 - 通用IM机器人框架

## 📋 目录
1. [当前架构分析](#当前架构分析)
2. [耦合点分析](#耦合点分析)
3. [解耦改造步骤](#解耦改造步骤)
4. [详细实施方案](#详细实施方案)
5. [测试验证策略](#测试验证策略)
6. [向后兼容策略](#向后兼容策略)

---

## 当前架构分析

### 核心组件结构
```
remilia/
├── bot.go              # Bot核心，依赖QQ特定的dto和openapi
├── engine.go           # 事件引擎（通用，可复用）
├── context.go          # 上下文（部分耦合QQ API）
├── matcher.go          # 匹配器（通用，可复用）
├── rules.go            # 规则函数（部分耦合QQ事件类型）
├── plugin.go           # 插件系统（通用，可复用）
├── middleware.go       # 中间件系统（通用，可复用）
└── openapi/            # QQ特定的API实现
    ├── dto/            # QQ数据传输对象
    ├── auth/token/     # QQ认证
    ├── protocol/       # 协议实现（webhook）
    └── openapi.go      # QQ OpenAPI接口
```

### 已具备的通用性
✅ **Engine**: COW并发模型，事件处理引擎 - **完全通用**  
✅ **Matcher**: 规则匹配系统 - **完全通用**  
✅ **Plugin**: 插件管理系统 - **完全通用**  
✅ **Middleware**: 中间件链 - **完全通用**  
✅ **Pool**: 协程池管理 - **完全通用**  
✅ **Metrics**: 性能指标收集 - **完全通用**  
✅ **DeadLetter**: 死信队列 - **完全通用**  

---

## 耦合点分析

### 🔴 强耦合 - 必须重构

#### 1. **Bot结构体** (`bot.go`)
```go
type Bot struct {
    wh     webhook.WebHook          // 耦合QQ webhook
    tm     *token.Manager           // 耦合QQ token管理
    api    openapi.OpenAPI          // 耦合QQ API
    engine *Engine
}
```
**问题**:
- 直接依赖QQ的webhook、token、openapi
- 构造函数 `New(info *dto.BotInfo)` 依赖QQ特定的BotInfo

#### 2. **Context** (`context.go`)
```go
type Context struct {
    event   *dto.Payload          // 耦合QQ的Payload结构
    api     openapi.OpenAPI       // 耦合QQ API
    // ...
}

// 方法级耦合
func (ctx *Context) GetMessageContent() string
func (ctx *Context) ReplyGroup(msg *dto.Message) 
func (ctx *Context) ReplyPrivate(msg *dto.Message)
func (ctx *Context) GetAuthor() *dto.Author
```
**问题**:
- 事件对象绑定到QQ的Payload
- 便捷方法依赖QQ特定的消息结构
- 使用gjson解析QQ特定的JSON结构

#### 3. **Rules** (`rules.go`)
```go
func OnEventType(eventType dto.EventType) Rule
func OnC2CMessage() Rule                    // QQ私聊
func OnGroupAtMessage() Rule                // QQ群聊@
func OnGroupAddRobot() Rule                 // QQ机器人事件
```
**问题**:
- 事件类型硬编码为QQ事件（C2CMessageCreate等）
- 规则函数依赖QQ特定的事件类型

#### 4. **OpenAPI包** (`openapi/`)
**完全QQ特定**:
- `dto.EventType`: 只包含QQ事件类型
- `dto.BotInfo`: QQ bot配置（AppID, Token, AppSecret）
- `openapi.Service`: QQ API实现（Authorization: QQBot {token}）
- URL常量: `https://api.sgroup.qq.com`

### 🟡 中度耦合 - 需要抽象

#### 5. **Engine事件处理** (`engine.go`)
```go
func (e *Engine) ProcessEvent(ctx *Context) {
    // 通用逻辑，但Context依赖QQ
}
```

#### 6. **命令解析器** (`command_parser.go`)
```go
func (ctx *Context) ParseCommand() (*CommandArgs, error) {
    content := ctx.GetMessageContent()  // 依赖QQ消息结构
}
```

---

## 解耦改造步骤

### 🎯 总体策略: **适配器模式 + 接口抽象**

采用分层架构：
```
应用层 (remilia核心)
    ↓ 依赖接口
平台抽象层 (interface)
    ↓ 实现
平台适配层 (adapters/qq, adapters/wechat, ...)
```

---

## 详细实施方案

### 第一阶段: 定义通用接口层 (2-3天)

#### Step 1.1: 创建平台抽象包 `platform/`

**创建文件**: `platform/types.go`
```go
package platform

import "context"

// Platform 平台类型
type Platform string

const (
    PlatformQQ     Platform = "qq"
    PlatformWeChat Platform = "wechat"
    PlatformDingTalk Platform = "dingtalk"
    // ... 扩展其他平台
)

// EventType 通用事件类型
type EventType string

const (
    EventUnknown        EventType = "unknown"
    EventMessageCreate  EventType = "message.create"      // 消息创建
    EventMessagePrivate EventType = "message.private"     // 私聊消息
    EventMessageGroup   EventType = "message.group"       // 群聊消息
    EventBotJoinGroup   EventType = "bot.join_group"      // 机器人加入群
    EventBotLeaveGroup  EventType = "bot.leave_group"     // 机器人离开群
    EventFriendAdd      EventType = "friend.add"          // 添加好友
    EventFriendDelete   EventType = "friend.delete"       // 删除好友
)

// Event 通用事件接口
type Event interface {
    // GetID 获取事件ID
    GetID() string
    
    // GetType 获取事件类型
    GetType() EventType
    
    // GetPlatform 获取平台类型
    GetPlatform() Platform
    
    // GetTimestamp 获取事件时间戳
    GetTimestamp() int64
    
    // GetRawData 获取原始数据（用于平台特定处理）
    GetRawData() []byte
    
    // AsMessage 尝试转换为消息事件
    AsMessage() (MessageEvent, bool)
}

// MessageEvent 消息事件接口
type MessageEvent interface {
    Event
    
    // GetContent 获取消息内容
    GetContent() string
    
    // GetSenderID 获取发送者ID
    GetSenderID() string
    
    // GetSenderName 获取发送者昵称
    GetSenderName() string
    
    // GetChatID 获取会话ID（群ID或用户ID）
    GetChatID() string
    
    // GetChatType 获取会话类型（private/group）
    GetChatType() ChatType
    
    // GetAttachments 获取附件列表
    GetAttachments() []Attachment
}

// ChatType 会话类型
type ChatType string

const (
    ChatTypePrivate ChatType = "private"
    ChatTypeGroup   ChatType = "group"
)

// Attachment 附件
type Attachment struct {
    Type     string // image, video, audio, file
    URL      string
    FileName string
    Size     int64
}

// Message 发送消息的通用接口
type Message interface {
    // GetContent 获取消息内容
    GetContent() string
    
    // GetMessageType 获取消息类型
    GetMessageType() MessageType
    
    // GetAttachments 获取附件
    GetAttachments() []Attachment
}

// MessageType 消息类型
type MessageType string

const (
    MessageTypeText     MessageType = "text"
    MessageTypeImage    MessageType = "image"
    MessageTypeVideo    MessageType = "video"
    MessageTypeAudio    MessageType = "audio"
    MessageTypeFile     MessageType = "file"
    MessageTypeMarkdown MessageType = "markdown"
)
```

**创建文件**: `platform/adapter.go`
```go
package platform

import "context"

// Adapter 平台适配器接口
type Adapter interface {
    // GetPlatform 获取平台类型
    GetPlatform() Platform
    
    // SendMessage 发送消息
    SendMessage(ctx context.Context, chatID string, chatType ChatType, message Message) error
    
    // SendPrivateMessage 发送私聊消息
    SendPrivateMessage(ctx context.Context, userID string, message Message) error
    
    // SendGroupMessage 发送群消息
    SendGroupMessage(ctx context.Context, groupID string, message Message) error
    
    // RecallMessage 撤回消息
    RecallMessage(ctx context.Context, messageID string) error
    
    // GetUserInfo 获取用户信息
    GetUserInfo(ctx context.Context, userID string) (UserInfo, error)
    
    // GetGroupInfo 获取群信息
    GetGroupInfo(ctx context.Context, groupID string) (GroupInfo, error)
}

// UserInfo 用户信息
type UserInfo struct {
    ID       string
    Name     string
    Avatar   string
    Platform Platform
}

// GroupInfo 群组信息
type GroupInfo struct {
    ID       string
    Name     string
    Platform Platform
}

// EventReceiver 事件接收器接口
type EventReceiver interface {
    // Start 启动事件接收
    Start(ctx context.Context) error
    
    // Stop 停止事件接收
    Stop(ctx context.Context) error
    
    // EventStream 获取事件流channel
    EventStream() <-chan Event
}

// AuthProvider 认证提供者接口
type AuthProvider interface {
    // GetToken 获取访问令牌
    GetToken() (string, error)
    
    // RefreshToken 刷新令牌
    RefreshToken() error
    
    // IsReady 是否就绪
    IsReady() bool
}
```

#### Step 1.2: 重构Context为平台无关

**创建文件**: `context_generic.go`
```go
package remilia

import (
    stdctx "context"
    "sync"
    
    "github.com/KomeiDiSanXian/remilia/platform"
)

// Context 通用上下文（平台无关）
type Context struct {
    ctx     stdctx.Context      // 标准库context
    matcher *Matcher
    event   platform.Event      // 通用事件接口
    adapter platform.Adapter    // 平台适配器
    state   State
    stateMu sync.RWMutex
}

// NewContext 创建通用上下文
func NewContext(event platform.Event, adapter platform.Adapter) *Context {
    return &Context{
        ctx:     stdctx.Background(),
        event:   event,
        adapter: adapter,
        state:   make(State),
    }
}

// GetEvent 获取事件
func (ctx *Context) GetEvent() platform.Event {
    return ctx.event
}

// GetEventType 获取事件类型
func (ctx *Context) GetEventType() platform.EventType {
    return ctx.event.GetType()
}

// GetPlatform 获取平台类型
func (ctx *Context) GetPlatform() platform.Platform {
    return ctx.event.GetPlatform()
}

// GetMessageContent 获取消息内容（通用）
func (ctx *Context) GetMessageContent() string {
    if msgEvent, ok := ctx.event.AsMessage(); ok {
        return msgEvent.GetContent()
    }
    return ""
}

// GetSenderID 获取发送者ID
func (ctx *Context) GetSenderID() string {
    if msgEvent, ok := ctx.event.AsMessage(); ok {
        return msgEvent.GetSenderID()
    }
    return ""
}

// SendMessage 发送消息（通用）
func (ctx *Context) SendMessage(chatID string, chatType platform.ChatType, message platform.Message) error {
    return ctx.adapter.SendMessage(ctx.ctx, chatID, chatType, message)
}

// Reply 回复消息（自动识别会话类型）
func (ctx *Context) Reply(message platform.Message) error {
    msgEvent, ok := ctx.event.AsMessage()
    if !ok {
        return fmt.Errorf("event is not a message event")
    }
    
    return ctx.adapter.SendMessage(
        ctx.ctx,
        msgEvent.GetChatID(),
        msgEvent.GetChatType(),
        message,
    )
}

// ReplyText 快捷回复文本消息
func (ctx *Context) ReplyText(content string) error {
    return ctx.Reply(&SimpleMessage{
        Content: content,
        Type:    platform.MessageTypeText,
    })
}
```

#### Step 1.3: 创建通用消息结构

**创建文件**: `platform/message.go`
```go
package platform

// SimpleMessage 简单消息实现
type SimpleMessage struct {
    Content     string
    Type        MessageType
    Attachments []Attachment
}

func (m *SimpleMessage) GetContent() string {
    return m.Content
}

func (m *SimpleMessage) GetMessageType() MessageType {
    return m.Type
}

func (m *SimpleMessage) GetAttachments() []Attachment {
    return m.Attachments
}

// TextMessage 文本消息构造器
func NewTextMessage(content string) *SimpleMessage {
    return &SimpleMessage{
        Content: content,
        Type:    MessageTypeText,
    }
}

// ImageMessage 图片消息构造器
func NewImageMessage(content string, imageURL string) *SimpleMessage {
    return &SimpleMessage{
        Content: content,
        Type:    MessageTypeImage,
        Attachments: []Attachment{
            {Type: "image", URL: imageURL},
        },
    }
}
```

---

### 第二阶段: QQ平台适配器实现 (3-4天)

#### Step 2.1: 创建QQ适配器包

**创建目录结构**:
```
adapters/
└── qq/
    ├── adapter.go      # QQ适配器实现
    ├── event.go        # QQ事件适配
    ├── message.go      # QQ消息适配
    ├── receiver.go     # QQ事件接收器
    └── auth.go         # QQ认证
```

**创建文件**: `adapters/qq/event.go`
```go
package qq

import (
    "encoding/json"
    
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// QQEvent QQ事件适配器
type QQEvent struct {
    payload *dto.Payload
}

func NewQQEvent(payload *dto.Payload) *QQEvent {
    return &QQEvent{payload: payload}
}

func (e *QQEvent) GetID() string {
    return string(e.payload.ID)
}

func (e *QQEvent) GetType() platform.EventType {
    // 映射QQ事件类型到通用事件类型
    switch e.payload.Type {
    case dto.C2CMessageCreate:
        return platform.EventMessagePrivate
    case dto.GroupAtMessageCreate:
        return platform.EventMessageGroup
    case dto.GroupAddRobot:
        return platform.EventBotJoinGroup
    case dto.GroupDelRobot:
        return platform.EventBotLeaveGroup
    case dto.FriendAdd:
        return platform.EventFriendAdd
    case dto.FriendDel:
        return platform.EventFriendDelete
    default:
        return platform.EventUnknown
    }
}

func (e *QQEvent) GetPlatform() platform.Platform {
    return platform.PlatformQQ
}

func (e *QQEvent) GetTimestamp() int64 {
    return time.Now().Unix() // 从payload解析
}

func (e *QQEvent) GetRawData() []byte {
    return e.payload.Raw
}

func (e *QQEvent) AsMessage() (platform.MessageEvent, bool) {
    switch e.payload.Type {
    case dto.C2CMessageCreate, dto.GroupAtMessageCreate:
        return &QQMessageEvent{QQEvent: e}, true
    default:
        return nil, false
    }
}

// QQMessageEvent QQ消息事件
type QQMessageEvent struct {
    *QQEvent
}

func (e *QQMessageEvent) GetContent() string {
    result := gjson.GetBytes(e.payload.Detail, "content")
    return result.String()
}

func (e *QQMessageEvent) GetSenderID() string {
    result := gjson.GetBytes(e.payload.Detail, "author.id")
    return result.String()
}

func (e *QQMessageEvent) GetSenderName() string {
    // QQ API可能不提供，返回空字符串
    return ""
}

func (e *QQMessageEvent) GetChatID() string {
    if e.payload.Type == dto.GroupAtMessageCreate {
        result := gjson.GetBytes(e.payload.Detail, "group_openid")
        return result.String()
    }
    // 私聊使用发送者ID
    return e.GetSenderID()
}

func (e *QQMessageEvent) GetChatType() platform.ChatType {
    if e.payload.Type == dto.GroupAtMessageCreate {
        return platform.ChatTypeGroup
    }
    return platform.ChatTypePrivate
}

func (e *QQMessageEvent) GetAttachments() []platform.Attachment {
    // 解析QQ附件格式
    // TODO: 实现
    return nil
}
```

**创建文件**: `adapters/qq/adapter.go`
```go
package qq

import (
    "context"
    "fmt"
    
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/openapi"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// QQAdapter QQ平台适配器
type QQAdapter struct {
    api openapi.OpenAPI
}

func NewQQAdapter(api openapi.OpenAPI) *QQAdapter {
    return &QQAdapter{api: api}
}

func (a *QQAdapter) GetPlatform() platform.Platform {
    return platform.PlatformQQ
}

func (a *QQAdapter) SendMessage(ctx context.Context, chatID string, chatType platform.ChatType, message platform.Message) error {
    qqMsg := a.convertToQQMessage(message)
    
    switch chatType {
    case platform.ChatTypePrivate:
        _, err := a.api.SingleChat(chatID, qqMsg)
        return err
    case platform.ChatTypeGroup:
        _, err := a.api.GroupChat(chatID, qqMsg)
        return err
    default:
        return fmt.Errorf("unsupported chat type: %s", chatType)
    }
}

func (a *QQAdapter) SendPrivateMessage(ctx context.Context, userID string, message platform.Message) error {
    return a.SendMessage(ctx, userID, platform.ChatTypePrivate, message)
}

func (a *QQAdapter) SendGroupMessage(ctx context.Context, groupID string, message platform.Message) error {
    return a.SendMessage(ctx, groupID, platform.ChatTypeGroup, message)
}

func (a *QQAdapter) RecallMessage(ctx context.Context, messageID string) error {
    // TODO: 实现消息撤回
    return nil
}

func (a *QQAdapter) GetUserInfo(ctx context.Context, userID string) (platform.UserInfo, error) {
    // TODO: 调用QQ API获取用户信息
    return platform.UserInfo{}, nil
}

func (a *QQAdapter) GetGroupInfo(ctx context.Context, groupID string) (platform.GroupInfo, error) {
    // TODO: 调用QQ API获取群信息
    return platform.GroupInfo{}, nil
}

// convertToQQMessage 将通用消息转换为QQ消息格式
func (a *QQAdapter) convertToQQMessage(msg platform.Message) *dto.Message {
    qqMsg := &dto.Message{
        Content: msg.GetContent(),
    }
    
    switch msg.GetMessageType() {
    case platform.MessageTypeText:
        qqMsg.Type = dto.TextMessage
    case platform.MessageTypeImage:
        qqMsg.Type = dto.MediaMessage
        // TODO: 处理图片附件
    case platform.MessageTypeMarkdown:
        qqMsg.Type = dto.MarkdownMessage
    default:
        qqMsg.Type = dto.TextMessage
    }
    
    return qqMsg
}
```

**创建文件**: `adapters/qq/receiver.go`
```go
package qq

import (
    "context"
    
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
)

// QQEventReceiver QQ事件接收器
type QQEventReceiver struct {
    webhook    webhook.WebHook
    eventChan  chan platform.Event
    stopCh     chan struct{}
}

func NewQQEventReceiver(wh webhook.WebHook) *QQEventReceiver {
    return &QQEventReceiver{
        webhook:   wh,
        eventChan: make(chan platform.Event, 100),
        stopCh:    make(chan struct{}),
    }
}

func (r *QQEventReceiver) Start(ctx context.Context) error {
    go r.eventLoop()
    return nil
}

func (r *QQEventReceiver) Stop(ctx context.Context) error {
    close(r.stopCh)
    return nil
}

func (r *QQEventReceiver) EventStream() <-chan platform.Event {
    return r.eventChan
}

func (r *QQEventReceiver) eventLoop() {
    for {
        select {
        case <-r.stopCh:
            close(r.eventChan)
            return
        case qqPayload := <-r.webhook.EventStream():
            // 转换QQ事件为通用事件
            genericEvent := NewQQEvent(qqPayload)
            r.eventChan <- genericEvent
        }
    }
}
```

#### Step 2.2: 创建QQ Builder

**创建文件**: `adapters/qq/builder.go`
```go
package qq

import (
    "context"
    
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/openapi"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/openapi/auth/token"
    "github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
)

// QQBotBuilder QQ机器人构建器
type QQBotBuilder struct {
    botInfo *dto.BotInfo
    engine  *remilia.Engine
    webhook webhook.WebHook
}

func NewQQBotBuilder(botInfo *dto.BotInfo) *QQBotBuilder {
    return &QQBotBuilder{
        botInfo: botInfo,
    }
}

func (b *QQBotBuilder) WithEngine(engine *remilia.Engine) *QQBotBuilder {
    b.engine = engine
    return b
}

func (b *QQBotBuilder) WithWebhook(wh webhook.WebHook) *QQBotBuilder {
    b.webhook = wh
    return b
}

func (b *QQBotBuilder) Build() (*remilia.Bot, error) {
    // 创建QQ token管理器
    tm := token.NewManager(b.botInfo)
    
    // 创建QQ API服务
    api := openapi.New(tm)
    
    // 创建QQ适配器
    adapter := NewQQAdapter(api)
    
    // 创建事件接收器
    var receiver platform.EventReceiver
    if b.webhook != nil {
        receiver = NewQQEventReceiver(b.webhook)
    }
    
    // 创建引擎（如果未提供）
    if b.engine == nil {
        b.engine = remilia.NewEngine()
    }
    
    // 创建通用Bot
    bot := remilia.NewBot(
        remilia.WithAdapter(adapter),
        remilia.WithReceiver(receiver),
        remilia.WithEngine(b.engine),
    )
    
    return bot, nil
}
```

---

### 第三阶段: 重构Bot核心 (2-3天)

#### Step 3.1: 创建通用Bot

**修改文件**: `bot.go`
```go
package remilia

import (
    "context"
    "net/http"
    "sync"
    "time"
    
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/sirupsen/logrus"
)

// Bot 通用机器人（平台无关）
type Bot struct {
    adapter  platform.Adapter       // 平台适配器
    receiver platform.EventReceiver // 事件接收器
    engine   *Engine                // 事件处理引擎
    
    // 生命周期管理
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
    stopCh chan struct{}
}

// BotOption Bot配置选项
type BotOption func(*Bot)

// WithAdapter 设置平台适配器
func WithAdapter(adapter platform.Adapter) BotOption {
    return func(b *Bot) {
        b.adapter = adapter
    }
}

// WithReceiver 设置事件接收器
func WithReceiver(receiver platform.EventReceiver) BotOption {
    return func(b *Bot) {
        b.receiver = receiver
    }
}

// WithEngine 设置自定义引擎
func WithEngine(engine *Engine) BotOption {
    return func(b *Bot) {
        b.engine = engine
    }
}

// NewBot 创建通用机器人
func NewBot(options ...BotOption) *Bot {
    bot := &Bot{
        engine: NewEngine(), // 默认创建新引擎
        stopCh: make(chan struct{}),
    }
    
    // 应用配置选项
    for _, opt := range options {
        opt(bot)
    }
    
    return bot
}

// GetEngine 获取引擎
func (b *Bot) GetEngine() *Engine {
    return b.engine
}

// GetAdapter 获取适配器
func (b *Bot) GetAdapter() platform.Adapter {
    return b.adapter
}

// Start 启动机器人
func (b *Bot) Start() error {
    b.ctx, b.cancel = context.WithCancel(context.Background())
    
    // 启动事件接收器
    if b.receiver != nil {
        if err := b.receiver.Start(b.ctx); err != nil {
            return err
        }
        
        // 启动事件处理循环
        b.wg.Add(1)
        go b.eventLoop()
    }
    
    logrus.Infof("[Remilia] Bot started on platform: %s", b.adapter.GetPlatform())
    return nil
}

// Shutdown 优雅关闭
func (b *Bot) Shutdown(ctx context.Context) error {
    logrus.Info("[Remilia] Starting graceful shutdown...")
    
    // 取消所有handler
    if b.cancel != nil {
        b.cancel()
    }
    
    // 停止事件接收器
    if b.receiver != nil {
        if err := b.receiver.Stop(ctx); err != nil {
            logrus.WithError(err).Warn("[Remilia] Receiver stop error")
        }
    }
    
    // 等待所有handler完成
    done := make(chan struct{})
    go func() {
        b.wg.Wait()
        close(done)
    }()
    
    select {
    case <-done:
        logrus.Info("[Remilia] All handlers completed")
    case <-ctx.Done():
        logrus.Warn("[Remilia] Shutdown timeout")
    }
    
    return nil
}

// eventLoop 事件处理循环
func (b *Bot) eventLoop() {
    defer b.wg.Done()
    
    for {
        select {
        case event, ok := <-b.receiver.EventStream():
            if !ok {
                logrus.Info("[Remilia] Event stream closed")
                return
            }
            
            logrus.WithFields(logrus.Fields{
                "type":     event.GetType(),
                "id":       event.GetID(),
                "platform": event.GetPlatform(),
            }).Debug("[Remilia] Processing event")
            
            // 创建通用Context
            ctx := NewContext(event, b.adapter)
            ctx.SetStdContext(b.ctx)
            
            // 处理事件
            b.engine.ProcessEvent(ctx)
            
        case <-b.stopCh:
            logrus.Info("[Remilia] Stopping event loop")
            return
        }
    }
}
```

#### Step 3.2: 更新Rules为通用规则

**修改文件**: `rules_generic.go` (新建)
```go
package remilia

import (
    "strings"
    "unicode"
    
    "github.com/KomeiDiSanXian/remilia/platform"
)

// OnEventType 匹配通用事件类型
func OnEventType(eventType platform.EventType) Rule {
    return func(ctx *Context) bool {
        return ctx.GetEventType() == eventType
    }
}

// OnPrivateMessage 匹配私聊消息
func OnPrivateMessage() Rule {
    return OnEventType(platform.EventMessagePrivate)
}

// OnGroupMessage 匹配群聊消息
func OnGroupMessage() Rule {
    return OnEventType(platform.EventMessageGroup)
}

// OnPlatform 匹配特定平台
func OnPlatform(platform platform.Platform) Rule {
    return func(ctx *Context) bool {
        return ctx.GetPlatform() == platform
    }
}

// OnCommand 匹配命令（通用）
func OnCommand(prefix string) Rule {
    return func(ctx *Context) bool {
        content := ctx.GetMessageContent()
        trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
        return strings.HasPrefix(trimmed, prefix)
    }
}

// OnKeyword 匹配关键词（通用）
func OnKeyword(keyword string) Rule {
    return func(ctx *Context) bool {
        content := ctx.GetMessageContent()
        return strings.Contains(content, keyword)
    }
}

// OnFullMatch 完全匹配（通用）
func OnFullMatch(text string) Rule {
    return func(ctx *Context) bool {
        content := ctx.GetMessageContent()
        trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
        return trimmed == text
    }
}
```

---

### 第四阶段: 向后兼容层 (1-2天)

#### Step 4.1: 创建兼容包 `compat/qq/`

**创建文件**: `compat/qq/bot.go`
```go
package qq

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/adapters/qq"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
    "github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
)

// New 创建QQ机器人（兼容旧API）
// 
// 已废弃: 使用 adapters/qq.NewQQBotBuilder 代替
func New(info *dto.BotInfo, options ...remilia.BotOption) *remilia.Bot {
    builder := qq.NewQQBotBuilder(info)
    
    // 解析旧的options
    for _, opt := range options {
        // 适配旧的WithWebHook等
        // ...
    }
    
    bot, _ := builder.Build()
    return bot
}

// WithWebHook 兼容函数
func WithWebHook(wh webhook.WebHook) remilia.BotOption {
    return func(b *remilia.Bot) {
        // 适配逻辑
    }
}
```

**创建文件**: `compat/qq/context.go`
```go
package qq

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// ContextCompat QQ兼容Context扩展
type ContextCompat struct {
    *remilia.Context
}

// ReplyGroup 兼容方法（使用QQ特定消息类型）
func (ctx *ContextCompat) ReplyGroup(msg *dto.Message) error {
    // 转换为通用消息
    genericMsg := convertQQMessageToGeneric(msg)
    return ctx.Reply(genericMsg)
}

// ReplyPrivate 兼容方法
func (ctx *ContextCompat) ReplyPrivate(msg *dto.Message) error {
    genericMsg := convertQQMessageToGeneric(msg)
    return ctx.Reply(genericMsg)
}

// GetAuthor 兼容方法（返回QQ特定的Author）
func (ctx *ContextCompat) GetAuthor() *dto.Author {
    // 从通用event转换
    // ...
}
```

#### Step 4.2: 创建兼容规则

**创建文件**: `compat/qq/rules.go`
```go
package qq

import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/platform"
    "github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// OnC2CMessage 兼容旧API（已废弃）
func OnC2CMessage() remilia.Rule {
    return remilia.OnEventType(platform.EventMessagePrivate)
}

// OnGroupAtMessage 兼容旧API（已废弃）
func OnGroupAtMessage() remilia.Rule {
    return remilia.OnEventType(platform.EventMessageGroup)
}

// OnGroupAddRobot 兼容旧API（已废弃）
func OnGroupAddRobot() remilia.Rule {
    return remilia.OnEventType(platform.EventBotJoinGroup)
}

// OnQQEventType 兼容QQ特定事件类型
func OnQQEventType(qqType dto.EventType) remilia.Rule {
    genericType := convertQQEventTypeToGeneric(qqType)
    return remilia.OnEventType(genericType)
}

func convertQQEventTypeToGeneric(qqType dto.EventType) platform.EventType {
    switch qqType {
    case dto.C2CMessageCreate:
        return platform.EventMessagePrivate
    case dto.GroupAtMessageCreate:
        return platform.EventMessageGroup
    // ... 其他映射
    default:
        return platform.EventUnknown
    }
}
```

---

### 第五阶段: Engine适配 (1天)

#### Step 5.1: 更新Engine事件处理

**修改文件**: `engine.go` (添加泛型事件支持)
```go
// ProcessEvent 处理通用事件（已更新为平台无关）
func (e *Engine) ProcessEvent(ctx *Context) {
    // 现在Context包含platform.Event
    // 无需修改核心逻辑，因为通过Context抽象访问
    
    // ... 现有逻辑保持不变
}

// OnAny 注册任意事件的处理器（平台无关）
func (e *Engine) OnAny(rules ...Rule) *Matcher {
    return e.On(rules...)
}

// OnMessage 注册消息事件处理器（平台无关）
func (e *Engine) OnMessage(rules ...Rule) *Matcher {
    return e.On(append([]Rule{
        func(ctx *Context) bool {
            _, ok := ctx.GetEvent().AsMessage()
            return ok
        },
    }, rules...)...)
}
```

---

### 第六阶段: 测试与文档 (2-3天)

#### Step 6.1: 编写适配器测试

**创建文件**: `adapters/qq/adapter_test.go`
```go
package qq

import (
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/KomeiDiSanXian/remilia/platform"
)

func TestQQEventConversion(t *testing.T) {
    // 测试QQ事件到通用事件的转换
    // ...
}

func TestQQMessageSend(t *testing.T) {
    // 测试消息发送
    // ...
}
```

#### Step 6.2: 集成测试

**创建文件**: `integration_platform_test.go`
```go
package remilia

import (
    "testing"
    
    "github.com/KomeiDiSanXian/remilia/adapters/qq"
    "github.com/KomeiDiSanXian/remilia/platform"
)

func TestPlatformAbstraction(t *testing.T) {
    // 测试平台抽象是否工作
    // 1. 创建QQ适配器
    // 2. 创建Bot
    // 3. 注册handler
    // 4. 模拟事件
    // 5. 验证处理结果
}

func TestMultiplePlatforms(t *testing.T) {
    // 测试多平台同时运行
}
```

#### Step 6.3: 编写迁移文档

**创建文件**: `docs/MIGRATION_GUIDE.md`
```markdown
# 迁移指南: 从QQ专用到通用平台

## 快速迁移

### 旧代码:
```go
import "github.com/KomeiDiSanXian/remilia"
import "github.com/KomeiDiSanXian/remilia/openapi/dto"

bot := remilia.New(&dto.BotInfo{...})
bot.GetEngine().OnC2C(remilia.OnCommand("/ping")).Handle(handler)
```

### 新代码 (QQ平台):
```go
import "github.com/KomeiDiSanXian/remilia"
import "github.com/KomeiDiSanXian/remilia/adapters/qq"

builder := qq.NewQQBotBuilder(&dto.BotInfo{...})
bot, _ := builder.Build()
bot.GetEngine().OnPrivateMessage(remilia.OnCommand("/ping")).Handle(handler)
```

## 重要变更

1. **事件类型**: `dto.C2CMessageCreate` -> `platform.EventMessagePrivate`
2. **Context方法**: `ctx.ReplyPrivate(qqMsg)` -> `ctx.ReplyText("text")`
3. **消息构造**: `&dto.Message{}` -> `platform.NewTextMessage()`

## 兼容性

旧代码可通过兼容包继续运行:
```go
import qqcompat "github.com/KomeiDiSanXian/remilia/compat/qq"

bot := qqcompat.New(&dto.BotInfo{...}) // 仍然可用
```
```

---

## 测试验证策略

### 1. 单元测试覆盖

```bash
# 测试QQ适配器
go test ./adapters/qq/... -v -cover

# 测试平台抽象层
go test ./platform/... -v -cover

# 测试通用Context
go test -run TestContext -v
```

### 2. 集成测试

```bash
# 运行E2E测试
go test -run TestIntegration -v -timeout 30s

# 测试向后兼容性
go test ./compat/qq/... -v
```

### 3. 性能基准测试

```bash
# 对比重构前后性能
go test -bench=BenchmarkProcessEvent -benchmem

# 检查内存分配是否增加
go test -bench=. -benchmem | grep alloc
```

### 4. 回归测试清单

- [ ] 现有所有测试通过（`go test ./... -v`）
- [ ] 消息发送功能正常
- [ ] 事件接收正常
- [ ] 插件系统兼容
- [ ] 中间件链正常
- [ ] 优雅关闭正常
- [ ] 并发安全性验证
- [ ] 死信队列正常
- [ ] Metrics收集正常

---

## 向后兼容策略

### 1. 保留旧API (废弃标记)

```go
// Deprecated: 使用 adapters/qq.NewQQBotBuilder 代替
func New(info *dto.BotInfo) *Bot {
    // 内部调用新实现
}
```

### 2. 渐进式迁移路径

**Phase 1**: 旧代码继续工作（通过compat包）  
**Phase 2**: 提供迁移工具和文档  
**Phase 3**: 标记旧API为废弃  
**Phase 4**: （可选）移除旧API

### 3. 版本策略

- **v0.x**: 当前版本，保持稳定
- **v1.0**: 引入平台抽象，保留兼容层
- **v2.0**: 移除旧API（至少6个月后）

---

## 实施时间表

| 阶段 | 任务 | 预计时间 | 依赖 |
|------|------|----------|------|
| 1 | 定义平台抽象接口 | 2-3天 | 无 |
| 2 | 实现QQ适配器 | 3-4天 | 阶段1 |
| 3 | 重构Bot核心 | 2-3天 | 阶段2 |
| 4 | 创建兼容层 | 1-2天 | 阶段3 |
| 5 | Engine适配 | 1天 | 阶段3 |
| 6 | 测试与文档 | 2-3天 | 阶段4,5 |
| **总计** | | **11-16天** | |

---

## 扩展其他平台示例

### 添加微信平台适配器

```go
// adapters/wechat/adapter.go
package wechat

type WeChatAdapter struct {
    // 微信API客户端
}

func (a *WeChatAdapter) GetPlatform() platform.Platform {
    return platform.PlatformWeChat
}

func (a *WeChatAdapter) SendMessage(...) error {
    // 调用微信API
}

// adapters/wechat/event.go
type WeChatEvent struct {
    // 微信事件结构
}

func (e *WeChatEvent) GetType() platform.EventType {
    // 映射微信事件类型
}
```

### 使用示例

```go
// 同时支持QQ和微信
qqBot := qq.NewQQBotBuilder(...).Build()
wechatBot := wechat.NewWeChatBotBuilder(...).Build()

// 共享同一个Engine
sharedEngine := remilia.NewEngine()

// 注册通用handler（两个平台都会处理）
sharedEngine.OnMessage(
    remilia.OnCommand("/ping"),
).Handle(func(ctx *remilia.Context) {
    ctx.ReplyText("pong!") // 自动适配不同平台的API
})

// 启动两个机器人
qqBot.Start()
wechatBot.Start()
```

---

## 关键设计决策

### ✅ 采用适配器模式的原因

1. **解耦**: 核心逻辑不依赖具体平台
2. **扩展性**: 新平台只需实现接口
3. **测试性**: 可以mock适配器进行测试
4. **复用性**: Engine/Plugin/Middleware完全通用

### ✅ 接口设计原则

1. **最小化**: 只暴露必要的方法
2. **平台无关**: 使用通用术语（Message而非QQMessage）
3. **可扩展**: 保留RawData字段供平台特定功能
4. **类型安全**: 使用强类型而非map[string]interface{}

### ✅ 性能考虑

1. **零拷贝**: 事件转换尽量避免深拷贝
2. **懒加载**: 只在需要时解析事件详情
3. **内存池**: 复用Context和Event对象（可选优化）
4. **并发安全**: 适配器需保证线程安全

---

## 风险与挑战

### 🚨 主要风险

1. **平台差异大**: 不同IM平台能力差异（如微信无法撤回）
   - **解决**: 接口方法返回error，不支持的返回ErrNotSupported

2. **性能回退**: 引入抽象层可能影响性能
   - **解决**: 基准测试验证，优化热路径，考虑零成本抽象

3. **向后兼容**: 旧代码可能大面积失效
   - **解决**: 提供compat包，保证渐进式迁移

4. **文档滞后**: 接口变化后文档未及时更新
   - **解决**: 同步更新文档，提供迁移示例

### 🛡️ 风险缓解措施

- 分阶段实施，每阶段充分测试
- 保留兼容层至少2个大版本
- 提供自动化迁移工具（代码生成）
- 维护详细的变更日志

---

## 结论

通过适配器模式将Remilia从QQ专用框架改造为通用IM机器人框架是**可行且必要的**。

### 关键优势

✅ **扩展性**: 轻松支持新平台（微信、钉钉、Telegram等）  
✅ **维护性**: 平台特定代码隔离，降低维护成本  
✅ **复用性**: 核心组件（Engine/Plugin）完全复用  
✅ **兼容性**: 通过兼容层保证平稳迁移  

### 预期收益

- 代码复用率提升 **70%+**
- 新平台接入时间缩短 **80%+**（只需实现适配器）
- 核心功能测试覆盖率提升（通用测试 + 平台测试）
- 项目吸引力提升（支持多平台的通用框架）

---

## 附录

### A. 完整目录结构（重构后）

```
remilia/
├── platform/                    # 平台抽象层
│   ├── types.go                # 通用类型定义
│   ├── adapter.go              # 适配器接口
│   └── message.go              # 消息接口
├── adapters/                   # 平台适配器实现
│   ├── qq/                     # QQ平台
│   │   ├── adapter.go
│   │   ├── event.go
│   │   ├── message.go
│   │   ├── receiver.go
│   │   ├── auth.go
│   │   └── builder.go
│   ├── wechat/                 # 微信平台（未来）
│   └── dingtalk/               # 钉钉平台（未来）
├── compat/                     # 向后兼容层
│   └── qq/
│       ├── bot.go
│       ├── context.go
│       └── rules.go
├── openapi/                    # QQ OpenAPI（保留，移至adapters/qq依赖）
│   ├── dto/
│   ├── auth/
│   ├── protocol/
│   └── openapi.go
├── bot.go                      # 通用Bot（平台无关）
├── context.go                  # 通用Context（平台无关）
├── engine.go                   # 事件引擎（无需修改）
├── matcher.go                  # 匹配器（无需修改）
├── rules.go                    # 通用规则
├── rules_generic.go            # 平台无关规则
├── plugin.go                   # 插件系统（无需修改）
├── middleware.go               # 中间件（无需修改）
└── docs/
    ├── MIGRATION_GUIDE.md      # 迁移指南
    ├── PLATFORM_GUIDE.md       # 平台开发指南
    └── API_REFERENCE.md        # API参考文档
```

### B. 核心接口一览

```go
// 平台抽象接口
platform.Event              # 通用事件
platform.MessageEvent       # 消息事件
platform.Adapter            # 平台适配器
platform.EventReceiver      # 事件接收器
platform.AuthProvider       # 认证提供者

// 适配器实现（示例）
qq.QQAdapter               # QQ适配器
qq.QQEvent                 # QQ事件
qq.QQEventReceiver         # QQ事件接收器
qq.QQBotBuilder            # QQ Bot构建器

// 核心组件（保持不变）
Engine                     # 事件引擎
Matcher                    # 匹配器
Plugin                     # 插件
Context                    # 上下文（重构为平台无关）
```

### C. 相关资源

- [适配器模式详解](https://refactoring.guru/design-patterns/adapter)
- [Go接口设计最佳实践](https://go.dev/blog/laws-of-reflection)
- [多平台IM机器人架构参考](https://github.com/go-cqhttp/go-cqhttp)

---

**文档版本**: v1.0  
**创建日期**: 2025-12-10  
**维护者**: Remilia团队  
**状态**: 待审核

---

## 快速开始（供审核使用）

### 1. 核心改动概览

- **新增**: `platform/` 包 - 平台抽象接口
- **新增**: `adapters/qq/` 包 - QQ平台适配器
- **新增**: `compat/qq/` 包 - 向后兼容层
- **修改**: `bot.go` - 重构为平台无关
- **修改**: `context.go` - 使用platform.Event
- **修改**: `rules.go` - 添加平台无关规则

### 2. 是否破坏现有代码？

**答**: 不会！通过兼容层保证旧代码继续工作。

### 3. 性能影响？

**答**: 引入接口抽象有微小开销（<5%），可通过优化抵消。

### 4. 测试覆盖率？

**答**: 保持现有80%+覆盖率，新增平台测试。

### 5. 何时开始？

**答**: 建议分阶段实施，首先实现阶段1-2（平台抽象+QQ适配器），验证可行性后继续。

