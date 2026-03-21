package dto

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
	Content    string         `json:"content,omitempty"`
	Type       MessageType    `json:"msg_type"`
	Markdown   *Markdown      `json:"markdown,omitempty"`
	Ark        *Ark           `json:"ark,omitempty"`
	Media      *MediaResponse `json:"media,omitempty"`
	EventID    EventID        `json:"event_id,omitempty"`
	MessageID  EventID        `json:"msg_id,omitempty"`
	MessageSeq uint64         `json:"msg_seq,omitempty"`
}

// Markdown ...
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/type/markdown.html#%E6%95%B0%E6%8D%AE%E7%BB%93%E6%9E%84%E4%B8%8E%E5%8D%8F%E8%AE%AE
type Markdown struct {
	Content          string           `json:"content"`
	CustomTemplateID string           `json:"custom_template_id"`
	Params           []map[string]any `json:"params"`
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
	Type       FileType `json:"file_type"`
	URL        string   `json:"url"`
	ActiveSend bool     `json:"srv_send_msg"`
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
