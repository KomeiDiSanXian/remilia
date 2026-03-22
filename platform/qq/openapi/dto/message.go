package dto

import (
	"encoding/json"
	"net/url"
)

// MessageType ...
type MessageType int

const (
	TextMessage MessageType = iota
	_
	MarkdownMessage
	ArkMessage
	EmbedMessage
	MediaMessage = 7
)

// FileType ...
type FileType int

const (
	ImageFile FileType = iota + 1
	VideoFile
	AudioFile
	File
)

// Message ...
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/send.html#%E5%8F%91%E9%80%81%E6%B6%88%E6%81%AF
type Message struct {
	Content  string          `json:"content,omitempty"`
	Type     MessageType     `json:"msg_type"`
	Markdown *Markdown       `json:"markdown,omitempty"`
	Keyboard json.RawMessage `json:"keyboard,omitempty"` // Keyboard 按钮对象（InlineKeyboard）
	Ark      *Ark            `json:"ark,omitempty"`
	Media    *MediaResponse  `json:"media,omitempty"`
	// MessageReference 消息引用（展示引用气泡），与 msg_id/event_id 独立，用于在消息中显示被引用消息。
	// 与 GuildMessage.MessageReference 含义相同，但适用于 QQ 单聊与群聊场景。
	MessageReference *MessageReference `json:"message_reference,omitempty"`
	EventID          EventID           `json:"event_id,omitempty"`
	MessageID        EventID           `json:"msg_id,omitempty"`
	MessageSeq       uint64            `json:"msg_seq,omitempty"`
	// IsWakeup 互动召回消息标志（2026/01/10 新增）
	// 与 msg_id / event_id 互斥使用；用户主动对话后每周期最多下发 1 条召回消息。
	// 仅适用于 QQ 单聊（C2C），群聊不支持此字段。
	IsWakeup bool `json:"is_wakeup,omitempty"`
}

// MarkdownParam Markdown 模版参数，{key, values} 键值对。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/type/markdown.html#数据结构与协议
type MarkdownParam struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// Markdown ...
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/type/markdown.html#%E6%95%B0%E6%8D%AE%E7%BB%93%E6%9E%84%E4%B8%8E%E5%8D%8F%E8%AE%AE
type Markdown struct {
	Content          string          `json:"content,omitempty"`
	CustomTemplateID string          `json:"custom_template_id,omitempty"`
	Params           []MarkdownParam `json:"params,omitempty"`
}

// Ark ...
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/type/ark.html#%E6%95%B0%E6%8D%AE%E7%BB%93%E6%9E%84%E4%B8%8E%E5%8D%8F%E8%AE%AE
type Ark struct {
	TemplateID int              `json:"template_id"`
	KV         []map[string]any `json:"kv"` // kv has two types: []map[string][]map[string]any and []map[string]any
}

// Media ...
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/rich-media.html#%E5%AF%8C%E5%AA%92%E4%BD%93%E6%B6%88%E6%81%AF
type Media struct {
	Type FileType `json:"file_type"`
	URL  string   `json:"url,omitempty"`
	// FileData base64 编码的二进制数据，与 URL 二选一
	FileData string `json:"file_data,omitempty"`
	// ActiveSend 已废弃 (2025-04-21)：主动推送能力已停用，设置 true 会返回错误。
	// 请始终保持 false，改用两步发送（先上传获取 file_info，再通过发消息接口发送）。
	// 官方公告：https://q.qq.com/miniapp#/news/detail/974e66a946a5e54c441ca983585a7aab
	ActiveSend bool `json:"srv_send_msg"`
}

// MediaResponse ...
type MediaResponse struct {
	FileUUID string `json:"file_uuid,omitempty"`
	FileInfo string `json:"file_info,omitempty"`
	TTL      int    `json:"ttl,omitempty"`
	ID       string `json:"id,omitempty"`
}

// At 用于 at 用户
//
// 需要嵌入文本消息中，例如：
//
//	"Hello " + At("openID") + ", welcome!"
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/text-chain.html#%E4%BD%BF%E7%94%A8-%E8%83%BD%E5%8A%9B
func At(openID string) string {
	return "<qqbot-at-user id=\"" + openID + "\" />"
}

// AtAll 用于 at 全体成员
//
// 需要嵌入文本消息中，例如：
//
//	"Hello " + AtAll() + ", welcome!"
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/text-chain.html#%E4%BD%BF%E7%94%A8-%E8%83%BD%E5%8A%9B
func AtAll() string {
	return "<qqbot-at-everyone />"
}

// CmdEnter 生成回车指令标签：点击后直接发送文本。
//
// 仅在 Markdown 消息中有效，不支持群聊和文字子频道。
// text 最大 100 字符，会自动进行 URL 编码。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/text-chain.html#指令操作
func CmdEnter(text string) string {
	return `<qqbot-cmd-enter text="` + url.QueryEscape(text) + `" />`
}

// CmdInput 生成参数指令标签：点击后将文本插入输入框。
//
// 仅在 Markdown 消息中有效。
//   - text：插入输入框的文本，最大 100 字符，会自动 URL 编码；
//   - show：消息中展示给用户的文本，为空时取 text 值；
//   - reference：是否携带消息原文引用一并插入输入框。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/text-chain.html#指令操作
func CmdInput(text, show string, reference bool) string {
	ref := "false"
	if reference {
		ref = "true"
	}
	if show == "" {
		show = text
	}
	return `<qqbot-cmd-input text="` + url.QueryEscape(text) + `" show="` + url.QueryEscape(show) + `" reference="` + ref + `" />`
}

// ChannelLink 生成跳转子频道标签（仅频道可用）。
//
// 点击后跳转至同频道内指定子频道，仅支持当前频道内的子频道。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/text-chain.html#跳转子频道
func ChannelLink(channelID string) string {
	return "<#" + channelID + ">"
}

// Emoji 生成系统表情内嵌标签（仅频道可用）。
//
// 仅支持 type=1 的系统表情；type=2 的 emoji 表情直接用字符串即可。
// 具体表情 ID 参考官方 Emoji 列表。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/text-chain.html#表情
func Emoji(id string) string {
	return "<emoji:" + id + ">"
}

// ────────────────────────────────────────────────────────────────────────────
// 频道（Guild Channel）消息结构体
// POST /channels/{channel_id}/messages
// ────────────────────────────────────────────────────────────────────────────

// GuildMessage 文字子频道消息请求体。
//
// 与群聊/单聊 Message 的主要区别：
//   - 无 msg_type 字段（格式由内容字段推断）
//   - 图片直接使用 image URL，无需先上传获取 file_info
//   - msg_id/event_id 均为普通 string（非 EventID 类型）
//   - content、embed、ark、image/markdown 至少需要一个字段
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/post_messages.html
type GuildMessage struct {
	// Content 文本内容，支持内嵌格式（@用户、表情等）
	Content string `json:"content,omitempty"`
	// Embed 嵌入消息（MessageEmbed，一种特殊的 Ark）
	Embed *GuildEmbed `json:"embed,omitempty"`
	// Ark Ark 模板消息
	Ark *Ark `json:"ark,omitempty"`
	// MessageReference 引用（转发）消息对象，展示被引用消息
	MessageReference *MessageReference `json:"message_reference,omitempty"`
	// Image 图片 URL，平台会转存；直接设置无需二次上传
	Image string `json:"image,omitempty"`
	// MsgID 触发该被动消息的来源消息 ID（Message.id），用于发送被动消息（回复）
	MsgID string `json:"msg_id,omitempty"`
	// EventID 触发该被动消息的事件 ID，与 MsgID 任填其一即可
	EventID string `json:"event_id,omitempty"`
	// Markdown Markdown 消息对象
	Markdown *Markdown `json:"markdown,omitempty"`
	// Keyboard 按钮对象（InlineKeyboard）
	Keyboard json.RawMessage `json:"keyboard,omitempty"`
}

// MessageReference 引用消息对象。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/template/model.html#messagereference
type MessageReference struct {
	// MessageID 被引用消息的 ID
	MessageID string `json:"message_id"`
	// IgnoreGetMessageError 是否忽略获取引用消息详情错误，默认 false
	IgnoreGetMessageError bool `json:"ignore_get_message_error"`
}

// GuildEmbed 频道嵌入消息（MessageEmbed）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/type/embed.html
type GuildEmbed struct {
	// Title 标题
	Title string `json:"title,omitempty"`
	// Prompt 提示文本（消息列表文案）
	Prompt string `json:"prompt,omitempty"`
	// Thumbnail 缩略图
	Thumbnail *GuildEmbedMedia `json:"thumbnail,omitempty"`
	// Fields 字段列表
	Fields []GuildEmbedField `json:"fields,omitempty"`
}

// GuildEmbedMedia Embed 中的媒体（缩略图/图片）
type GuildEmbedMedia struct {
	// URL 图片 URL
	URL string `json:"url,omitempty"`
}

// GuildEmbedField Embed 中的字段行
type GuildEmbedField struct {
	// Name 字段内容
	Name string `json:"name,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// 消息按钮（InlineKeyboard）
// 发送时序列化后赋值给 Message.Keyboard（json.RawMessage）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/msg-btn.html
// ────────────────────────────────────────────────────────────────────────────

// InlineKeyboard 按钮消息顶层结构。
//
// 两种使用方式：
//   - 模板按钮：填 ID（申请模板后获得），Content 留空
//   - 自定义按钮：填 Content（需内邀开通），ID 留空
type InlineKeyboard struct {
	// ID 申请的按钮模板 ID，与 Content 二选一
	ID string `json:"id,omitempty"`
	// Content 自定义按钮内容，与 ID 二选一
	Content *InlineKeyboardContent `json:"content,omitempty"`
}

// InlineKeyboardContent 自定义按钮内容。
type InlineKeyboardContent struct {
	// Rows 按钮行，最多 5 行，每行最多 5 个按钮
	Rows []KeyboardRow `json:"rows"`
}

// KeyboardRow 按钮行。
type KeyboardRow struct {
	// Buttons 该行中的按钮列表，最多 5 个
	Buttons []KeyboardButton `json:"buttons"`
}

// KeyboardButton 单个按钮对象。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/msg-btn.html#%E6%95%B0%E6%8D%AE%E7%BB%93%E6%9E%84%E4%B8%8E%E5%8D%8F%E8%AE%AE
type KeyboardButton struct {
	// ID 按钮 ID，在一个 keyboard 消息内设置唯一，可不填
	ID string `json:"id,omitempty"`
	// RenderData 按钮展示样式
	RenderData *KeyboardRenderData `json:"render_data"`
	// Action 按钮点击操作
	Action *KeyboardAction `json:"action"`
}

// KeyboardRenderData 按钮展示配置。
type KeyboardRenderData struct {
	// Label 按钮上的文字
	Label string `json:"label"`
	// VisitedLabel 点击后按钮上的文字
	VisitedLabel string `json:"visited_label"`
	// Style 按钮样式：0 灰色线框，1 蓝色线框
	Style int `json:"style"`
}

// KeyboardAction 按钮操作配置。
type KeyboardAction struct {
	// Type 操作类型：0=跳转按钮（http/小程序 scheme），1=回调按钮（通知后台），2=指令按钮（插入输入框）
	Type int `json:"type"`
	// Permission 按钮可操作权限
	Permission *KeyboardPermission `json:"permission"`
	// Data 操作相关数据，跳转时为 URL，回调时透传给后台，指令时为指令文本
	Data string `json:"data"`
	// UnsupportTips 客户端不支持本 action 时弹出的 toast 文案
	UnsupportTips string `json:"unsupport_tips"`
	// Reply 指令按钮专有：是否带引用回复本消息，默认 false（v8983+）
	Reply bool `json:"reply,omitempty"`
	// Enter 指令按钮专有：点击后直接自动发送 data，默认 false（v8983+）
	Enter bool `json:"enter,omitempty"`
	// Anchor 指令按钮专有：1=唤起手Q选图器（仅手机端 v8983+，桌面端不支持）
	Anchor int `json:"anchor,omitempty"`
	// ClickLimit 【已弃用】可操作点击的次数，默认不限。
	// 已弃用，保留字段仅供反序列化旧版按钮数据使用，发送时请勿使用。
	ClickLimit int `json:"click_limit,omitempty"`
	// AtBotShowChannelList 【已弃用】指令按钮专有：弹出子频道选择器，默认 false。
	// 已弃用，保留字段仅供反序列化旧版按钮数据使用，发送时请勿使用。
	AtBotShowChannelList bool `json:"at_bot_show_channel_list,omitempty"`
}

// KeyboardPermission 按钮操作权限配置。
type KeyboardPermission struct {
	// Type 权限类型：0=指定用户，1=仅管理者，2=所有人，3=指定身份组（仅频道）
	Type int `json:"type"`
	// SpecifyUserIDs 有权限的用户 openID 列表（Type=0 时使用）
	SpecifyUserIDs []string `json:"specify_user_ids,omitempty"`
	// SpecifyRoleIDs 有权限的身份组 ID 列表（Type=3，仅频道场景）
	SpecifyRoleIDs []string `json:"specify_role_ids,omitempty"`
}

// MarshalKeyboard 将 InlineKeyboard 序列化为 json.RawMessage，便于赋值给 Message.Keyboard。
//
// 示例：
//
//	kb := dto.MarshalKeyboard(&dto.InlineKeyboard{
//	    Content: &dto.InlineKeyboardContent{
//	        Rows: []dto.KeyboardRow{{Buttons: []dto.KeyboardButton{{...}}}},
//	    },
//	})
//	msg.Keyboard = kb
func MarshalKeyboard(kb *InlineKeyboard) json.RawMessage {
	data, _ := json.Marshal(kb)
	return data
}
