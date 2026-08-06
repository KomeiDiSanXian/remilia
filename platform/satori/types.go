// Package satori 实现了基于 Satori 协议的 platform.Adapter，
// 可连接任意兼容 Satori 协议的 SDK（如 Chronocat、Lagrange、Koishi 等）。
//
// Satori 协议规范：https://satori.chat/zh-CN/protocol/
package satori

import "encoding/json"

// ─────────────────────────────────────────────────────────────────────────────
// Opcode
// ─────────────────────────────────────────────────────────────────────────────

// Opcode 是 Satori 协议定义的 WebSocket 信令类型枚举。
type Opcode int

const (
	OpcodeEvent    Opcode = 0 // EVENT    – SDK 向应用推送事件
	OpcodePing     Opcode = 1 // PING     – 应用向 SDK 发送心跳
	OpcodePong     Opcode = 2 // PONG     – SDK 回复心跳
	OpcodeIdentify Opcode = 3 // IDENTIFY – 应用向 SDK 发送鉴权/恢复会话
	OpcodeReady    Opcode = 4 // READY    – SDK 在 IDENTIFY 后回复鉴权成功
	OpcodeMeta     Opcode = 5 // META     – 实验性：SDK 推送元信息更新
)

// ─────────────────────────────────────────────────────────────────────────────
// WebSocket 信令封装
// ─────────────────────────────────────────────────────────────────────────────

// Signal 是 WebSocket 消息的顶层信令封装。
//
//	{ "op": 0, "body": { ... } }
type Signal struct {
	Op   Opcode          `json:"op"`
	Body json.RawMessage `json:"body,omitempty"`
}

// IdentifyBody 是 IDENTIFY 信令的 body 数据。
type IdentifyBody struct {
	Token string `json:"token,omitempty"` // 鉴权令牌；若 SDK 未配置鉴权则省略
	SN    *int64 `json:"sn,omitempty"`    // 上次收到的事件序列号，用于会话恢复
}

// ReadyBody 是 READY 信令的 body 数据。
type ReadyBody struct {
	Logins    []*Login `json:"logins"`
	ProxyURLs []string `json:"proxy_urls,omitempty"` // 代理路由列表
}

// MetaBody 是 META 信令的 body 数据（实验性）。
type MetaBody struct {
	ProxyURLs []string `json:"proxy_urls,omitempty"` // 代理路由列表
}

// ─────────────────────────────────────────────────────────────────────────────
// Event
// ─────────────────────────────────────────────────────────────────────────────

// Event 是 EVENT 信令 body 中携带的 Satori 事件对象。
type Event struct {
	SN        int64        `json:"sn"`
	Type      string       `json:"type"`
	Timestamp int64        `json:"timestamp"` // 毫秒时间戳
	Login     *Login       `json:"login,omitempty"`
	Argv      *Argv        `json:"argv,omitempty"`
	Button    *Button      `json:"button,omitempty"`
	Channel   *Channel     `json:"channel,omitempty"`
	Emoji     *Emoji       `json:"emoji,omitempty"`
	Friend    *Friend      `json:"friend,omitempty"`
	Guild     *Guild       `json:"guild,omitempty"`
	Member    *GuildMember `json:"member,omitempty"`
	Message   *Message     `json:"message,omitempty"`
	Operator  *User        `json:"operator,omitempty"`
	Role      *GuildRole   `json:"role,omitempty"`
	User      *User        `json:"user,omitempty"`
	// Referrer 是用于被动请求的来源信息（实验性）。
	// 参见：https://satori.chat/zh-CN/advanced/passive.html
	Referrer *json.RawMessage `json:"referrer,omitempty"`

	// NativeType 是平台原生事件类型（实验性）。
	// 当 Type 为 "internal" 时，此字段包含平台原生事件类型字符串；
	// 当为标准事件时，此字段包含平台通用名称（如适配器所定义）。
	// 参见：https://satori.chat/zh-CN/advanced/internal.html#事件扩展
	NativeType string `json:"_type,omitempty"`

	// NativeData 是平台原生事件数据（实验性）。
	// 结构由具体平台/适配器定义，框架层透传不解析。
	// 参见：https://satori.chat/zh-CN/advanced/internal.html#事件扩展
	NativeData *json.RawMessage `json:"_data,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// ChannelType
// ─────────────────────────────────────────────────────────────────────────────

// ChannelType 是 Satori 协议定义的频道类型枚举。
type ChannelType int

const (
	ChannelTypeText     ChannelType = 0 // 文本频道
	ChannelTypeDirect   ChannelType = 1 // 私聊频道
	ChannelTypeCategory ChannelType = 2 // 分类频道
	ChannelTypeVoice    ChannelType = 3 // 语音频道
)

// ─────────────────────────────────────────────────────────────────────────────
// Channel
// ─────────────────────────────────────────────────────────────────────────────

// Channel 表示 Satori 频道资源。
type Channel struct {
	ID       string      `json:"id"`
	Type     ChannelType `json:"type"`
	Name     *string     `json:"name,omitempty"`
	ParentID *string     `json:"parent_id,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// User
// ─────────────────────────────────────────────────────────────────────────────

// User 表示 Satori 用户资源。
type User struct {
	ID     string  `json:"id"`
	Name   *string `json:"name,omitempty"`   // 用户名称
	Nick   *string `json:"nick,omitempty"`   // 用户昵称（优先级高于 Name）
	Avatar *string `json:"avatar,omitempty"` // 用户头像链接
	IsBot  *bool   `json:"is_bot,omitempty"` // 是否为机器人
}

// ─────────────────────────────────────────────────────────────────────────────
// Guild
// ─────────────────────────────────────────────────────────────────────────────

// Guild 表示 Satori 群组资源（群/服务器）。
type Guild struct {
	ID     string  `json:"id"`
	Name   *string `json:"name,omitempty"`
	Avatar *string `json:"avatar,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GuildMember
// ─────────────────────────────────────────────────────────────────────────────

// GuildMember 表示群组成员资源。
type GuildMember struct {
	User     *User   `json:"user,omitempty"`
	Nick     *string `json:"nick,omitempty"`
	Avatar   *string `json:"avatar,omitempty"`
	JoinedAt *int64  `json:"joined_at,omitempty"` // 入群时间戳（毫秒）
}

// ─────────────────────────────────────────────────────────────────────────────
// GuildRole
// ─────────────────────────────────────────────────────────────────────────────

// GuildRole 表示群组角色资源。
type GuildRole struct {
	ID   string  `json:"id"`
	Name *string `json:"name,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// LoginStatus
// ─────────────────────────────────────────────────────────────────────────────

// LoginStatus 是 Satori 协议定义的登录状态枚举。
type LoginStatus int

const (
	LoginStatusOffline    LoginStatus = 0 // 离线
	LoginStatusOnline     LoginStatus = 1 // 在线
	LoginStatusConnect    LoginStatus = 2 // 正在连接
	LoginStatusDisconnect LoginStatus = 3 // 正在断开连接
	LoginStatusReconnect  LoginStatus = 4 // 正在重新连接
)

// ─────────────────────────────────────────────────────────────────────────────
// Login
// ─────────────────────────────────────────────────────────────────────────────

// Login 表示 Satori 登录信息资源。
type Login struct {
	SN       *int64      `json:"sn,omitempty"`       // 序列号（实验性），仅用于标识 Login 对象
	Platform *string     `json:"platform,omitempty"` // 平台名称
	User     *User       `json:"user,omitempty"`     // 用户对象
	Status   LoginStatus `json:"status"`             // 登录状态
	Adapter  string      `json:"adapter,omitempty"`  // 适配器名称（实验性）
	Features []string    `json:"features,omitempty"` // 平台特性列表（实验性）
}

// ─────────────────────────────────────────────────────────────────────────────
// Message
// ─────────────────────────────────────────────────────────────────────────────

// Message 表示 Satori 消息资源。
type Message struct {
	ID        string       `json:"id"`
	Content   *string      `json:"content,omitempty"`    // 消息内容
	Channel   *Channel     `json:"channel,omitempty"`    // 频道对象
	Guild     *Guild       `json:"guild,omitempty"`      // 群组对象
	Member    *GuildMember `json:"member,omitempty"`     // 群组成员对象
	User      *User        `json:"user,omitempty"`       // 用户对象
	CreatedAt *int64       `json:"created_at,omitempty"` // 消息发送时间戳（毫秒）
	UpdatedAt *int64       `json:"updated_at,omitempty"` // 消息修改时间戳（毫秒）
}

// ─────────────────────────────────────────────────────────────────────────────
// Emoji
// ─────────────────────────────────────────────────────────────────────────────

// Emoji 表示 Satori 表情资源。
type Emoji struct {
	ID   string  `json:"id"`
	Name *string `json:"name,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Friend
// ─────────────────────────────────────────────────────────────────────────────

// Friend 表示 Satori 好友资源。
type Friend struct {
	User
}

// ─────────────────────────────────────────────────────────────────────────────
// Reaction
// ─────────────────────────────────────────────────────────────────────────────

// Reaction 表示 Satori 表态资源。
// 当前与 Emoji 字段结构相同，作为独立类型保留以备将来扩展。
type Reaction struct {
	Emoji *Emoji `json:"emoji,omitempty"`
	User  *User  `json:"user,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Interaction（Argv / Button）
// ─────────────────────────────────────────────────────────────────────────────

// Argv 表示交互指令的参数（实验性）。
type Argv struct {
	Name      string `json:"name"`
	Arguments []any  `json:"arguments,omitempty"` // 参数列表
	Options   any    `json:"options,omitempty"`   // 选项
}

// Button 表示交互按钮资源（实验性）。
// 注意：此处是入站的按钮事件资源，与消息编码中发出的按钮元素不同。
type Button struct {
	ID string `json:"id"`
}

// ─────────────────────────────────────────────────────────────────────────────
// 分页类型
// ─────────────────────────────────────────────────────────────────────────────

// List 是 Satori 列表 API 返回的标准分页列表。
type List[T any] struct {
	Data []T     `json:"data"`
	Next *string `json:"next,omitempty"` // 下一页的令牌
}

// BidiList 是少数 Satori API（如 message.list）返回的双向分页列表。
type BidiList[T any] struct {
	Data []T     `json:"data"`
	Prev *string `json:"prev,omitempty"` // 上一页的令牌
	Next *string `json:"next,omitempty"` // 下一页的令牌
}

// Direction 控制 BidiList 查询的方向。
type Direction string

const (
	DirectionBefore Direction = "before" // 向前查询
	DirectionAfter  Direction = "after"  // 向后查询
	DirectionAround Direction = "around" // 向两侧查询
)

// Order 控制 BidiList 查询的排序方式。
type Order string

const (
	OrderAsc  Order = "asc"  // 升序
	OrderDesc Order = "desc" // 降序
)

// ─────────────────────────────────────────────────────────────────────────────
// Meta（元信息，实验性）
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// 附件扩展元数据
// ─────────────────────────────────────────────────────────────────────────────

// MediaExtra 是 Satori 音频/视频/文件元素的扩展元数据，
// 存储于 platform.Attachment.Extra 字段。
//
// 解析 <audio>、<video>、<file> 元素时，若存在 duration 或 poster 属性则填充此结构。
// Attachment/Button 的 Extra map 中使用的键名常量。
const (
	// ExtraKeyMedia 媒体元素元数据键，值为 *MediaExtra。
	ExtraKeyMedia = "media"
	// ExtraKeyForwarded 转发消息元数据键，值为 *ForwardedMessage。
	ExtraKeyForwarded = "forwarded"
)

type MediaExtra struct {
	// Duration 媒体时长（秒），来自 duration 属性。
	Duration float64
	// Poster 封面/缩略图 URL，来自 poster 属性。
	Poster string
}

// ForwardedMessage 表示消息内容中的 <message> 转发元素。
// 存储于 platform.Attachment.Extra 字段。
//
// 参见：https://satori.chat/zh-CN/protocol/elements.html#消息-message
type ForwardedMessage struct {
	// ID 被转发的原始消息 ID（若有）。
	ID string
	// Forward 是否为转发消息（<message forward/>）。
	Forward bool
	// AuthorID 模拟作者的用户 ID（来自 <author id="..."/>）。
	AuthorID string
	// AuthorName 模拟作者的显示名称（来自 <author name="..."/>）。
	AuthorName string
	// AuthorAvatar 模拟作者的头像 URL（来自 <author avatar="..."/>）。
	AuthorAvatar string
	// Content 消息内容纯文本（递归解析子节点得到）。
	Content string
}

// Meta 是 Satori 元信息对象（实验性）。
//
// 通过 POST /{version}/meta 接口返回，包含所有登录信息和代理路由列表。
// 代理路由列表也会在 READY 和 META 信令中携带。
//
// 参见：https://satori.js.org/zh-CN/advanced/meta.html
type Meta struct {
	Logins    []*Login `json:"logins,omitempty"`     // 所有登录信息
	ProxyURLs []string `json:"proxy_urls,omitempty"` // 需代理的资源链接前缀列表
}
