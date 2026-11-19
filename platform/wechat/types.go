package wechat

import "github.com/KomeiDiSanXian/remilia/infra/jsonx"

// types.go — 企业微信智能机器人的线格式（长连接帧 + 回调报文）。
//
// 只覆盖适配器需要读写的字段：回调报文的字段随 msgtype 变化，未列出的字段
// 一律忽略；需要完整报文时用 Callback 的 Raw 字段（见 event.go 的 RawPayload）。

// ────────────────────────────────────────────────────────────────────────────
// 长连接帧
// ────────────────────────────────────────────────────────────────────────────

// 开发者 → 企业微信的帧命令。
const (
	cmdSubscribe      = "aibot_subscribe"           // 认证订阅
	cmdPing           = "ping"                      // 心跳
	cmdRespondMsg     = "aibot_respond_msg"         // 回复消息（透传 req_id）
	cmdRespondWelcome = "aibot_respond_welcome_msg" // 回复欢迎语
	cmdRespondUpdate  = "aibot_respond_update_msg"  // 更新模板卡片
	cmdSendMsg        = "aibot_send_msg"            // 主动推送
	cmdUploadInit     = "aibot_upload_media_init"   // 分片上传：初始化
	cmdUploadChunk    = "aibot_upload_media_chunk"  // 分片上传：分片
	cmdUploadFinish   = "aibot_upload_media_finish" // 分片上传：完成
)

// 企业微信 → 开发者的帧命令。
const (
	cmdCallback      = "aibot_msg_callback"   // 消息推送
	cmdEventCallback = "aibot_event_callback" // 事件推送
)

// Frame 是长连接的基础帧结构：{cmd, headers, body, errcode, errmsg}。
//
// 同一个结构承载上行与下行：上行时 Body 是请求体，下行时 Body 是结果体，
// errcode/errmsg 表示该帧是否被接受。
type Frame struct {
	Cmd     string           `json:"cmd,omitempty"`
	Headers FrameHeaders     `json:"headers"`
	Body    jsonx.RawMessage `json:"body,omitempty"`
	ErrCode *int             `json:"errcode,omitempty"`
	ErrMsg  string           `json:"errmsg,omitempty"`
}

// FrameHeaders 是帧头。
//
// req_id 是请求与回执的关联键：下行回调帧里的 req_id 在回复时必须原样透传，
// 因此它同时承担"回复授权"的角色。
type FrameHeaders struct {
	ReqID  string `json:"req_id,omitempty"`
	ChatID string `json:"chat_id,omitempty"`
	MsgID  string `json:"msg_id,omitempty"`
}

// ok 报告帧是否被企业微信接受。
//
// errcode 缺失（nil）视为成功：部分回执帧不带该字段。
func (f *Frame) ok() bool { return f.ErrCode == nil || *f.ErrCode == 0 }

// errorCode 返回错误码，缺失时为 0。
func (f *Frame) errorCode() int {
	if f.ErrCode == nil {
		return 0
	}
	return *f.ErrCode
}

// ────────────────────────────────────────────────────────────────────────────
// 回调报文
// ────────────────────────────────────────────────────────────────────────────

// 回调消息类型（[Callback.MsgType]）。
const (
	msgTypeText   = "text"
	msgTypeImage  = "image"
	msgTypeVoice  = "voice"
	msgTypeFile   = "file"
	msgTypeVideo  = "video"
	msgTypeMixed  = "mixed"
	msgTypeEvent  = "event"
	msgTypeStream = "stream"
)

// 会话类型（[Callback.ChatType]）。
const (
	chatTypeSingle = "single"
	chatTypeGroup  = "group"
)

// 事件类型（[Callback.Event.EventType]）。
const (
	eventTypeEnterChat    = "enter_chat"
	eventTypeTemplateCard = "template_card_event"
	eventTypeFeedback     = "feedback_event"
	eventTypeDisconnected = "disconnected_event"
)

// Callback 是解密后的回调报文。
//
// 长连接把它放在 frame.body 里，URL 回调把它放在 encrypt 解密后的 JSON 里，
// 两种传输共用同一份结构。
type Callback struct {
	MsgID       string `json:"msgid"`
	AIBotID     string `json:"aibotid"`
	CreateTime  int64  `json:"create_time"`
	ChatID      string `json:"chatid"`
	ChatType    string `json:"chattype"`
	From        From   `json:"from"`
	ResponseURL string `json:"response_url"`
	MsgType     string `json:"msgtype"`

	Text  *TextBody  `json:"text,omitempty"`
	Image *MediaBody `json:"image,omitempty"`
	Voice *VoiceBody `json:"voice,omitempty"`
	File  *FileBody  `json:"file,omitempty"`
	Video *MediaBody `json:"video,omitempty"`
	Mixed *MixedBody `json:"mixed,omitempty"`
	Event *EventBody `json:"event,omitempty"`
	Quote *QuoteBody `json:"quote,omitempty"`

	// CompatInfo 非空表示本次回调不是普通用户消息
	// （智能助理调用 source=ai_assistant、频道 source=channel）。
	CompatInfo *CompatInfo `json:"compat_info,omitempty"`
}

// From 是回调的触发者。
type From struct {
	UserID string `json:"userid"`
	CorpID string `json:"corpid,omitempty"`
}

// TextBody 文本消息体。
type TextBody struct {
	Content string `json:"content"`
}

// MediaBody 图片/视频消息体。
//
// URL 只有 5 分钟有效期，AesKey 每条链接唯一；仅长连接模式下发 AesKey。
type MediaBody struct {
	URL    string `json:"url"`
	AesKey string `json:"aeskey,omitempty"`
	MD5    string `json:"md5,omitempty"`
}

// VoiceBody 语音消息体。Content 是平台侧免费的 ASR 转写文本。
type VoiceBody struct {
	Content string `json:"content"`
	MediaID string `json:"media_id,omitempty"`
}

// FileBody 文件消息体。
type FileBody struct {
	URL    string `json:"url"`
	AesKey string `json:"aeskey,omitempty"`
	MD5    string `json:"md5,omitempty"`
	Name   string `json:"name,omitempty"`
	Size   int    `json:"size,omitempty"`
}

// MixedBody 图文混排消息体。
type MixedBody struct {
	MsgItem []MixedItem `json:"msg_item"`
}

// MixedItem 混排消息中的单个子项，类型由 MsgType 决定。
type MixedItem struct {
	MsgType string     `json:"msgtype"`
	Text    *TextBody  `json:"text,omitempty"`
	Image   *MediaBody `json:"image,omitempty"`
}

// EventBody 事件回调体。
type EventBody struct {
	EventType string `json:"eventtype"`
	EventKey  string `json:"event_key,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	MsgID     string `json:"msg_id,omitempty"`
	Rating    int    `json:"rating,omitempty"`
}

// QuoteBody 引用的被引用消息摘要。
type QuoteBody struct {
	MsgType string     `json:"msgtype"`
	Text    *TextBody  `json:"text,omitempty"`
	Image   *MediaBody `json:"image,omitempty"`
	Voice   *VoiceBody `json:"voice,omitempty"`
	File    *FileBody  `json:"file,omitempty"`
	Video   *MediaBody `json:"video,omitempty"`
	Mixed   *MixedBody `json:"mixed,omitempty"`
}

// CompatInfo 是回调的来源附加信息。
type CompatInfo struct {
	Source string `json:"source"`
}

// ────────────────────────────────────────────────────────────────────────────
// 上行请求体
// ────────────────────────────────────────────────────────────────────────────

// subscribeBody 是 aibot_subscribe 的请求体。
type subscribeBody struct {
	BotID  string `json:"bot_id"`
	Secret string `json:"secret"`
}

// notifyBody 是 aibot_send_msg 的请求体。
//
// chatid 单聊填 userid、群聊填 chatid；ChatType 显式声明解析方式，
// 留空由服务端"优先当群聊"猜测（官方建议总是填）。
//
// 同一个结构体也用于 response_url 的回复体（那条通道不带 chatid/chat_type，
// 两个字段留零值即可被 omitempty 省略）。
type notifyBody struct {
	ChatID   string `json:"chatid,omitempty"`
	ChatType int    `json:"chat_type,omitempty"`
	MsgType  string `json:"msgtype"`

	Markdown     *MarkdownBody `json:"markdown,omitempty"`
	TemplateCard *templateCard `json:"template_card,omitempty"`
	Image        *MediaIDBody  `json:"image,omitempty"`
	File         *MediaIDBody  `json:"file,omitempty"`
	Voice        *MediaIDBody  `json:"voice,omitempty"`
	Video        *VideoBody    `json:"video,omitempty"`
}

// 主动推送的会话类型（[wechat.NotifyMedia] 的 chatType 参数）。
const (
	// ChatTypeAuto 由服务端推断（优先按群聊解析，官方不建议依赖）。
	ChatTypeAuto = 0
	// ChatTypeUser chatid 是用户 userid（单聊）。
	ChatTypeUser = 1
	// ChatTypeChat chatid 是群聊 chatid。
	ChatTypeChat = 2
)

// MarkdownBody markdown 消息体。
type MarkdownBody struct {
	Content string `json:"content"`
}

// MediaIDBody 使用已上传素材的媒体消息体。
type MediaIDBody struct {
	MediaID string `json:"media_id"`
}

// VideoBody 视频消息体（可带标题与描述）。
type VideoBody struct {
	MediaID     string `json:"media_id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// uploadInitBody 是 aibot_upload_media_init 的请求体。
type uploadInitBody struct {
	Type        string `json:"type"`
	Filename    string `json:"filename"`
	TotalSize   int    `json:"total_size"`
	TotalChunks int    `json:"total_chunks"`
	MD5         string `json:"md5,omitempty"`
}

// uploadChunkBody 是 aibot_upload_media_chunk 的请求体。
type uploadChunkBody struct {
	UploadID   string `json:"upload_id"`
	ChunkIndex int    `json:"chunk_index"`
	Base64Data string `json:"base64_data"`
}

// uploadFinishBody 是 aibot_upload_media_finish 的请求体。
type uploadFinishBody struct {
	UploadID string `json:"upload_id"`
}

// uploadInitResult 是初始化上传的结果。
type uploadInitResult struct {
	UploadID string `json:"upload_id"`
}

// uploadFinishResult 是完成上传的结果。
type uploadFinishResult struct {
	Type      string `json:"type"`
	MediaID   string `json:"media_id"`
	CreatedAt string `json:"created_at"`
}

// ────────────────────────────────────────────────────────────────────────────
// 模板卡片
// ────────────────────────────────────────────────────────────────────────────

// templateCard 是按钮交互型模板卡片（card_type=button_interaction）。
//
// 企业微信的卡片类型与字段很多，这里只落地"把平台无关的 Buttons 映射成
// 可点击按钮"这一条路径：按钮 key 即 Button.ID，点击后触发
// template_card_event 回调并带回该 key。
type templateCard struct {
	CardType   string               `json:"card_type"`
	Source     *templateCardSource  `json:"source,omitempty"`
	MainTitle  templateCardTitle    `json:"main_title"`
	SubTitle   string               `json:"sub_title_text,omitempty"`
	ButtonList []templateCardButton `json:"button_list"`
	TaskID     string               `json:"task_id"`
}

// templateCardSource 是卡片左上角来源信息。
type templateCardSource struct {
	Desc string `json:"desc,omitempty"`
}

// templateCardTitle 是卡片主标题。
type templateCardTitle struct {
	Title string `json:"title"`
	Desc  string `json:"desc,omitempty"`
}

// templateCardButton 是卡片按钮。Type=0 表示点击产生回调事件。
type templateCardButton struct {
	Type  int    `json:"type,omitempty"`
	Text  string `json:"text"`
	Style int    `json:"style,omitempty"`
	Key   string `json:"key,omitempty"`
	URL   string `json:"url,omitempty"`
}

// MediaKind 是上传素材的类型（企业微信 upload 接口的 type 字段）。
type MediaKind string

const (
	// MediaKindFile 普通文件，≤20MB。
	MediaKindFile MediaKind = "file"
	// MediaKindImage 图片（png/jpg/jpeg/gif），≤10MB。
	MediaKindImage MediaKind = "image"
	// MediaKindVoice 语音（amr），≤2MB。
	MediaKindVoice MediaKind = "voice"
	// MediaKindVideo 视频（mp4），≤10MB。
	MediaKindVideo MediaKind = "video"
)
