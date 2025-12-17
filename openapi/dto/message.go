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
