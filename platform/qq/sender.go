package qq

import (
	"bytes"
	stdctx "context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "golang.org/x/image/webp"

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
		att := msg.Attachments[0]
		// Markdown + 图片附件：图片以原生 markdown 图片语法内嵌，
		// 正文按 Markdown 渲染（图文同条，无需二次上传展示）。
		if msg.Markdown != "" && att.Kind == platform.AttachmentKindImage {
			return s.sendMarkdownWithImage(ctx, chat, msg, att)
		}
		return s.sendAttachment(ctx, chat, msg, att)
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
	uploadResult, err := s.uploadChunked(ctx, chat, att)
	if err != nil {
		return platform.SendResult{}, err
	}
	return s.sendMediaMessage(ctx, chat, msg, uploadResult.Get("file_info").String(), uploadResult)
}

// uploadChunked 执行分片上传的完整四步流程，返回合并（merge）响应——
// 其中除 file_info 外，图片/视频/语音还携带 raw_url（COS 预签名 GET URL，
// 公网可访问，有效期与 ttl 一致）。上传完成即返回，不发送消息。
func (s *qqSender) uploadChunked(ctx stdctx.Context, chat platform.ChatInfo, att platform.Attachment) (gjson.Result, error) {
	fileType := attachmentKindToFileType(att.Kind)
	fileMD5 := md5Sum(att.Data)
	prepareReq := &dto.UploadPrepareRequest{
		FileType: fileType,
		FileSize: strconv.FormatInt(int64(len(att.Data)), 10),
		FileName: att.Name,
		FileMD5:  fileMD5,
		FileSHA1: sha1Sum(att.Data),
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
		return gjson.Result{}, platform.NewSendError(
			platform.SendErrNetworkError, "qq", chat.ID,
			fmt.Sprintf("chunked upload prepare failed: %v", err), 0, err,
		)
	}

	uploadID := prepare.Get("upload_id").String()
	blockSize := int(prepare.Get("block_size").Int())
	if blockSize <= 0 {
		return gjson.Result{}, fmt.Errorf("qq sender: chunked upload prepare returned invalid block_size (response: %s)", prepare.Raw)
	}
	parts := prepare.Get("parts").Array()
	if len(parts) == 0 {
		return gjson.Result{}, fmt.Errorf("qq sender: chunked upload prepare returned no parts (response: %s)", prepare.Raw)
	}

	// 2+3. 分片 PUT + 完成确认
	data := att.Data
	for i, part := range parts {
		start := i * blockSize
		if start >= len(data) {
			break
		}
		end := min(start+blockSize, len(data))
		chunk := data[start:end]

		presignedURL := part.Get("presigned_url").String()
		if presignedURL == "" {
			return gjson.Result{}, fmt.Errorf("qq sender: chunked upload prepare returned part %d without presigned_url (response: %s)", i, prepare.Raw)
		}
		if err := putPresignedChunk(ctx, presignedURL, chunkContentType(att), chunk); err != nil {
			return gjson.Result{}, platform.NewSendError(
				platform.SendErrNetworkError, "qq", chat.ID,
				fmt.Sprintf("chunked upload PUT part %d failed: %v", i, err), 0, err,
			)
		}

		// part_index 使用服务端返回的 UploadPart.index（真实响应从 1 开始，
		// 文档示例从 0 开始）；字节偏移按数组顺序累加 blockSize。
		finishReq := &dto.UploadPartFinishRequest{UploadID: uploadID, PartIndex: int(part.Get("index").Int())}
		if chat.IsGroup {
			_, err = s.api.GroupUploadPartFinish(ctx, chat.ID, finishReq)
		} else {
			_, err = s.api.UserUploadPartFinish(ctx, chat.ID, finishReq)
		}
		if err != nil {
			return gjson.Result{}, platform.NewSendError(
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
		return gjson.Result{}, platform.NewSendError(
			platform.SendErrNetworkError, "qq", chat.ID,
			fmt.Sprintf("chunked upload merge failed: %v", err), 0, err,
		)
	}
	fileInfo := uploadResult.Get("file_info").String()
	if fileInfo == "" {
		return gjson.Result{}, fmt.Errorf("qq sender: chunked upload merge returned empty file_info (response: %s)", uploadResult.Raw)
	}

	return uploadResult, nil
}

// chunkContentType 返回分片上传对象的 Content-Type。
// 该类型会存储为 COS 对象元数据，决定 raw_url 下载时的响应 Content-Type
// （QQ markdown 内嵌图片依赖它被识别为图片）。
// 判定优先级：二进制魔数嗅探（最可靠，字节就是真相）→ 附件自带 MIME →
// 按附件类型兜底。
func chunkContentType(att platform.Attachment) string {
	if len(att.Data) > 0 {
		if ct := sniffContentType(att.Data); ct != "" {
			return ct
		}
	}
	if att.MimeType != "" {
		return att.MimeType
	}
	switch att.Kind {
	case platform.AttachmentKindImage:
		return "image/png"
	case platform.AttachmentKindVideo:
		return "video/mp4"
	case platform.AttachmentKindAudio:
		return "audio/silk"
	default:
		return "application/octet-stream"
	}
}

// sniffContentType 按二进制魔数嗅探常见图片/媒体类型，未知返回空串。
// 仅凭声明（MIME/扩展名）不可靠——pic 压缩链路就可能把 PNG 转 JPEG。
func sniffContentType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp"
	case len(data) >= 12 && (bytes.Equal(data[4:8], []byte("ftypavc1")) || bytes.Equal(data[4:8], []byte("ftypisom"))):
		return "video/mp4"
	}
	return ""
}

// qqMarkdownImageMaxDisplay QQ markdown 内嵌图片的最长显示边（像素）。
// 原生 markdown 图片语法要求显式尺寸参数（#宽px #高px），按原图比例缩放
// 到该边长内展示，避免超大图撑爆排版。
const qqMarkdownImageMaxDisplay = 600

// sendMarkdownWithImage 发送"图片 + Markdown 正文"同条消息（msg_type=2）。
//
// QQ 原生 Markdown 支持图片语法 `![text #宽px #高px](公网URL)`，开放平台
// 会在发送时下载转存该资源。图片链接来源：
//   - URL 附件：直接嵌入（平台转存）
//   - Data 附件：URL 直传接口不返回公网链接，强制走分片上传——合并响应
//     携带 raw_url（COS 预签名 GET URL），可嵌入 markdown
//
// Markdown 与富媒体是互斥的 msg_type，file_info 无法直接嵌入 markdown，
// 因此本地图片必须先上传换取公网 URL。
// Markdown 发送失败（如未申请 markdown 权限 304036）时回退为
// msg_type=7 富媒体图文混排，正文降级为纯文本，保证图片与信息不丢失。
func (s *qqSender) sendMarkdownWithImage(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage, att platform.Attachment) (platform.SendResult, error) {
	imgURL := att.URL
	if imgURL == "" {
		uploadResult, uerr := s.uploadChunked(ctx, chat, att)
		if uerr == nil {
			imgURL = uploadResult.Get("raw_url").String()
		}
		if imgURL == "" {
			// 拿不到公网 URL（上传失败 / 服务端未返回 raw_url）：
			// 无法内嵌图片，回退富媒体图文路径。
			return s.sendAttachmentWithPlainText(ctx, chat, msg, att)
		}
	}

	// 图片前置、信息在下：与 Telegram/Discord 的 caption 渲染顺序一致
	md := qqMarkdownImage(att, imgURL) + "\n\n" + msg.Markdown
	msgMD := msg
	msgMD.Markdown = md
	msgMD.Text = ""
	msgMD.Attachments = nil // 图片已在 markdown 内嵌，避免走富媒体分支
	dtoMsg := s.buildDTOMessage(msgMD, chat)

	logger.WithFields(logger.Fields{
		"chat": chat.ID, "group": chat.IsGroup, "image_url": imgURL,
	}).Debugf("[qq.Sender] sending markdown image message")

	var raw gjson.Result
	var err error
	if chat.IsGroup {
		raw, err = s.api.GroupChat(ctx, chat.ID, dtoMsg)
	} else {
		raw, err = s.api.SingleChat(ctx, chat.ID, dtoMsg)
	}
	if err != nil {
		logger.WithError(err).Warnf(
			"[qq.Sender] markdown image message failed, falling back to rich media (msg_type=7)")
		return s.sendAttachmentWithPlainText(ctx, chat, msg, att)
	}
	return buildSendResult(raw), nil
}

// qqMarkdownImage 构造 QQ 原生 markdown 的图片片段。
//
// 语法要求显式尺寸参数（社区实测缺 #宽px #高px 时图片不渲染，只有文字/链接），
// 尺寸按图片真实比例缩放到最长边 qqMarkdownImageMaxDisplay 内。
// 图片尺寸从附件二进制的头部解出（DecodeConfig 只读文件头，不解码像素）；
// URL 附件（无本地字节）或无法解析尺寸时退化为无尺寸参数写法。
func qqMarkdownImage(att platform.Attachment, imgURL string) string {
	alt := "image"
	w, h := qqImageDisplaySize(att)
	if w > 0 && h > 0 {
		return fmt.Sprintf("![%s #%dpx #%dpx](%s)", alt, w, h, imgURL)
	}
	return fmt.Sprintf("![%s](%s)", alt, imgURL)
}

// qqImageDisplaySize 从附件数据解出图片显示尺寸（最长边缩放到
// qqMarkdownImageMaxDisplay 内，至少 1px）。无法解析时返回 (0, 0)。
func qqImageDisplaySize(att platform.Attachment) (w, h int) {
	if len(att.Data) == 0 {
		return 0, 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(att.Data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0
	}
	w, h = cfg.Width, cfg.Height
	if maxSide := max(w, h); maxSide > qqMarkdownImageMaxDisplay {
		w = max(1, w*qqMarkdownImageMaxDisplay/maxSide)
		h = max(1, h*qqMarkdownImageMaxDisplay/maxSide)
	}
	return w, h
}

// sendAttachmentWithPlainText 将 Markdown 正文降级为纯文本后走
// msg_type=7 富媒体图文混排发送（真机验证 media+content 同条可用）。
func (s *qqSender) sendAttachmentWithPlainText(ctx stdctx.Context, chat platform.ChatInfo, msg platform.OutboundMessage, att platform.Attachment) (platform.SendResult, error) {
	msgFB := msg
	if msgFB.Text == "" && msgFB.Markdown != "" {
		msgFB.Text = plainTextFromMarkdown(msgFB.Markdown)
		msgFB.Markdown = ""
	}
	return s.sendAttachment(ctx, chat, msgFB, att)
}

// markdownLinkRe 匹配 Markdown 链接 [text](url)。
var markdownLinkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// markdownEmphasisRe 匹配行内强调标记（**bold**、*em*、`code`、~~del~~）。
var markdownEmphasisRe = regexp.MustCompile(`\*\*([^*]+)\*\*|\*([^*]+)\*|` + "`" + `([^` + "`" + `]+)` + "`" + `|~~([^~]+)~~`)

// plainTextFromMarkdown 将 Markdown 正文降级为可读纯文本：
// 链接转为 "text (url)"，去除强调标记与标题前缀。用于富媒体消息的
// content（msg_type=7 纯文本，不渲染 Markdown 语法）。
func plainTextFromMarkdown(md string) string {
	out := markdownLinkRe.ReplaceAllString(md, "$1 ($2)")
	out = markdownEmphasisRe.ReplaceAllStringFunc(out, func(m string) string {
		for _, sub := range markdownEmphasisRe.FindStringSubmatch(m)[1:] {
			if sub != "" {
				return sub
			}
		}
		return m
	})
	// 标题前缀
	out = regexp.MustCompile(`(?m)^#{1,6}\s+`).ReplaceAllString(out, "")
	return strings.TrimSpace(out)
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

	// 真机验证（2026-09，C2C/群聊）：msg_type=7 可同时携带 media 与 content，
	// QQ 客户端会把图片与正文渲染在同一条消息里。此前单聊一律清空 content，
	// 导致"图片+文字"场景的文字被吞掉，这里不再清空。
	// 纯图片无正文时直接省略 content（官方富媒体示例即不带 content）。早期文档
	// 把群聊 content 标注为必填，曾用空格占位兜底，但真机验证（2026-09）显示
	// content:" " 会让 QQ 端在图片下方渲染出多余空格，且省略 content 后群聊
	// 纯图（含带 message_reference 的被动回复）均发送正常，故不再补空格。

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
//
// contentType 会写入 COS 对象的元数据：预签名 URL 仅签名 host 头
// （q-header-list=host），附加 Content-Type 不破坏签名。若不设置，
// COS 默认存为 application/octet-stream——QQ markdown 内嵌图片
// （sendMarkdownWithImage）依赖合并响应的 raw_url，对象类型不明会导致
// 转存/渲染失败（客户端显示破损图片），因此必须带上真实类型。
func putPresignedChunk(ctx stdctx.Context, presignedURL, contentType string, chunk []byte) error {
	req := httpclient.Put(presignedURL).
		SetContext(ctx).
		SetTimeout(chunkPutTimeout).
		SetBody(bytes.NewReader(chunk))
	if contentType != "" {
		req = req.SetHeader("Content-Type", contentType)
	}
	resp, err := req.Do()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Close() }()
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

// sha1Sum 计算数据的十六进制 SHA1（QQ upload_prepare 要求的必填校验值）。
func sha1Sum(data []byte) string {
	h := sha1.Sum(data)
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
		RefIDX:    raw.Get("ext_info.ref_idx").String(),
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
		// 来自发送响应 ext_info.ref_idx：引用机器人自己的消息时作为 message_reference.message_id
		RefIDX: sendRaw.Get("ext_info.ref_idx").String(),
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

	// 引用回复：展示消息气泡引用（不同于被动回复关联）。
	// 显式 ReplyToID 优先；未显式指定但消息带 QuoteTrigger（引用触发消息）时，
	// 用事件授权的消息 ID（频道 payload.ID 即 message id）作为引用目标。
	quoteID := msg.ReplyToID
	if quoteID == "" && msg.QuoteTrigger {
		quoteID = chat.Tokens[TokenMsgID]
	}
	if quoteID != "" {
		guildMsg.MessageReference = &dto.MessageReference{
			MessageID:             quoteID,
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
// 被动授权：
//   - msg_id：chat.Tokens[TokenMsgID]（C2C_MESSAGE_CREATE / GROUP_AT_MESSAGE_CREATE
//     自动填充，即事件 d.id）
//   - event_id：extra.EventID（手动 ApplyExtra）> chat.Tokens[TokenEventID]
//     （INTERACTION_CREATE / C2C_MSG_RECEIVE 等自动填充）
//
// msg_id/event_id 是"被动回复授权"，与"引用展示"相互独立：ReplyToID 只映射
// message_reference（引用气泡），不再充当 msg_id 的来源。
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

	// msg_id：被动回复授权 token（message-based），只取事件自动填充的授权
	//（C2C_MESSAGE_CREATE / GROUP_AT_MESSAGE_CREATE 的 d.id）。
	// 注意：不再用 msg.ReplyToID 充当 msg_id——ReplyToID 是引用目标
	//（REFIDX_...，展示气泡），不是本条被动消息的授权；两者已解耦。
	if resolvedMsgID := chat.Tokens[TokenMsgID]; resolvedMsgID != "" {
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

	// 引用回复：设置 MessageReference（展示被引用消息气泡），与被动授权无关
	// （对齐频道的 buildGuildDTOMessage）。取值应为 REFIDX_...：引用用户消息时
	// 来自事件 message_scene.ext 的 msg_idx/ref_msg_idx（入站 reply 段解析值）；
	// 引用机器人自己的消息时来自发送响应 ext_info.ref_idx（见 SendResult.RefIDX）。
	// 真机验证（2026-09）：被动回复（msg_id）+ message_reference 可同时携带，
	// 文本 / Markdown / 媒体 / 图文混排均正常。
	//
	// 优先级：显式 ReplyToID（reply 段 / 手动 WithReply）> QuoteTrigger
	// （引用触发消息自身：解析事件 msg_idx → TokenQuoteID，见 populateC2C /
	// populateGroupAt）。事件未提供可引用的 msg_idx 时（如主动消息）QuoteTrigger
	// 静默不生效；召回消息（IsWakeup）不携带来源引用。
	quoteID := msg.ReplyToID
	if quoteID == "" && msg.QuoteTrigger && !extra.IsWakeup {
		quoteID = chat.Tokens[TokenQuoteID]
	}
	if quoteID != "" {
		dtoMsg.MessageReference = &dto.MessageReference{
			MessageID:             quoteID,
			IgnoreGetMessageError: true,
		}
	}

	// 操作按钮与提示键盘：与 keyboard 字段相互独立，msg_type 保持原样。
	dtoMsg.ActionButton = extra.ActionButton
	dtoMsg.PromptKeyboard = extra.PromptKeyboard

	return dtoMsg
}

// qqSegmentsToFlat 将统一出站段折叠为 QQ 便捷字段等价物（段路径）。
//
// QQ 的文本接口支持内联 AT 标签（<qqbot-at-user id="..."/>），因此文本/at
// 可以保序交错进 Content；媒体取首个（QQ 单媒体限制）；富媒体消息（msg_type=7）
// 支持携带 content，文本/at 折叠后可与图片在同一条消息展示（2026-09 真机验证）；
// reply 段 → ReplyToID（Sender 统一映射 message_reference 引用气泡，
// 见 buildDTOMessage / buildGuildDTOMessage；被动授权仍由事件 Tokens 提供）；
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
		// 图文同发：msg_type=7 可同时携带 media 与 content（2026-09 C2C/群聊真机验证）。
		// 注意 QQ 一条消息只有一个 media，多图仍须逐条发送（MultiAttachment=false）。
		Caption: true,
		Forward: true, // 合并转发（msg_type=3 富媒体外，msg_elements 引用内可含 forward）
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

// KickMember 通过官方群成员批量移除接口实现 platform.GroupManager
// （POST /v2/groups/{group_openid}/batch_remove_members，2026-09 新增）。
//
// permanent=true 时同时加入群黑名单（add_to_member_blacklist=true，禁止重新
// 加入）；false 时仅移除出群。移除操作需要机器人拥有群管理员身份，且仅能
// 操作普通成员（不能移除群主/管理员/机器人）。该接口目前仅白名单机器人可用。
func (s *qqSender) KickMember(ctx stdctx.Context, groupID, userID string, permanent bool) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	_, err := s.api.BatchRemoveGroupMembers(ctx, groupID, &dto.BatchRemoveGroupMembersRequest{
		MemberOpenIDs:        []string{userID},
		AddToMemberBlacklist: permanent,
	})
	return err
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

// GetGroupMemberList 通过 cursor 分页拉取群成员列表实现
// platform.GroupInfoProvider。
//
// 官方 v2 接口（2026-09 更新为 GET /v2/groups/{group_openid}/members）每次
// 最多返回 30 条，且返回 username/member_role/joined_at 等资料，可填充
// DisplayName/GroupRole/JoinedAt（头像仍不提供）。此处循环拉取直至服务端
// 返回空 next_cursor（末页）。
func (s *qqSender) GetGroupMemberList(ctx stdctx.Context, groupID string) ([]platform.GroupMemberInfo, error) {
	if s.api == nil {
		return nil, fmt.Errorf("qq sender: openAPI client is nil")
	}
	var members []platform.GroupMemberInfo
	cursor := ""
	for {
		result, err := s.api.GetGroupMemberList(ctx, groupID, cursor)
		if err != nil {
			return nil, err
		}
		for _, m := range result.Get("members").Array() {
			members = append(members, groupMemberFromResult(m))
		}
		next := result.Get("next_cursor").String()
		if next == "" || next == cursor {
			break
		}
		cursor = next
	}
	return members, nil
}

// GetGroupMember 通过官方单成员查询接口实现 platform.GroupInfoProvider
// （GET /v2/groups/{group_openid}/members/{member_openid}，2026-09 新增）。
func (s *qqSender) GetGroupMember(ctx stdctx.Context, groupID, userID string) (platform.GroupMemberInfo, error) {
	if s.api == nil {
		return platform.GroupMemberInfo{}, fmt.Errorf("qq sender: openAPI client is nil")
	}
	result, err := s.api.GetGroupMember(ctx, groupID, userID)
	if err != nil {
		return platform.GroupMemberInfo{}, err
	}
	return groupMemberFromResult(result), nil
}

// groupMemberFromResult 将官方群成员对象（成员列表/单成员查询响应）
// 映射为 platform.GroupMemberInfo。
func groupMemberFromResult(m gjson.Result) platform.GroupMemberInfo {
	info := platform.GroupMemberInfo{
		UserID:      m.Get("member_openid").String(),
		DisplayName: m.Get("username").String(),
		GroupRole:   parseQQGroupRole(m.Get("member_role").String()),
	}
	if ts := m.Get("joined_at").String(); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			info.JoinedAt = t
		}
	}
	return info
}

// GetJoinedGroups QQ 官方 v2 无机器人已加入群列表接口，返回 ErrNotSupported。
func (s *qqSender) GetJoinedGroups(_ stdctx.Context) ([]platform.GroupInfo, error) {
	return nil, platform.ErrNotSupported
}
