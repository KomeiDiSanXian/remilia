package wechat

import (
	"bytes"
	stdctx "context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// maxContentBytes 是 markdown/content 的字节上限（官方 20480 字节）。
const maxContentBytes = 20480

// wechatSender 实现 platform.Sender 及若干可选接口。
//
// 支持的可选接口：
//   - platform.SessionNotifier （主动推送：长连接 aibot_send_msg）
//   - platform.APIProvider     （暴露 *wechatSender 访问上传/流式等平台能力）
//
// 刻意**不**实现 MessageEditor / MessageDeleter / ReactionSender /
// TypingIndicator：企业微信智能机器人没有对应的接口，声明了也发不出去。
type wechatSender struct {
	adapter    *Adapter
	httpClient *http.Client

	// streamMu 保护 streamIDs，用于把"同一次回复的多条流式片段"关联到
	// 同一个 stream.id。
	streamMu  sync.Mutex
	streamIDs map[string]string
}

// newSender 创建发送器。
func newSender(a *Adapter) *wechatSender {
	return &wechatSender{
		adapter:    a,
		httpClient: newHTTPClient(),
		streamIDs:  make(map[string]string),
	}
}

// PlatformAPI 实现 platform.APIProvider，返回发送器本身以访问
// UploadMedia / DownloadMedia / StreamReply 等平台专有能力。
func (s *wechatSender) PlatformAPI() any { return s }

// 编译期接口检查。
var (
	_ platform.Sender          = (*wechatSender)(nil)
	_ platform.SessionNotifier = (*wechatSender)(nil)
	_ platform.APIProvider     = (*wechatSender)(nil)
)

// Sender 是企业微信发送器的公开句柄类型。
//
// 通过 [platform.GetPlatformAPIAs] 拿到它，即可访问平台专有能力
// （素材上传、媒体下载解密、流式回复、模板卡片更新等）：
//
//	api, ok := platform.GetPlatformAPIAs[*wechat.Sender](ctx.GetPlatformSender())
//	if ok {
//	    mediaID, err := api.UploadMedia(ctx, wechat.MediaKindImage, "a.png", data)
//	}
//
// 平台无关的发送仍走 [platform.Sender]，两者互补。
type Sender = wechatSender

// ────────────────────────────────────────────────────────────────────────────
// platform.Sender
// ────────────────────────────────────────────────────────────────────────────

// Send 发送一条消息。
//
// 路由规则（按顺序）：
//   - ChatInfo.Tokens 有 response_url（webhook 模式回调）→ POST 该一次性地址
//   - 否则走长连接 aibot_respond_msg（需要 reply_req_id，透传回调的 req_id）
//   - 两者都没有 → 明确报错：企业微信只能回复当前回调，主动推送必须显式
//     调用 NotifyUser / NotifyGroup（它们走 aibot_send_msg）
//
// 内容形态按优先级：Buttons → 模板卡片；Markdown/Text → markdown 消息；
// 附件 → 逐条上传后发送（企业微信一条消息只能带一个媒体）。
func (s *wechatSender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if err := req.Validate(); err != nil {
		return platform.SendResult{}, err
	}

	chatID := req.Target.ID
	msg := req.Message

	// 有附件时先发媒体，再发正文：企业微信无法图文同条，
	// 先图后文更符合"先给结果、再给说明"的阅读习惯。
	if len(msg.Attachments) > 0 {
		res, err := s.sendAttachments(ctx, req)
		if err != nil {
			return res, err
		}
		if !hasTextContent(msg) && len(msg.Buttons) == 0 {
			return res, nil
		}
	}

	body, err := s.buildBody(ctx, msg)
	if err != nil {
		return platform.SendResult{}, err
	}
	if body == nil {
		// 只有附件且已发送完毕：此时 body 为空是正常路径。
		return platform.SendResult{Platform: PlatformID}, nil
	}
	return s.dispatch(ctx, req, body, chatID)
}

// sendAttachments 逐条发送附件。
func (s *wechatSender) sendAttachments(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	var last platform.SendResult
	for i, att := range req.Message.Attachments {
		data, kind, name, err := s.loadAttachment(ctx, att)
		if err != nil {
			return last, fmt.Errorf("wechat: 读取第 %d 个附件失败: %w", i, err)
		}
		mediaID, err := s.UploadMedia(ctx, kind, name, data)
		if err != nil {
			return last, err
		}
		body := mediaBody(kind, mediaID)
		res, err := s.dispatch(ctx, req, body, req.Target.ID)
		if err != nil {
			return res, err
		}
		last = res
	}
	return last, nil
}

// mediaBody 构造媒体消息体。
func mediaBody(kind MediaKind, mediaID string) *notifyBody {
	switch kind {
	case MediaKindImage:
		return &notifyBody{MsgType: "image", Image: &MediaIDBody{MediaID: mediaID}}
	case MediaKindVoice:
		return &notifyBody{MsgType: "voice", Voice: &MediaIDBody{MediaID: mediaID}}
	case MediaKindVideo:
		return &notifyBody{MsgType: "video", Video: &VideoBody{MediaID: mediaID}}
	default:
		return &notifyBody{MsgType: "file", File: &MediaIDBody{MediaID: mediaID}}
	}
}

// buildBody 把 OutboundMessage 转成企业微信消息体。
//
// 返回 nil 表示"没有可发送的正文"（仅附件场景）。
func (s *wechatSender) buildBody(_ stdctx.Context, msg platform.OutboundMessage) (*notifyBody, error) {
	extra := extractExtra(msg)
	text := contentOf(msg)

	if len(msg.Buttons) > 0 {
		card := buildTemplateCard(text, msg.Buttons, extra)
		return &notifyBody{MsgType: "template_card", TemplateCard: &card}, nil
	}
	if text == "" {
		return nil, nil
	}
	if len(text) > maxContentBytes {
		return nil, platform.NewSendError(platform.SendErrMsgTooLong, PlatformID, "",
			fmt.Sprintf("正文 %d 字节，超过 %d 字节上限", len(text), maxContentBytes), 0, nil)
	}
	return &notifyBody{MsgType: "markdown", Markdown: &MarkdownBody{Content: text}}, nil
}

// contentOf 取正文：优先 Markdown，其次 Text（企业微信两种都按 markdown 渲染）。
//
// Segments 存在时以段为准（唯一真相源），与其它适配器一致。
func contentOf(msg platform.OutboundMessage) string {
	if len(msg.Segments) > 0 {
		if text := strings.TrimSpace(platform.SegmentsContent(msg.Segments)); text != "" {
			return text
		}
	}
	if msg.Markdown != "" {
		return msg.Markdown
	}
	return msg.Text
}

// hasTextContent 报告消息是否带正文。
func hasTextContent(msg platform.OutboundMessage) bool { return contentOf(msg) != "" }

// buildTemplateCard 把平台无关的 Buttons 映射成按钮交互型模板卡片。
//
// 企业微信一条卡片最多 6 个按钮；超出的部分截断（不静默丢整张卡片）。
// 链接按钮（ButtonStyleLink / URL 非空）映射为 type=1 跳转按钮，
// 其余为 type=0 回调按钮，key 即 Button.ID。
func buildTemplateCard(text string, buttons []platform.Button, extra MessageExtra) templateCard {
	const maxButtons = 6

	title := extra.Title
	desc := extra.Description
	if title == "" {
		title = firstLine(text)
		if title == "" {
			title = "消息"
		}
	}
	if len(buttons) > maxButtons {
		logger.Warnf("[wechat.Sender] 模板卡片按钮 %d 个，超过平台上限 %d，已截断",
			len(buttons), maxButtons)
		buttons = buttons[:maxButtons]
	}

	list := make([]templateCardButton, 0, len(buttons))
	for _, btn := range buttons {
		b := templateCardButton{
			Text: btn.Label,
			Key:  btn.ID,
		}
		if b.Text == "" {
			b.Text = btn.ID
		}
		if btn.URL != "" || btn.Style == platform.ButtonStyleLink {
			b.Type = 1
			b.URL = btn.URL
			b.Key = ""
		}
		if style := extractButtonExtra(btn).Style; style > 0 {
			b.Style = style
		}
		list = append(list, b)
	}

	taskID := extra.TaskID
	if taskID == "" {
		taskID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}

	return templateCard{
		CardType:   "button_interaction",
		MainTitle:  templateCardTitle{Title: truncateRunes(title, 64), Desc: truncateRunes(desc, 160)},
		SubTitle:   truncateRunes(bodyAfterFirstLine(text), 512),
		ButtonList: list,
		TaskID:     taskID,
	}
}

// firstLine 取首行（用作卡片标题）。
func firstLine(text string) string {
	if before, _, ok := strings.Cut(text, "\n"); ok {
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(text)
}

// bodyAfterFirstLine 取首行之后的正文（卡片副标题）。
func bodyAfterFirstLine(text string) string {
	if _, after, ok := strings.Cut(text, "\n"); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// truncateRunes 按字符数截断（避免切坏多字节字符）。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// dispatch 按会话的授权信息选择回复通道。
func (s *wechatSender) dispatch(ctx stdctx.Context, req platform.SendRequest, body *notifyBody, chatID string) (platform.SendResult, error) {
	if url := req.Target.Tokens[TokenResponseURL]; url != "" {
		return s.replyByResponseURL(ctx, url, body, chatID)
	}
	// 事件里没有 response_url 时，回退到该会话最近一次回调的地址：
	// response_url 只能用一次，但"同一个 handler 先后发两条"是常见写法。
	if hook := s.adapter.webhook; hook != nil {
		if url := hook.responseURLFor(chatID); url != "" {
			res, err := s.replyByResponseURL(ctx, url, body, chatID)
			if err == nil {
				return res, nil
			}
			logger.WithError(err).Debug("[wechat.Sender] 复用 response_url 失败（可能已使用过）")
		}
	}
	if reqID := req.Target.Tokens[TokenReplyReqID]; reqID != "" {
		return s.replyByReqID(ctx, reqID, req.Target.Tokens[TokenMsgID], body, chatID)
	}
	return platform.SendResult{}, platform.NewSendError(platform.SendErrInvalidTarget, PlatformID, chatID,
		"缺少回复授权（无 response_url / reply_req_id）：企业微信只能回复当前回调，主动推送请用 NotifyUser/NotifyGroup", 0, nil)
}

// replyByReqID 通过长连接回执回复（aibot_respond_msg，透传回调的 req_id）。
func (s *wechatSender) replyByReqID(ctx stdctx.Context, reqID, messageID string, body *notifyBody, chatID string) (platform.SendResult, error) {
	client := s.adapter.currentClient()
	if client == nil {
		return platform.SendResult{}, platform.NewSendError(platform.SendErrNetworkError, PlatformID, chatID,
			"长连接未就绪", 0, nil)
	}
	// 回复带超时：长连接半死（TCP 活着但服务端不处理）时，无界的等待会把
	// handler 一起挂住，进而拖垮整条事件处理链路。
	replyCtx, cancel := stdctx.WithTimeout(ctx, s.adapter.cfg.RequestTimeout)
	defer cancel()
	if err := client.reply(replyCtx, reqID, cmdRespondMsg, body); err != nil {
		code := frameCode(err)
		if code == 0 {
			// 没有错误码说明是传输层问题（写帧失败/等回执超时），可重试。
			return platform.SendResult{}, platform.NewSendError(platform.SendErrNetworkError, PlatformID, chatID,
				"发送回复帧失败", 0, err)
		}
		return platform.SendResult{}, platform.NewSendError(sendErrCodeFor(code), PlatformID, chatID,
			"发送回复帧被拒绝", 0, err)
	} // 企业微信的回执不含消息 ID，用回调的 msgid 作为回复标识就够用了：
	// 调用方一般只拿它做日志追踪（本平台没有编辑/撤回）。
	return platform.SendResult{
		MessageID: messageID,
		Timestamp: time.Now(),
		Platform:  PlatformID,
		Raw:       body,
	}, nil
}

// replyByResponseURL 通过一次性 response_url 回复（webhook 模式）。
func (s *wechatSender) replyByResponseURL(ctx stdctx.Context, url string, body *notifyBody, chatID string) (platform.SendResult, error) {
	data, err := jsonx.Marshal(body)
	if err != nil {
		return platform.SendResult{}, fmt.Errorf("wechat: 序列化回复失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return platform.SendResult{}, fmt.Errorf("wechat: 构造回复请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(platform.SendErrNetworkError, PlatformID, chatID,
			"response_url 回复失败", 0, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return platform.SendResult{}, platform.NewSendError(platform.SendErrPlatform, PlatformID, chatID,
			fmt.Sprintf("response_url 返回 HTTP %d（每个 response_url 只能使用一次，且有效期 1 小时）",
				resp.StatusCode), 0, nil)
	}
	return platform.SendResult{Timestamp: time.Now(), Platform: PlatformID, Raw: body}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// platform.SessionNotifier（主动推送）
// ────────────────────────────────────────────────────────────────────────────

// NotifyUser 向指定用户主动推送消息（长连接 aibot_send_msg）。
//
// 前置条件：该用户此前给机器人发过消息。企业微信要求"会话已存在"，
// 否则推送会被拒绝。
func (s *wechatSender) NotifyUser(ctx stdctx.Context, userID string, msg platform.OutboundMessage) error {
	return s.notify(ctx, userID, ChatTypeUser, msg)
}

// NotifyGroup 向指定群聊主动推送消息。
//
// groupID 必须是回调里出现过的 chatid（群聊场景企业微信下发的会话 ID）。
func (s *wechatSender) NotifyGroup(ctx stdctx.Context, groupID string, msg platform.OutboundMessage) error {
	return s.notify(ctx, groupID, ChatTypeChat, msg)
}

// notify 执行 aibot_send_msg。
func (s *wechatSender) notify(ctx stdctx.Context, chatID string, chatType int, msg platform.OutboundMessage) error {
	client := s.adapter.currentClient()
	if client == nil {
		return platform.NewSendError(platform.SendErrNetworkError, PlatformID, chatID,
			"主动推送需要长连接（URL 回调模式没有主动推送接口）", 0, nil)
	}

	body, err := s.buildBody(ctx, msg)
	if err != nil {
		return err
	}
	if body == nil {
		return platform.NewSendError(platform.SendErrUnsupported, PlatformID, chatID,
			"主动推送不支持纯附件（需先 UploadMedia 再用 NotifyMedia）", 0, nil)
	}
	body.ChatID = chatID
	body.ChatType = chatType

	frame, err := client.request(ctx, cmdSendMsg, body)
	if err != nil {
		return platform.NewSendError(platform.SendErrNetworkError, PlatformID, chatID, "主动推送失败", 0, err)
	}
	if !frame.ok() {
		return platform.NewSendError(sendErrCodeFor(frame.errorCode()), PlatformID, chatID,
			fmt.Sprintf("主动推送被拒: errcode=%d errmsg=%s", frame.errorCode(), frame.ErrMsg), 0, nil)
	}
	return nil
}

// NotifyMedia 主动推送一条已上传的媒体（需先调用 UploadMedia 换 media_id）。
//
// chatType 取 [ChatTypeUser] 或 [ChatTypeChat]：企业微信要求显式声明 chatid
// 的解析方式，留空（[ChatTypeAuto]）会优先按群聊解析，单聊容易投错。
func (s *wechatSender) NotifyMedia(ctx stdctx.Context, chatID string, kind MediaKind, chatType int, mediaID string) error {
	client := s.adapter.currentClient()
	if client == nil {
		return errors.New("wechat: 主动推送需要长连接")
	}
	body := mediaBody(kind, mediaID)
	body.ChatID = chatID
	body.ChatType = chatType
	frame, err := client.request(ctx, cmdSendMsg, body)
	if err != nil {
		return err
	}
	if !frame.ok() {
		return fmt.Errorf("wechat: 媒体推送被拒: errcode=%d errmsg=%s", frame.errorCode(), frame.ErrMsg)
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// 流式回复（长连接专有）
// ────────────────────────────────────────────────────────────────────────────

// StreamReply 发送一段流式回复内容。
//
// 首次调用（同一个 req_id）自动分配 stream.id，后续片段必须复用同一个
// req_id 与 stream.id；finish=true 结束这条流式消息。企业微信在长连接
// 模式下不再回调刷新，必须由本端主动推。
func (s *wechatSender) StreamReply(ctx stdctx.Context, req platform.SendRequest, content string, finish bool) error {
	reqID := req.Target.Tokens[TokenReplyReqID]
	if reqID == "" {
		return errors.New("wechat: 流式回复需要长连接回调（缺少 reply_req_id）")
	}
	client := s.adapter.currentClient()
	if client == nil {
		return errors.New("wechat: 长连接未就绪")
	}

	streamID := s.streamID(reqID)
	frame := struct {
		MsgType string `json:"msgtype"`
		Stream  struct {
			ID      string `json:"id"`
			Finish  bool   `json:"finish,omitempty"`
			Content string `json:"content"`
		} `json:"stream"`
	}{MsgType: "stream"}
	frame.Stream.ID = streamID
	frame.Stream.Finish = finish
	frame.Stream.Content = content

	// 中间帧不等回执：逐帧等 RTT 会把流式输出拖成挤牙膏；最后一帧等，
	// 这样"结束帧被拒"能作为错误返回。
	if finish {
		if err := client.reply(ctx, reqID, cmdRespondMsg, frame); err != nil {
			return err
		}
	} else {
		if err := client.replyNoWait(reqID, cmdRespondMsg, frame); err != nil {
			return err
		}
	}
	if finish {
		s.streamMu.Lock()
		delete(s.streamIDs, reqID)
		s.streamMu.Unlock()
	}
	return nil
}

// streamID 返回（必要时创建）req_id 对应的 stream.id。
func (s *wechatSender) streamID(reqID string) string {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if id, ok := s.streamIDs[reqID]; ok {
		return id
	}
	id, err := newReqID("stream")
	if err != nil {
		// 退化为 req_id 派生值：stream.id 只要求"同一条消息内唯一"。
		id = "stream-" + reqID
	}
	s.streamIDs[reqID] = id
	return id
}

// ReplyWelcome 回复"进入会话"欢迎语（aibot_respond_welcome_msg）。
func (s *wechatSender) ReplyWelcome(ctx stdctx.Context, reqID, text string) error {
	client := s.adapter.currentClient()
	if client == nil {
		return errors.New("wechat: 长连接未就绪")
	}
	return client.reply(ctx, reqID, cmdRespondWelcome, &notifyBody{
		MsgType:  "markdown",
		Markdown: &MarkdownBody{Content: text},
	})
}

// UpdateTemplateCard 原地更新模板卡片（aibot_respond_update_msg）。
//
// 必须复用卡片回调帧里的 req_id，且只在收到 template_card_event 后 5 秒内有效。
func (s *wechatSender) UpdateTemplateCard(ctx stdctx.Context, reqID string, card templateCard) error {
	client := s.adapter.currentClient()
	if client == nil {
		return errors.New("wechat: 长连接未就绪")
	}
	return client.reply(ctx, reqID, cmdRespondUpdate, struct {
		ResponseType string       `json:"response_type"`
		TemplateCard templateCard `json:"template_card"`
	}{ResponseType: "update_template_card", TemplateCard: card})
}

// 企业微信的错误码（与发送/推送相关的常见项）。
const (
	errCodeInvalidCredential = 40001 // 凭证无效或不是当前身份
	errCodeInvalidToken      = 40014 // access_token 非法（长连接模式下少见）
	errCodeInvalidUserID     = 40003 // userid 不合法
	errCodeContentTooLong    = 45002 // 消息内容超过长度限制
	errCodeRateLimit         = 45009 // 接口调用超过限制
	errCodeFrequencyLimit    = 45047 // 超出企业微信群机器人发送频率
	errCodeUserNotInChat     = 86003 // 会话不存在 / 用户未与机器人产生过会话
	errCodeNotInChatScope    = 86001 // 会话不在机器人可用范围内
)

// sendErrCodeFor 把企业微信 errcode 映射到标准发送错误码。
//
// 映射的意义在于让上层能区分"退避重试有用"（频率限制）与"改配置才有用"
// （凭证/会话不存在）；未识别的错误码一律归为 SendErrPlatform。
func sendErrCodeFor(code int) platform.SendErrorCode {
	switch code {
	case errCodeRateLimit, errCodeFrequencyLimit:
		return platform.SendErrRateLimit
	case errCodeContentTooLong:
		return platform.SendErrMsgTooLong
	case errCodeInvalidCredential, errCodeInvalidToken:
		return platform.SendErrTokenExpired
	case errCodeInvalidUserID, errCodeUserNotInChat, errCodeNotInChatScope:
		return platform.SendErrInvalidTarget
	default:
		return platform.SendErrPlatform
	}
}
