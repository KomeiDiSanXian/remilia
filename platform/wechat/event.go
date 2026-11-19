package wechat

// event.go — 把企业微信智能机器人的回调报文包装成 platform.Event。
//
// 事件映射：
//
//	msgtype=text/image/voice/file/video/mixed → PRIVATE_MESSAGE（单聊）
//	                                          → GROUP_MESSAGE（群聊）
//	eventtype=enter_chat                      → NOTICE（rawType=enter_chat）
//	eventtype=template_card_event             → INTERACTION
//	eventtype=feedback_event                  → NOTICE（rawType=feedback_event）
//	eventtype=disconnected_event              → NOTICE（rawType=disconnected_event）
//
// 一条重要的平台语义：智能机器人在群里**只能收到 @ 它的消息**（官方文档：
// "用户群里@智能机器人或者单聊中向智能机器人发送文本消息"）。因此群消息与
// 单聊消息在语义上都是"直接发给机器人"，事件实现 [platform.DirectedAtBotEvent]
// 并返回 true，让 GetMentions 恒为空的平台也能命中 OnMentionedBot。

import (
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// wechatEvent 是 platform.Event 的企业微信实现。
type wechatEvent struct {
	kind       platform.EventKind
	rawType    string
	rawPayload any
	id         string
	sender     platform.UserInfo
	chat       platform.ChatInfo
	segments   []platform.Segment
	timestamp  time.Time
}

// ── platform.Event ──────────────────────────────────────────────────────────

func (e *wechatEvent) Platform() string             { return PlatformID }
func (e *wechatEvent) Kind() platform.EventKind     { return e.kind }
func (e *wechatEvent) ID() string                   { return e.id }
func (e *wechatEvent) Sender() platform.UserInfo    { return e.sender }
func (e *wechatEvent) Chat() platform.ChatInfo      { return e.chat }
func (e *wechatEvent) Timestamp() time.Time         { return e.timestamp }
func (e *wechatEvent) Segments() []platform.Segment { return e.segments }

// ── platform.RawEvent ───────────────────────────────────────────────────────

func (e *wechatEvent) RawType() string { return e.rawType }
func (e *wechatEvent) RawPayload() any { return e.rawPayload }

// ── platform.ReplyEvent ─────────────────────────────────────────────────────

func (e *wechatEvent) ReplyToID() string { return platform.SegmentsReplyToID(e.segments) }

// ── platform.DirectedAtBotEvent ─────────────────────────────────────────────

// DirectedAtBot 恒为 true：智能机器人只会收到"@机器人"或"单聊发给机器人"的消息。
func (e *wechatEvent) DirectedAtBot() bool { return true }

// 编译期接口检查。
var (
	_ platform.Event              = (*wechatEvent)(nil)
	_ platform.RawEvent           = (*wechatEvent)(nil)
	_ platform.ReplyEvent         = (*wechatEvent)(nil)
	_ platform.DirectedAtBotEvent = (*wechatEvent)(nil)
)

// newEvent 把回调报文转成 platform.Event；不支持的消息类型返回 nil。
//
// botID 用于把群消息开头的 @机器人 文本切成 at 段（否则正文会带上
// "@机器人 " 前缀，命令匹配会全部失配）。
func newEvent(cb *Callback, botID string) platform.Event {
	if cb == nil {
		return nil
	}

	e := &wechatEvent{
		id:         cb.MsgID,
		rawPayload: cb,
		rawType:    cb.MsgType,
		sender:     userFromCallback(cb.From),
		chat:       chatFromCallback(cb),
	}

	if cb.CreateTime > 0 {
		e.timestamp = time.Unix(cb.CreateTime, 0)
	}

	switch {
	case cb.MsgType == msgTypeEvent:
		return applyEventKind(e, cb)
	case cb.ChatType == chatTypeSingle:
		e.kind = platform.EventKindPrivateMessage
	case cb.ChatType == chatTypeGroup:
		e.kind = platform.EventKindGroupMessage
	default:
		// chattype 缺失（老报文）时按单聊处理：会话 ID 用 userid，
		// 至少能回得去；群聊缺 chatid 时回复必然失败。
		e.kind = platform.EventKindPrivateMessage
	}

	e.segments = buildSegments(cb, botID)
	if len(e.segments) == 0 && cb.MsgType != msgTypeEvent {
		return nil
	}
	return e
}

// applyEventKind 处理 msgtype=event 的回调。
func applyEventKind(e *wechatEvent, cb *Callback) platform.Event {
	if cb.Event == nil {
		return nil
	}
	e.rawType = cb.Event.EventType
	switch cb.Event.EventType {
	case eventTypeEnterChat:
		e.kind = platform.EventKindNotice
	case eventTypeTemplateCard:
		e.kind = platform.EventKindInteraction
		// 按钮 key 作为正文，插件可用 GetMessageContent 直接拿到，
		// 无需感知模板卡片结构。
		if cb.Event.EventKey != "" {
			e.segments = []platform.Segment{{Type: platform.SegmentText, Text: cb.Event.EventKey}}
		}
	case eventTypeFeedback:
		e.kind = platform.EventKindNotice
	case eventTypeDisconnected:
		// 新连接把旧连接踢掉时下发；适配器用它触发重连，不进业务 handler。
		e.kind = platform.EventKindNotice
	default:
		e.kind = platform.EventKindNotice
	}
	// 群里的卡片点击事件没有 chatid 之外的上下文，保留会话信息以便就地回卡片。
	e.chat = chatFromCallback(cb)
	return e
}

// buildSegments 把回调内容转成有序消息段。
func buildSegments(cb *Callback, botID string) []platform.Segment {
	var segs []platform.Segment

	if q := cb.Quote; q != nil {
		segs = append(segs, quoteSegment(q))
	}

	switch cb.MsgType {
	case msgTypeText:
		segs = append(segs, textSegments(derefText(cb.Text), cb.ChatType, botID)...)

	case msgTypeVoice:
		// 语音消息只有 ASR 转写文本：正文给出转写内容，
		// 插件无需再下载音频即可理解用户说了什么。
		text := ""
		if cb.Voice != nil {
			text = cb.Voice.Content
		}
		segs = append(segs, textSegments(text, cb.ChatType, botID)...)

	case msgTypeImage:
		if cb.Image != nil && cb.Image.URL != "" {
			segs = append(segs, mediaSegment(platform.SegmentImage, platform.AttachmentKindImage,
				cb.Image.URL, cb.Image.AesKey, "", 0))
		}

	case msgTypeVideo:
		if cb.Video != nil && cb.Video.URL != "" {
			segs = append(segs, mediaSegment(platform.SegmentVideo, platform.AttachmentKindVideo,
				cb.Video.URL, cb.Video.AesKey, "", 0))
		}

	case msgTypeFile:
		if cb.File != nil && cb.File.URL != "" {
			segs = append(segs, mediaSegment(platform.SegmentFile, platform.AttachmentKindFile,
				cb.File.URL, cb.File.AesKey, cb.File.Name, cb.File.Size))
		}

	case msgTypeMixed:
		if cb.Mixed != nil {
			for _, item := range cb.Mixed.MsgItem {
				switch item.MsgType {
				case msgTypeText:
					segs = append(segs, textSegments(derefText(item.Text), cb.ChatType, botID)...)
				case msgTypeImage:
					if item.Image != nil && item.Image.URL != "" {
						segs = append(segs, mediaSegment(platform.SegmentImage, platform.AttachmentKindImage,
							item.Image.URL, item.Image.AesKey, "", 0))
					}
				}
			}
		}
	}
	return segs
}

// derefText 安全解引用文本体。
func derefText(t *TextBody) string {
	if t == nil {
		return ""
	}
	return t.Content
}

// textSegments 把文本转成段，并在群聊里剥掉开头的 @机器人 前缀。
//
// 企业微信群消息的正文形如 "@RobotA hello robot"：@ 部分没有结构化字段，
// 只能从字符串里切。若原样保留，正文会以 "@RobotA " 开头，`/ping` 这类
// 命令匹配全部失配（框架不会把未知前缀当空白跳过）。
//
// 切分规则：仅当一个 '@' 开头、以空白结尾的 token 存在时切掉它，并把该
// token 记为 at 段（目标为机器人自身）。切不出 token（例如正文直接就是
// "hello"）时保持原样，避免误吃正常的 @ 文本。
func textSegments(content, chatType, botID string) []platform.Segment {
	if content == "" {
		return nil
	}
	var segs []platform.Segment
	if chatType == chatTypeGroup {
		if mention, rest, ok := splitLeadingMention(content); ok {
			segs = append(segs, platform.Segment{
				Type:   platform.SegmentAt,
				Text:   mention,
				UserID: botID,
				Extra:  map[string]any{platform.SegmentExtraIsSelf: true},
			})
			content = rest
		}
	}
	if content != "" {
		segs = append(segs, platform.Segment{Type: platform.SegmentText, Text: content})
	}
	return segs
}

// splitLeadingMention 切出形如 "@名字 " 的前缀。
//
// 返回 (mention, 剩余正文, 是否切出)。mention 保留 '@'，便于日志与展示；
// 剩余正文去掉紧随其后的空白。
func splitLeadingMention(content string) (mention, rest string, ok bool) {
	if !strings.HasPrefix(content, "@") {
		return "", content, false
	}
	idx := strings.IndexFunc(content, isSpaceRune)
	if idx <= 1 {
		// 没有空白（整条就是 "@xxx"）或只有 "@"：不切，交给下游原文处理。
		return "", content, false
	}
	return content[:idx], strings.TrimLeftFunc(content[idx:], isSpaceRune), true
}

// isSpaceRune 判断是否空白字符。
func isSpaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\u3000':
		return true
	}
	return false
}

// quoteSegment 把引用消息转成 reply 段。
//
// 被引用内容统一放在 Extra[SegmentExtraQuoteAtts]（与 Telegram/Discord 一致），
// 供识图等下游直接消费；ReplyToID 在企业微信里没有稳定 ID，留空。
func quoteSegment(q *QuoteBody) platform.Segment {
	seg := platform.Segment{Type: platform.SegmentReply}
	var atts []platform.Attachment
	switch q.MsgType {
	case msgTypeText:
		seg.Text = derefText(q.Text)
	case msgTypeImage:
		if q.Image != nil && q.Image.URL != "" {
			atts = append(atts, platform.Attachment{
				Kind:     platform.AttachmentKindImage,
				URL:      q.Image.URL,
				MimeType: "image/jpeg",
				Extra:    mediaExtra(q.Image.AesKey, ""),
			})
		}
	case msgTypeFile:
		if q.File != nil && q.File.URL != "" {
			atts = append(atts, platform.Attachment{
				Kind:  platform.AttachmentKindFile,
				URL:   q.File.URL,
				Name:  q.File.Name,
				Size:  q.File.Size,
				Extra: mediaExtra(q.File.AesKey, q.File.Name),
			})
		}
	case msgTypeVideo:
		if q.Video != nil && q.Video.URL != "" {
			atts = append(atts, platform.Attachment{
				Kind:  platform.AttachmentKindVideo,
				URL:   q.Video.URL,
				Extra: mediaExtra(q.Video.AesKey, ""),
			})
		}
	case msgTypeVoice:
		if q.Voice != nil {
			seg.Text = q.Voice.Content
		}
	case msgTypeMixed:
		if q.Mixed != nil {
			var texts []string
			for _, item := range q.Mixed.MsgItem {
				switch item.MsgType {
				case msgTypeText:
					texts = append(texts, derefText(item.Text))
				case msgTypeImage:
					if item.Image != nil && item.Image.URL != "" {
						atts = append(atts, platform.Attachment{
							Kind:     platform.AttachmentKindImage,
							URL:      item.Image.URL,
							MimeType: "image/jpeg",
							Extra:    mediaExtra(item.Image.AesKey, ""),
						})
					}
				}
			}
			seg.Text = strings.Join(texts, "\n")
		}
	}
	if len(atts) > 0 {
		seg.Extra = map[string]any{platform.SegmentExtraQuoteAtts: atts}
	}
	return seg
}

// mediaSegment 构造图片/视频/文件段。
func mediaSegment(segType platform.SegmentType, kind platform.AttachmentKind, url, aesKey, name string, size int) platform.Segment {
	mime := ""
	switch kind {
	case platform.AttachmentKindImage:
		mime = "image/jpeg"
	case platform.AttachmentKindVideo:
		mime = "video/mp4"
	}
	return platform.Segment{
		Type: segType,
		Attachment: platform.Attachment{
			Kind:     kind,
			URL:      url,
			Name:     name,
			Size:     size,
			MimeType: mime,
			Extra:    mediaExtra(aesKey, name),
		},
	}
}

// mediaExtra 生成附件扩展元数据。
//
// AesKey 必须随附件一起传出：媒体直链是加密的，下载方没有它拿到的就是密文。
// URL 本身 5 分钟后失效，因此这里只做"随事件交给下游"，不做持久化承诺。
func mediaExtra(aesKey, name string) map[string]any {
	if aesKey == "" && name == "" {
		return nil
	}
	extra := make(map[string]any, 2)
	if aesKey != "" {
		extra[ExtraKeyAesKey] = aesKey
	}
	if name != "" {
		extra[ExtraKeyFileName] = name
	}
	return extra
}

// userFromCallback 把回调的触发者转成 UserInfo。
func userFromCallback(from From) platform.UserInfo {
	return platform.UserInfo{ID: from.UserID}
}

// chatFromCallback 把回调的会话信息转成 ChatInfo。
//
// 群聊用 chatid 作为会话 ID；单聊没有 chatid，用 userid 代替（与企业微信
// 主动推送接口的约定一致：单聊 chatid 填 userid）。
func chatFromCallback(cb *Callback) platform.ChatInfo {
	chat := platform.ChatInfo{IsGroup: cb.ChatType == chatTypeGroup}
	chat.ID = cb.ChatID
	if chat.ID == "" {
		chat.ID = cb.From.UserID
	}
	return chat
}
