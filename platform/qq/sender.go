package qq

import (
	"bytes"
	stdctx "context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/httpclient"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

const (
	// passiveReplyTTL 是被动回复授权的最长有效期（QQ 平台限制）。
	//
	// 官方限制（2026-08 文档核实）：
	//   - 群聊：被动消息有效 5 分钟，每个消息最多回复 5 次
	//   - C2C：被动消息有效 60 分钟，每个消息最多回复 4 次
	//
	// 官方 SDK（botgo）与官方 OpenClaw 插件均**不在客户端拦截**被动回复
	// 次数/时长——限制完全由平台端校验（错误码 40034128"被动回复时间或
	// 次数超限"）。实测平台限制已放宽（agent 场景远超文档值），客户端
	// 自加拦截会误伤正常的多次回复，因此本 sender 不再做发送前拦截。
	//
	// 此常量仅用作 msgSeqMap 的内存清理基准（sweepExpired）：
	// 取两种场景中最长的 C2C 有效期（60 分钟），确保条目在其仍可用的
	// 窗口内不被提前回收——若被提前回收，seq 会重置并可能撞上平台
	// "相同 msg_id+msg_seq 重复发送会失败"的去重规则。
	passiveReplyTTL = 60 * time.Minute
	// msgSeqSweepInterval 是 msgSeqMap 过期条目清理的最小间隔。
	msgSeqSweepInterval = time.Minute
)

// msgSeqEntry 按 msg_id 跟踪被动回复的 msg_seq 状态。
//
// 仅用于防重放：对同一 msg_id 递增 seq，避免相同 (msg_id, msg_seq)
// 组合触发平台去重导致消息被吞。不记录回复次数——次数限制由平台端
// 校验（错误码 40034128），客户端不拦截（官方 SDK/插件同策略）。
type msgSeqEntry struct {
	seq       atomic.Uint64
	createdAt atomic.Value // time.Time
}

// qqSender 将 platform.Sender 接口桥接到 openapi.OpenAPI
type qqSender struct {
	api       openapi.OpenAPI
	msgSeqMap sync.Map     // map[string]*msgSeqEntry，按 msg_id 管理回复状态
	lastSweep atomic.Int64 // 上次清理 msgSeqMap 的 UnixNano 时间戳
}

// NewSender 创建 QQ 平台的消息发送器
func NewSender(api openapi.OpenAPI) platform.Sender {
	return &qqSender{api: api}
}

// PlatformAPI 实现 platform.APIProvider，返回 QQ 开放平台 OpenAPI 客户端，
// 调用方可断言 openapi.OpenAPI 访问全部 QQ 能力（频道管理、富媒体、互动等）。
func (s *qqSender) PlatformAPI() any { return s.api }

// 编译期接口实现检查。
var _ platform.APIProvider = (*qqSender)(nil)

// NotifyUser 向指定用户发送私聊消息，实现 platform.SessionNotifier。
// QQ 开放平台 API 本身支持主动消息，无需事件上下文。
func (s *qqSender) NotifyUser(ctx stdctx.Context, userID string, msg platform.OutboundMessage) error {
	_, err := s.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: userID, IsGroup: false},
		Message: msg,
	})
	return err
}

// NotifyGroup 向指定群组发送消息，实现 platform.SessionNotifier。
func (s *qqSender) NotifyGroup(ctx stdctx.Context, groupID string, msg platform.OutboundMessage) error {
	_, err := s.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: groupID, IsGroup: true},
		Message: msg,
	})
	return err
}

// 编译期接口实现检查。
var _ platform.SessionNotifier = (*qqSender)(nil)

// Send 将 OutboundMessage 转换并发送到 QQ 平台，返回平台响应摘要。
//
// 路由规则：
//   - ChatInfo.ParentID 非空 → 频道消息（子频道 / 频道私信）
//   - Attachments 非空（取第一个）→ 两步富媒体发送（上传 → 发送）
//   - 其余 → Text / Markdown 文本消息
//
// SendResult.Raw 类型为 *SendResult，包含完整的 QQ 平台响应字段。
// 富媒体两步发送时，上传阶段（FileInfo、FileUUID、TTL）与发送阶段（MessageID）
// 均合并在同一个 *SendResult 中返回。
func (s *qqSender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if s.api == nil {
		return platform.SendResult{}, fmt.Errorf("qq sender: openAPI client is nil")
	}

	chat := req.Target
	if chat.ID == "" {
		return platform.SendResult{}, errutil.ErrNoChatInfo
	}

	msg := req.Message

	// 出站段优先路径：段 → 便捷字段等价物（at 内联标签保序，按钮混排受限）
	if len(msg.Segments) > 0 {
		msg = qqSegmentsToFlat(msg)
	}

	// 被动回复不做客户端次数/时长拦截：QQ 平台限制由服务端校验
	// （错误码 40034128"被动回复时间或次数超限"），实测限制已放宽，
	// 且官方 SDK（botgo）/ OpenClaw 插件均无客户端限制。
	// 这里只保留 msg_seq 递增（nextMsgSeq 在 buildDTOMessage 内），
	// 防止相同 msg_id+msg_seq 组合触发平台去重导致消息被吞。

	// 频道消息（ChatInfo.ParentID 非空）使用频道专属 API
	if chat.ParentID != "" {
		return s.sendGuildChannelMessage(ctx, chat, msg)
	}

	// 富媒体优先（QQ 不支持多附件，取第一个）
	if len(msg.Attachments) > 0 {
		return s.sendAttachment(ctx, chat, msg, msg.Attachments[0])
	}

	dtoMsg := s.buildDTOMessage(msg, chat)
	var (
		raw gjson.Result
		err error
	)
	if chat.IsGroup {
		raw, err = s.api.GroupChat(ctx, chat.ID, dtoMsg)
	} else {
		raw, err = s.api.SingleChat(ctx, chat.ID, dtoMsg)
	}
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, "qq", chat.ID,
			err.Error(), 0, err,
		)
	}
	return buildSendResult(raw), nil
}

// sendAttachment 实现 QQ 富媒体两步发送：先上传获取 file_info，再发送 MediaMessage。
// 返回的 SendResult.Raw(*SendResult) 同时包含上传响应和发送响应的字段。
//
// 上传策略：
//   - URL 附件：直接走 /files 接口（平台自动下载转存）
//   - 小文件 Data 附件：base64 file_data 直传 /files
//   - 大文件 Data 附件（> chunkedUploadThreshold）：分片上传
//     （upload_prepare → 分片 PUT → upload_part_finish → 合并），对齐官方 100MB 大文件能力
func (s *qqSender) sendAttachment(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage, att platform.Attachment) (platform.SendResult, error) {
	if att.URL == "" && len(att.Data) == 0 {
		return platform.SendResult{}, fmt.Errorf("qq sender: attachment has neither URL nor data")
	}

	// 大文件走分片上传（本地 Data 场景；URL 由平台侧转存，无需分片）
	if len(att.Data) > chunkedUploadThreshold {
		return s.sendAttachmentChunked(ctx, chat, msg, att)
	}

	media := &dto.Media{
		Type:       attachmentKindToFileType(att.Kind),
		ActiveSend: false,
	}
	if len(att.Data) > 0 {
		media.FileData = base64.StdEncoding.EncodeToString(att.Data)
	} else {
		media.URL = att.URL
	}

	var (
		uploadResult gjson.Result
		err          error
	)
	if chat.IsGroup {
		uploadResult, err = s.api.GroupRichMedia(ctx, chat.ID, media)
	} else {
		uploadResult, err = s.api.SingleRichMedia(ctx, chat.ID, media)
	}
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrNetworkError, "qq", chat.ID,
			fmt.Sprintf("media upload failed: %v", err), 0, err,
		)
	}

	fileInfo := uploadResult.Get("file_info").String()
	if fileInfo == "" {
		return platform.SendResult{}, fmt.Errorf("qq sender: media upload returned empty file_info (response: %s)", uploadResult.Raw)
	}

	return s.sendMediaMessage(ctx, chat, msg, fileInfo, uploadResult)
}

// chunkedUploadThreshold 触发分片上传的附件大小阈值（字节）。
//
// QQ 官方 /files 直传对大文件有限制（图片/语音软限制 20MB，硬限制 200MB），
// 且 base64 file_data 直传会让 JSON 请求体膨胀 4/3 倍。超过该阈值时
// 走官方推荐的分片上传流程（upload_prepare → 分片 PUT → upload_part_finish），
// 对齐官方 OpenClaw 插件的 100MB 大文件能力。
const chunkedUploadThreshold = 5 * 1024 * 1024 // 5MB

// sendAttachmentChunked 实现 QQ 大文件分片上传（四步流程）：
//
//  1. upload_prepare：传入文件信息 → 获取 upload_id + block_size + 各分片预签名 URL
//  2. 按 block_size 将文件切片，逐片 HTTP PUT 到对应的预签名 URL
//  3. 每片 PUT 成功后调用 upload_part_finish 通知服务端
//  4. 全部分片完成后调用上传接口（携带 upload_id）合并 → 返回 file_info
//
// 之后复用 sendMediaMessage 发送富媒体消息。
func (s *qqSender) sendAttachmentChunked(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage, att platform.Attachment) (platform.SendResult, error) {
	fileType := attachmentKindToFileType(att.Kind)
	fileMD5 := md5Sum(att.Data)
	prepareReq := &dto.UploadPrepareRequest{
		FileType: fileType,
		FileSize: int64(len(att.Data)),
		FileName: att.Name,
		FileMD5:  fileMD5,
		MD510M:   md5Sum(firstBytes(att.Data, 10002432)),
	}

	var (
		prepare gjson.Result
		err     error
	)
	if chat.IsGroup {
		prepare, err = s.api.GroupUploadPrepare(ctx, chat.ID, prepareReq)
	} else {
		prepare, err = s.api.UserUploadPrepare(ctx, chat.ID, prepareReq)
	}
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrNetworkError, "qq", chat.ID,
			fmt.Sprintf("chunked upload prepare failed: %v", err), 0, err,
		)
	}

	uploadID := prepare.Get("upload_id").String()
	blockSize := int(prepare.Get("block_size").Int())
	if blockSize <= 0 {
		return platform.SendResult{}, fmt.Errorf("qq sender: chunked upload prepare returned invalid block_size (response: %s)", prepare.Raw)
	}
	presignedURLs := prepare.Get("presigned_urls").Array()
	if len(presignedURLs) == 0 {
		return platform.SendResult{}, fmt.Errorf("qq sender: chunked upload prepare returned no presigned urls (response: %s)", prepare.Raw)
	}

	// 2+3. 分片 PUT + 完成确认
	data := att.Data
	for i, pu := range presignedURLs {
		start := i * blockSize
		if start >= len(data) {
			break
		}
		end := min(start+blockSize, len(data))
		chunk := data[start:end]

		if err := putPresignedChunk(ctx, pu.String(), chunk); err != nil {
			return platform.SendResult{}, platform.NewSendError(
				platform.SendErrNetworkError, "qq", chat.ID,
				fmt.Sprintf("chunked upload PUT part %d failed: %v", i, err), 0, err,
			)
		}

		finishReq := &dto.UploadPartFinishRequest{UploadID: uploadID, PartIndex: i}
		if chat.IsGroup {
			_, err = s.api.GroupUploadPartFinish(ctx, chat.ID, finishReq)
		} else {
			_, err = s.api.UserUploadPartFinish(ctx, chat.ID, finishReq)
		}
		if err != nil {
			return platform.SendResult{}, platform.NewSendError(
				platform.SendErrNetworkError, "qq", chat.ID,
				fmt.Sprintf("chunked upload part_finish %d failed: %v", i, err), 0, err,
			)
		}
	}

	// 4. 合并：携带 upload_id 调用上传接口 → file_info
	merge := &dto.Media{Type: fileType, UploadID: uploadID, ActiveSend: false}
	var uploadResult gjson.Result
	if chat.IsGroup {
		uploadResult, err = s.api.GroupRichMedia(ctx, chat.ID, merge)
	} else {
		uploadResult, err = s.api.SingleRichMedia(ctx, chat.ID, merge)
	}
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrNetworkError, "qq", chat.ID,
			fmt.Sprintf("chunked upload merge failed: %v", err), 0, err,
		)
	}
	fileInfo := uploadResult.Get("file_info").String()
	if fileInfo == "" {
		return platform.SendResult{}, fmt.Errorf("qq sender: chunked upload merge returned empty file_info (response: %s)", uploadResult.Raw)
	}

	return s.sendMediaMessage(ctx, chat, msg, fileInfo, uploadResult)
}

// sendMediaMessage 构建携带 file_info 的 MediaMessage 并发送，返回合并的上传/发送响应。
func (s *qqSender) sendMediaMessage(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage, fileInfo string, uploadResult gjson.Result) (platform.SendResult, error) {
	// 构建携带 file_info 的 MediaMessage
	dtoMsg := s.buildDTOMessage(msg, chat)
	dtoMsg.Type = dto.MediaMessage
	dtoMsg.Media = &dto.MediaResponse{FileInfo: fileInfo}

	// 富媒体消息不走 markdown 渲染：若 buildDTOMessage 因为带按钮而把正文
	// 迁移进了 Markdown，这里必须搬回 Content 并清掉 Markdown。
	// 否则正文会连同 markdown 载荷一起丢失（下面的空值兜底会把它替换成一个
	// 空格），用户只收到一张没有任何说明文字的图片。
	if dtoMsg.Markdown != nil {
		if dtoMsg.Content == "" {
			dtoMsg.Content = dtoMsg.Markdown.Content
		}
		dtoMsg.Markdown = nil
	}
	// 富媒体消息不支持按钮，去掉以免服务端拒绝整条消息。
	dtoMsg.Keyboard = nil

	// 群聊接口 content 字段为必填（API 文档标注"是"），不可清空。
	// 单聊接口 content 为可选，媒体消息通常不携带文本内容。
	if !chat.IsGroup {
		dtoMsg.Content = ""
	} else if dtoMsg.Content == "" {
		dtoMsg.Content = " "
	}

	var sendResult gjson.Result
	var err error
	if chat.IsGroup {
		sendResult, err = s.api.GroupChat(ctx, chat.ID, dtoMsg)
	} else {
		sendResult, err = s.api.SingleChat(ctx, chat.ID, dtoMsg)
	}
	if err != nil {
		return platform.SendResult{}, err
	}

	// 合并上传响应与发送响应
	return buildSendResultFromUpload(uploadResult, sendResult), nil
}

// putPresignedChunk 将单个分片 PUT 到预签名 URL（cos 直传，无需鉴权头）。
func putPresignedChunk(ctx stdctx.Context, presignedURL string, chunk []byte) error {
	resp, err := httpclient.Put(presignedURL).
		SetContext(ctx).
		SetTimeout(chunkPutTimeout).
		SetBody(bytes.NewReader(chunk)).
		Do()
	if err != nil {
		return err
	}
	defer resp.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := resp.Bytes()
		return fmt.Errorf("PUT presigned url status %d: %s", resp.StatusCode, truncateForLog(body))
	}
	return nil
}

// chunkPutTimeout 单分片 PUT 的超时（cos 直传 5MB 分片通常秒级完成）。
const chunkPutTimeout = 60 * time.Second

// md5Sum 计算数据的十六进制 MD5。
func md5Sum(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

// firstBytes 返回数据前 n 字节的副本（不足则全量）。
func firstBytes(data []byte, n int) []byte {
	if len(data) <= n {
		return data
	}
	return data[:n]
}

// truncateForLog 截断响应体用于错误日志（避免把整个响应刷进日志）。
func truncateForLog(b []byte) string {
	const max = 200
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}

// sendGuildChannelMessage 向 QQ 文字子频道或频道私信发送消息。
func (s *qqSender) sendGuildChannelMessage(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage) (platform.SendResult, error) {
	guildMsg := s.buildGuildDTOMessage(msg, chat)
	var (
		raw gjson.Result
		err error
	)
	if chat.IsDM {
		raw, err = s.api.DMChat(ctx, chat.ID, guildMsg)
	} else {
		raw, err = s.api.ChannelChat(ctx, chat.ID, guildMsg)
	}
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, "qq", chat.ID,
			err.Error(), 0, err,
		)
	}
	return buildSendResult(raw), nil
}

// buildSendResult 从普通发送响应构建 platform.SendResult。
func buildSendResult(raw gjson.Result) platform.SendResult {
	qqResult := &SendResult{
		MessageID: raw.Get("id").String(),
	}
	if ts := raw.Get("timestamp").Int(); ts > 0 {
		qqResult.Timestamp = time.Unix(ts, 0)
	}
	return platform.SendResult{
		MessageID: qqResult.MessageID,
		Timestamp: qqResult.Timestamp,
		Platform:  "qq",
		Raw:       qqResult,
	}
}

// buildSendResultFromUpload 合并富媒体上传响应与消息发送响应，构建 platform.SendResult。
func buildSendResultFromUpload(uploadRaw, sendRaw gjson.Result) platform.SendResult {
	qqResult := &SendResult{
		// 来自发送响应
		MessageID: sendRaw.Get("id").String(),
		// 来自上传响应
		FileUUID: uploadRaw.Get("file_uuid").String(),
		FileInfo: uploadRaw.Get("file_info").String(),
		TTL:      int(uploadRaw.Get("ttl").Int()),
	}
	if ts := sendRaw.Get("timestamp").Int(); ts > 0 {
		qqResult.Timestamp = time.Unix(ts, 0)
	}
	return platform.SendResult{
		MessageID: qqResult.MessageID,
		Timestamp: qqResult.Timestamp,
		Platform:  "qq",
		Raw:       qqResult,
	}
}

// buildGuildDTOMessage 将 platform.OutboundMessage 转换为频道专属的 dto.GuildMessage。
//
// 优先级：Ark > Markdown > Text(Content) > Image(Attachment)
// 被动消息：MsgID 优先使用 MessageExtra.EventID，其次使用 req.EventID。
// 引用回复：ReplyToID 非空时设置 MessageReference（展示被引用消息气泡）。
func (s *qqSender) buildGuildDTOMessage(msg platform.OutboundMessage, chat platform.ChatInfo) *dto.GuildMessage {
	guildMsg := &dto.GuildMessage{}
	extra := extractExtra(msg)

	// 消息类型优先级：Ark > Markdown > Text
	if extra.Ark != nil {
		guildMsg.Ark = convertArk(extra.Ark)
	} else if msg.Markdown != "" || extra.MarkdownTemplateID != "" {
		guildMsg.Markdown = &dto.Markdown{Content: msg.Markdown, CustomTemplateID: extra.MarkdownTemplateID, Params: extra.MarkdownParams}
	} else {
		guildMsg.Content = msg.Text
	}

	// @用户：将 Mentions 转为 QQ AT 内嵌标签，前置于正文
	if len(msg.Mentions) > 0 && extra.Ark == nil {
		var sb strings.Builder
		for _, uid := range msg.Mentions {
			sb.WriteString(dto.At(uid))
		}
		if guildMsg.Markdown != nil {
			guildMsg.Markdown.Content = sb.String() + guildMsg.Markdown.Content
		} else {
			guildMsg.Content = sb.String() + guildMsg.Content
		}
	}

	// 图片附件：频道 API 直接接受图片 URL，无需预先上传
	// 非图片类型（音视频/文件）在频道消息中不支持，忽略
	if len(msg.Attachments) > 0 {
		att := msg.Attachments[0]
		if att.Kind == platform.AttachmentKindImage && att.URL != "" {
			guildMsg.Image = att.URL
		}
	}

	// 交互按钮：转换为 InlineKeyboard
	if len(msg.Buttons) > 0 {
		guildMsg.Keyboard = dto.MarshalKeyboard(convertButtons(msg.Buttons))
	}

	// 被动消息关联：频道用 msg_id（来源消息的 Message.id / payload.ID）
	// 优先使用手动 ApplyExtra 注入的值（extra.EventID），其次 ChatInfo.Tokens[TokenMsgID]
	resolvedMsgID := extra.EventID
	if resolvedMsgID == "" {
		resolvedMsgID = chat.Tokens[TokenMsgID] // 频道消息：payload.ID 即 message id
	}
	if resolvedMsgID != "" {
		guildMsg.MsgID = resolvedMsgID
	}

	// 引用回复：展示消息气泡引用（不同于被动回复关联）
	if msg.ReplyToID != "" {
		guildMsg.MessageReference = &dto.MessageReference{
			MessageID:             msg.ReplyToID,
			IgnoreGetMessageError: true,
		}
	}

	return guildMsg
}

// attachmentKindToFileType 将平台无关的附件类型映射到 QQ dto.FileType
func attachmentKindToFileType(kind platform.AttachmentKind) dto.FileType {
	switch kind {
	case platform.AttachmentKindImage:
		return dto.ImageFile
	case platform.AttachmentKindVideo:
		return dto.VideoFile
	case platform.AttachmentKindAudio:
		return dto.AudioFile
	default:
		return dto.File
	}
}

// buildDTOMessage 将 platform.OutboundMessage 转换为 dto.Message（用于 C2C / 群聊）。
//
// 被动回复 ID 优先级：
//   - msg_id：msg.ReplyToID（手动设置）> chat.Tokens[TokenMsgID]（C2C_MESSAGE_CREATE / GROUP_AT_MESSAGE_CREATE 自动填充）
//   - event_id：extra.EventID（手动 ApplyExtra）> chat.Tokens[TokenEventID]（INTERACTION_CREATE / C2C_MSG_RECEIVE 等自动填充）
//
// 主动消息：ChatInfo.Tokens 中无相应 token 时，不设置 msg_id / event_id，即为主动消息。
func (s *qqSender) buildDTOMessage(msg platform.OutboundMessage, chat platform.ChatInfo) *dto.Message {
	dtoMsg := &dto.Message{}
	extra := extractExtra(msg)

	// 消息类型优先级：InputNotify > Ark > Card > Markdown > Text
	if extra.InputNotify != nil {
		dtoMsg.Type = dto.InputNotifyMsg
		dtoMsg.InputNotify = extra.InputNotify
	} else if extra.Ark != nil {
		dtoMsg.Type = dto.ArkMessage
		dtoMsg.Ark = convertArk(extra.Ark)
	} else if extra.Card != nil {
		dtoMsg.Type = dto.CardMessage
		dtoMsg.Card = extra.Card
	} else if msg.Markdown != "" || extra.MarkdownTemplateID != "" {
		dtoMsg.Type = dto.MarkdownMessage
		dtoMsg.Markdown = &dto.Markdown{Content: msg.Markdown, CustomTemplateID: extra.MarkdownTemplateID, Params: extra.MarkdownParams}
	} else {
		dtoMsg.Type = dto.TextMessage
		dtoMsg.Content = msg.Text
	}

	// 处理 Mentions（@ 用户）：将用户 ID 列表转换为 QQ AT 标签，前置于正文
	if len(msg.Mentions) > 0 && extra.Ark == nil {
		var sb strings.Builder
		for _, uid := range msg.Mentions {
			sb.WriteString(dto.At(uid))
		}
		if dtoMsg.Type == dto.MarkdownMessage && dtoMsg.Markdown != nil {
			dtoMsg.Markdown.Content = sb.String() + dtoMsg.Markdown.Content
		} else {
			dtoMsg.Content = sb.String() + dtoMsg.Content
		}
	}

	// 交互按钮：转换为 InlineKeyboard。
	//
	// QQ 的群/C2C 发送接口按 msg_type 决定渲染方式，keyboard 必须挂在
	// markdown 类型的消息上。此前这里只塞了 keyboard 而不改 msg_type，
	// 消息仍以 msg_type=0（纯文本）发出，QQ 直接忽略按钮：
	// 用户只看到一段没有任何按钮的文本，且**不会收到任何错误**，
	// 而 Capabilities().Buttons 却声明为 true。
	if len(msg.Buttons) > 0 && extra.Ark == nil {
		dtoMsg.Keyboard = dto.MarshalKeyboard(convertButtons(msg.Buttons))
		if dtoMsg.Type != dto.MarkdownMessage {
			// 把已有的纯文本正文迁移到 markdown 载荷里，再切换消息类型。
			content := dtoMsg.Content
			if content == "" {
				// 纯按钮消息（无正文）是合法的，见 OutboundMessage.IsEmpty 的说明。
				// 但空的 markdown 载荷会序列化成 "markdown":{} 被服务端拒绝，
				// 因此补一个占位空格。
				content = " "
			}
			dtoMsg.Type = dto.MarkdownMessage
			dtoMsg.Markdown = &dto.Markdown{Content: content}
			dtoMsg.Content = ""
		}
	}

	// 回复消息 ID（msg_id）：被动回复授权 token（message-based）
	// 优先级：msg.ReplyToID（手动设置）> chat.Tokens[TokenMsgID]（框架从 C2C/Group 消息事件自动填充）
	resolvedMsgID := msg.ReplyToID
	if resolvedMsgID == "" {
		resolvedMsgID = chat.Tokens[TokenMsgID]
	}
	if resolvedMsgID != "" {
		dtoMsg.MessageID = dto.EventID(resolvedMsgID)
	}

	if extra.MsgSeq != 0 {
		dtoMsg.MessageSeq = extra.MsgSeq
	} else {
		// 按 msg_id 递增序列号，避免相同 msg_id 重复发送
		// 空 msg_id 时设为 0 表示不设置此字段
		dtoMsg.MessageSeq = s.nextMsgSeq(string(dtoMsg.MessageID))
	}

	// event_id：被动回复授权 token（event-based，仅 INTERACTION_CREATE / C2C_MSG_RECEIVE 等）
	// 优先级：extra.EventID（手动 ApplyExtra）> chat.Tokens[TokenEventID]（框架从事件类型自动填充）
	resolvedEventID := extra.EventID
	if resolvedEventID == "" {
		resolvedEventID = chat.Tokens[TokenEventID]
	}
	if resolvedEventID != "" {
		dtoMsg.EventID = dto.EventID(resolvedEventID)
	}

	// IsWakeup：互动召回消息，与 event_id/msg_id 互斥，仅在 extra 中显式开启时有效。
	// 注意：is_wakeup 字段仅 QQ 单聊（C2C）接口支持，群聊接口不存在此字段。
	if extra.IsWakeup && !chat.IsGroup {
		dtoMsg.IsWakeup = true
		// 召回消息不关联来源事件/消息，清除已设置的 ID 字段
		dtoMsg.EventID = ""
		dtoMsg.MessageID = ""
	}

	return dtoMsg
}

// qqSegmentsToFlat 将统一出站段折叠为 QQ 便捷字段等价物（段路径、混排受限）。
//
// QQ 的文本接口支持内联 AT 标签（<qqbot-at-user id="..."/>），因此文本/at
// 可以保序交错进 Content；媒体取首个（QQ 单媒体限制，富媒体不可混排文本）；
// reply 段 → ReplyToID（复用既有"引用即被动回复 msg_id / 频道 MessageReference"逻辑）；
// 按钮不参与段路径（QQ 按钮不可与正文混排，降级处理）。
func qqSegmentsToFlat(msg platform.OutboundMessage) platform.OutboundMessage {
	segs := msg.Segments
	var sb strings.Builder
	for _, s := range segs {
		switch s.Type {
		case platform.SegmentText:
			sb.WriteString(s.Text)
		case platform.SegmentAt:
			sb.WriteString(dto.At(s.UserID))
		case platform.SegmentMentionAll:
			sb.WriteString(dto.AtAll())
		}
	}
	msg.Segments = nil
	msg.Text = sb.String()
	msg.ReplyToID = platform.SegmentsReplyToID(segs)
	msg.Attachments = platform.SegmentsAttachments(segs)
	msg.Mentions = nil
	msg.Buttons = nil
	msg.Markdown = ""
	return msg
}

// nextMsgSeq 返回指定 msg_id 的下一个消息序列号。
//
// msg_seq 是 QQ v2 API 防重放字段，与 msg_id 联合使用：
// 相同的 msg_id + msg_seq 重复发送会失败。
// 不填默认是 1，框架按 msg_id 分别递增。
// msgID 为空时（主动消息），返回 0 表示不设置 msg_seq。
func (s *qqSender) nextMsgSeq(msgID string) uint64 {
	if msgID == "" {
		return 0
	}
	s.sweepExpired()
	v, _ := s.msgSeqMap.LoadOrStore(msgID, &msgSeqEntry{})
	entry := v.(*msgSeqEntry)
	if entry.createdAt.Load() == nil {
		entry.createdAt.Store(time.Now())
	}
	return entry.seq.Add(1)
}

// sweepExpired 回收 msgSeqMap 中早已失效的条目。
//
// 正常路径（每条消息只回复一次）下条目一旦写入就再无人访问，此前没有任何
// 回收机制：一个 10 QPS 的机器人每天新增约 86 万条永不释放的条目，运行数天
// 即耗尽内存。msg_id 超过 passiveReplyTTL 后对 QQ 已不可用，条目纯属死重。
//
// 采用惰性清理而非后台 goroutine：qqSender 由 NewSender 构造且没有 Stop
// 钩子，后台 goroutine 无处停止，反而会造成新的泄漏。
func (s *qqSender) sweepExpired() {
	now := time.Now()
	last := s.lastSweep.Load()
	if now.UnixNano()-last < int64(msgSeqSweepInterval) {
		return
	}
	// CAS 保证并发调用下只有一个 goroutine 真正执行清理。
	if !s.lastSweep.CompareAndSwap(last, now.UnixNano()) {
		return
	}
	// 留出一个 TTL 的余量，避免与仍在判定过期的调用竞争。
	deadline := now.Add(-2 * passiveReplyTTL)
	s.msgSeqMap.Range(func(key, value any) bool {
		entry, ok := value.(*msgSeqEntry)
		if !ok {
			s.msgSeqMap.Delete(key)
			return true
		}
		created, ok := entry.createdAt.Load().(time.Time)
		if ok && created.Before(deadline) {
			s.msgSeqMap.Delete(key)
		}
		return true
	})
}

// Delete 撤回/删除消息。
//
// chatID 为目标会话 ID（openid / group_openid / channel_id）。
// 实现优先级：群聊 > 单聊 > 频道，以适应不同的 chatID 类型。
func (s *qqSender) Delete(ctx stdctx.Context, chatID, messageID string) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	var err error
	_, err = s.api.GroupReset(ctx, chatID, messageID)
	if err == nil {
		return nil
	}
	_, err = s.api.SingleReset(ctx, chatID, messageID)
	if err == nil {
		return nil
	}
	_, err = s.api.ChannelReset(ctx, chatID, messageID, true)
	return err
}

// AddReaction 为频道消息添加表情表态。
//
// 仅频道（Guild）消息支持，C2C 和群聊不支持。
// emoji.Kind 映射规则：
//   - EmojiKindSystem → emojiType=1, emojiID=emoji.ID（QQ 系统表情）
//   - EmojiKindUnicode → emojiType=2, emojiID=emoji.Value
//   - EmojiKindCustom → emojiType=2, emojiID=emoji.ID
func (s *qqSender) AddReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	emojiType, emojiID := resolveQQEmoji(emoji)
	_, err := s.api.AddReaction(ctx, chatID, messageID, emojiType, emojiID)
	return err
}

// RemoveReaction 移除频道消息的表情表态。
func (s *qqSender) RemoveReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	emojiType, emojiID := resolveQQEmoji(emoji)
	_, err := s.api.DeleteReaction(ctx, chatID, messageID, emojiType, emojiID)
	return err
}

// resolveQQEmoji 将 platform.Emoji 转换为 QQ 表态 API 的 emojiType 和 emojiID。
func resolveQQEmoji(emoji platform.Emoji) (emojiType int, emojiID string) {
	switch emoji.Kind {
	case platform.EmojiKindSystem:
		return 1, emoji.ID
	case platform.EmojiKindCustom:
		if emoji.ID != "" {
			return 2, emoji.ID
		}
		return 2, emoji.Value
	default: // EmojiKindUnicode
		return 2, emoji.Value
	}
}

// qqCapabilities 返回 QQ 平台的能力声明。
//
// 使用函数而非包级变量，保留运行时动态更新的能力（如连接后更新权限）。
func qqCapabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        true,
		Buttons:         true,
		MultiAttachment: false,
		MessageEdit:     false,
		MessageDelete:   true,
		Embeds:          false,
		FileUpload:      true,
		GuildSupport:    true,
		Reactions:       true,
		ThreadReply:     true,
		TypingIndicator: true,
		MentionAll:      true,
		VoiceChannel:    false,
		Forward:         true, // 合并转发（msg_type=3 富媒体外，msg_elements 引用内可含 forward）
		// QQ 按钮布局限制：最多 5 行，每行最多 5 个
		MaxButtonsPerRow: 5,
		MaxButtonRows:    5,
	}
}

// convertButtons 将平台无关的 []platform.Button 转换为 QQ InlineKeyboard。
//
// 行分组规则：
//   - Button.Row > 0：Row 值相同的按钮分入同一行；
//   - Button.Row == 0：每个按钮独占一行（安全默认值）。
//
// 最多 5 行，每行最多 5 个按钮（超出部分截断）。
// 按钮样式映射：ButtonStylePrimary → style=1（蓝色），其余 → style=0（灰色）。
// 按钮操作映射：
//   - ButtonStyleLink + URL → type=0（跳转）
//   - Button.Command 非空 → type=2（指令按钮：点击后在输入框插入 @bot <Command>）
//   - 其余 → type=1（回调按钮：data 为按钮 ID）
func convertButtons(buttons []platform.Button) *dto.InlineKeyboard {
	const maxRows, maxPerRow = 5, 5

	keyOrder := make([]int, 0, len(buttons))
	rowMap := make(map[int][]platform.Button)
	autoKey := 0 // 从 -1 递减，给 Row=0 的按钮分配唯一 key

	for _, b := range buttons {
		if b.Row == 0 {
			autoKey--
			rowMap[autoKey] = []platform.Button{b}
			keyOrder = append(keyOrder, autoKey)
		} else {
			if _, exists := rowMap[b.Row]; !exists {
				keyOrder = append(keyOrder, b.Row)
			}
			rowMap[b.Row] = append(rowMap[b.Row], b)
		}
	}

	rows := make([]dto.KeyboardRow, 0, min(len(keyOrder), maxRows))
	for _, key := range keyOrder {
		if len(rows) >= maxRows {
			break
		}
		btns := rowMap[key]
		if len(btns) > maxPerRow {
			btns = btns[:maxPerRow]
		}
		kbBtns := make([]dto.KeyboardButton, 0, len(btns))
		for _, b := range btns {
			style := 0
			if b.Style == platform.ButtonStylePrimary {
				style = 1
			}
			actionType := 1 // 默认回调
			data := b.ID
			if b.Style == platform.ButtonStyleLink && b.URL != "" {
				actionType = 0 // 跳转
				data = b.URL
			}
			if b.Command != "" {
				// 指令按钮：点击后在输入框插入 @bot <Command>，不产生互动回调
				actionType = 2
				data = b.Command
			}
			action := &dto.KeyboardAction{
				Type: actionType,
				Permission: &dto.KeyboardPermission{
					Type: 2, // 所有人可操作
				},
				Data:          data,
				UnsupportTips: "当前版本不支持此操作",
			}
			if ext, ok := b.Extra[ExtraKeyButton].(*ButtonExtra); ok {
				action.Enter = ext.Enter
				action.Reply = ext.Reply
				action.Anchor = ext.Anchor
			}
			kbBtns = append(kbBtns, dto.KeyboardButton{
				ID: b.ID,
				RenderData: &dto.KeyboardRenderData{
					Label:        b.Label,
					VisitedLabel: b.Label,
					Style:        style,
				},
				Action: action,
			})
		}
		rows = append(rows, dto.KeyboardRow{Buttons: kbBtns})
	}

	return &dto.InlineKeyboard{
		Content: &dto.InlineKeyboardContent{Rows: rows},
	}
}

// SendTyping 实现 platform.TypingNotifier，向 C2C 单聊发送"正在输入"状态。
//
// QQ 的输入中状态（msg_type=6 input_notify）**仅支持 C2C 单聊**，群聊不支持
// （QQ 会拒绝）。因此群聊场景返回 [platform.ErrNotSupported]。
//
// 判定依据：输入中状态接口只能打到 C2C 单聊端点；但 TypingNotifier 只提供
// chatID，无法区分群/单聊。为安全起见，仅在发送失败时静默降级：
// 输入中状态是尽力而为的提示，失败不影响后续真实消息。
func (s *qqSender) SendTyping(ctx stdctx.Context, chatID string) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	if chatID == "" {
		return errutil.ErrNoChatInfo
	}
	// input_notify 仅 C2C 单聊端点支持；群聊调用会返回平台错误。
	// 输入中状态是尽力而为的提示，失败时静默降级，不阻塞调用方。
	_, err := s.api.SingleChat(ctx, chatID, &dto.Message{
		Type:        dto.InputNotifyMsg,
		InputNotify: &dto.InputNotify{InputType: 1, InputSecond: 30},
	})
	if err != nil {
		logger.Debugf("[qq sender] SendTyping failed (typing indicator is best-effort): %v", err)
	}
	return nil
}

var (
	_ platform.Sender            = (*qqSender)(nil)
	_ platform.MessageDeleter    = (*qqSender)(nil)
	_ platform.ReactionSender    = (*qqSender)(nil)
	_ platform.GroupManager      = (*qqSender)(nil)
	_ platform.InvitationHandler = (*qqSender)(nil)
	_ platform.GroupInfoProvider = (*qqSender)(nil)
	_ platform.TypingNotifier    = (*qqSender)(nil)
)

// ────────────────────────────────────────────────────────────────────────────
// platform.GroupManager（群成员管理，2026-08 新增能力）
// ────────────────────────────────────────────────────────────────────────────

// BanMember 通过设置群成员禁言实现 platform.GroupManager。
//
// duration <= 0 时解除禁言（op=del）；否则设置禁言到期时间（op=add，
// mute_expire_at = now + duration）。
// 注意：QQ 群禁言只能操作普通成员，不能操作群主、管理员、机器人。
func (s *qqSender) BanMember(ctx stdctx.Context, groupID, userID string, duration time.Duration) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	state := dto.SetMemberMuteState{
		MemberOpenID: userID,
	}
	if duration <= 0 {
		state.Op = "del"
	} else {
		state.Op = "add"
		state.MuteExpireAt = time.Now().Add(duration).Format(time.RFC3339)
	}
	_, err := s.api.SetGroupMemberMute(ctx, groupID, &dto.SetRestrictChatSettingRequest{
		Members: []dto.SetMemberMuteState{state},
	})
	return err
}

// KickMember QQ 官方 v2 群聊暂无踢人接口，返回 ErrNotSupported。
func (s *qqSender) KickMember(_ stdctx.Context, _, _ string, _ bool) error {
	return platform.ErrNotSupported
}

// SetAdmin QQ 官方 v2 群聊暂无设置管理员接口，返回 ErrNotSupported。
func (s *qqSender) SetAdmin(_ stdctx.Context, _, _ string, _ bool) error {
	return platform.ErrNotSupported
}

// ────────────────────────────────────────────────────────────────────────────
// platform.InvitationHandler（入群申请审批，2026-08 新增能力）
// ────────────────────────────────────────────────────────────────────────────

// AcceptGroupInvite 通过审批入群申请实现 platform.InvitationHandler。
//
// inviteID 为 GROUP_JOIN_REQUEST 事件中编码的
// "group_openid:member_openid:join_request_id" 格式字符串
// （存于 ChatInfo.Tokens[TokenJoinRequest]）。
func (s *qqSender) AcceptGroupInvite(ctx stdctx.Context, inviteID string) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	gid, mid, jid, err := parseJoinInviteID(inviteID)
	if err != nil {
		return err
	}
	_, err = s.api.ApproveJoinRequest(ctx, gid, mid, &dto.ApprovalJoinRequest{
		Op:            "approve",
		JoinRequestID: jid,
	})
	return err
}

// RejectGroupInvite 通过拒绝入群申请实现 platform.InvitationHandler。
// reason 为拒绝理由（可选）。
func (s *qqSender) RejectGroupInvite(ctx stdctx.Context, inviteID, reason string) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	gid, mid, jid, err := parseJoinInviteID(inviteID)
	if err != nil {
		return err
	}
	_, err = s.api.ApproveJoinRequest(ctx, gid, mid, &dto.ApprovalJoinRequest{
		Op:            "decline",
		JoinRequestID: jid,
		RejectReason:  reason,
	})
	return err
}

// AcceptFriendRequest QQ 官方 v2 无好友申请审批接口，返回 ErrNotSupported。
func (s *qqSender) AcceptFriendRequest(_ stdctx.Context, _ string) error {
	return platform.ErrNotSupported
}

// RejectFriendRequest QQ 官方 v2 无好友申请审批接口，返回 ErrNotSupported。
func (s *qqSender) RejectFriendRequest(_ stdctx.Context, _, _ string) error {
	return platform.ErrNotSupported
}

// parseJoinInviteID 解析 "group_openid:member_openid:join_request_id" 格式的邀请 ID。
func parseJoinInviteID(id string) (groupOpenID, memberOpenID, joinRequestID string, err error) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("qq sender: invalid join invite id %q, want group_openid:member_openid:join_request_id", id)
	}
	return parts[0], parts[1], parts[2], nil
}

// ────────────────────────────────────────────────────────────────────────────
// platform.GroupInfoProvider（群信息查询，2026-08 新增能力）
// ────────────────────────────────────────────────────────────────────────────

// GetGroupInfo 通过获取群基本信息实现 platform.GroupInfoProvider。
func (s *qqSender) GetGroupInfo(ctx stdctx.Context, groupID string) (platform.GroupInfo, error) {
	if s.api == nil {
		return platform.GroupInfo{}, fmt.Errorf("qq sender: openAPI client is nil")
	}
	result, err := s.api.GetGroupInfo(ctx, groupID)
	if err != nil {
		return platform.GroupInfo{}, err
	}
	return platform.GroupInfo{
		ID:          result.Get("group_openid").String(),
		Name:        result.Get("group_name").String(),
		MemberCount: int(result.Get("group_member_num").Int()),
		Description: result.Get("group_finger_memo").String(),
	}, nil
}

// GetGroupMemberList QQ 官方 v2 无群成员列表接口，返回 ErrNotSupported。
func (s *qqSender) GetGroupMemberList(_ stdctx.Context, _ string) ([]platform.GroupMemberInfo, error) {
	return nil, platform.ErrNotSupported
}

// GetGroupMember QQ 官方 v2 无单成员查询接口，返回 ErrNotSupported。
func (s *qqSender) GetGroupMember(_ stdctx.Context, _, _ string) (platform.GroupMemberInfo, error) {
	return platform.GroupMemberInfo{}, platform.ErrNotSupported
}

// GetJoinedGroups QQ 官方 v2 无机器人已加入群列表接口，返回 ErrNotSupported。
func (s *qqSender) GetJoinedGroups(_ stdctx.Context) ([]platform.GroupInfo, error) {
	return nil, platform.ErrNotSupported
}
