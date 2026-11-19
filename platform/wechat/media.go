package wechat

// media.go — 素材上传与入站媒体下载。
//
// 企业微信的两处媒体限制决定了本文件的形态：
//
//  1. 出站**不能**直接发 URL 或 base64：必须先用分片上传换 `media_id`
//     （aibot_upload_media_init → chunk × N → finish），再用 media_id 发送。
//     分片上限 512KB（Base64 之前），最多 100 片，上传会话 30 分钟内有效。
//  2. 入站直链只有 5 分钟有效期且**内容是加密的**（AES-256-CBC），
//     每条链接自带唯一 aeskey。拿到 URL 而不解密，得到的只是密文。
//
// 上传只在长连接模式下可用：URL 回调模式没有这条命令，因此
// Capabilities.FileUpload 会随模式变化。

import (
	stdctx "context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/platform"
)

const (
	// mediaChunkSize 是单个上传分片的原始字节上限（Base64 编码前）。
	mediaChunkSize = 512 << 10 // 512 KiB
	// mediaMaxChunks 是单次上传允许的最大分片数。
	mediaMaxChunks = 100
	// mediaUploadTimeout 是整次上传（初始化 + 全部分片 + 完成）的超时。
	mediaUploadTimeout = 120 * time.Second
	// mediaDownloadTimeout 是单次媒体下载的超时。
	mediaDownloadTimeout = 60 * time.Second
	// mediaMaxDownloadBytes 是入站媒体单文件下载上限（普通文件上限 20MB）。
	mediaMaxDownloadBytes = 20 << 20
)

// 各类型素材的大小上限（官方素材接口约束）。
const (
	maxImageBytes = 10 << 20
	maxVoiceBytes = 2 << 20
	maxVideoBytes = 10 << 20
	maxFileBytes  = 20 << 20
)

// limitFor 返回素材类型对应的体积上限。
func limitFor(kind MediaKind) int {
	switch kind {
	case MediaKindImage:
		return maxImageBytes
	case MediaKindVoice:
		return maxVoiceBytes
	case MediaKindVideo:
		return maxVideoBytes
	default:
		return maxFileBytes
	}
}

// UploadMedia 分片上传一份素材，返回 media_id（3 天内可重复使用）。
//
// 只在长连接模式可用；URL 回调模式返回 [platform.ErrNotSupported]。
func (s *wechatSender) UploadMedia(ctx stdctx.Context, kind MediaKind, filename string, data []byte) (string, error) {
	client := s.adapter.currentClient()
	if client == nil {
		return "", fmt.Errorf("wechat: 长连接未就绪，无法上传素材（URL 回调模式不支持上传）")
	}
	if len(data) == 0 {
		return "", errors.New("wechat: 素材内容为空")
	}
	if limit := limitFor(kind); len(data) > limit {
		return "", fmt.Errorf("wechat: %s 素材 %d 字节，超过平台上限 %d 字节", kind, len(data), limit)
	}
	if filename == "" {
		filename = "file"
	}

	total := (len(data) + mediaChunkSize - 1) / mediaChunkSize
	if total > mediaMaxChunks {
		return "", fmt.Errorf("wechat: 需要 %d 个分片，超过平台上限 %d", total, mediaMaxChunks)
	}

	uploadCtx, cancel := stdctx.WithTimeout(ctx, mediaUploadTimeout)
	defer cancel()

	initFrame, err := client.request(uploadCtx, cmdUploadInit, uploadInitBody{
		Type:        string(kind),
		Filename:    filename,
		TotalSize:   len(data),
		TotalChunks: total,
		MD5:         md5Hex(data),
	})
	if err != nil {
		return "", err
	}
	if !initFrame.ok() {
		return "", fmt.Errorf("wechat: 初始化上传失败: errcode=%d errmsg=%s",
			initFrame.errorCode(), initFrame.ErrMsg)
	}
	var initRes uploadInitResult
	if err := unmarshalFrameBody(initFrame, &initRes); err != nil {
		return "", fmt.Errorf("wechat: 解析上传初始化的 upload_id 失败: %w", err)
	}
	if initRes.UploadID == "" {
		return "", errors.New("wechat: 上传初始化未返回 upload_id")
	}

	for i := range total {
		start := i * mediaChunkSize
		end := min(start+mediaChunkSize, len(data))
		chunk := uploadChunkBody{
			UploadID:   initRes.UploadID,
			ChunkIndex: i,
			Base64Data: base64.StdEncoding.EncodeToString(data[start:end]),
		}
		frame, err := client.request(uploadCtx, cmdUploadChunk, chunk)
		if err != nil {
			return "", fmt.Errorf("wechat: 上传第 %d/%d 片失败: %w", i+1, total, err)
		}
		if !frame.ok() {
			return "", fmt.Errorf("wechat: 上传第 %d/%d 片被拒: errcode=%d errmsg=%s",
				i+1, total, frame.errorCode(), frame.ErrMsg)
		}
	}

	// uploadInitResult 与 uploadFinishBody 字段完全一致，直接转换即可
	// （staticcheck 会提示改用转换而非结构体字面量）。
	finishFrame, err := client.request(uploadCtx, cmdUploadFinish, uploadFinishBody(initRes))
	if err != nil {
		return "", err
	}
	if !finishFrame.ok() {
		return "", fmt.Errorf("wechat: 完成上传失败: errcode=%d errmsg=%s",
			finishFrame.errorCode(), finishFrame.ErrMsg)
	}
	var finishRes uploadFinishResult
	if err := unmarshalFrameBody(finishFrame, &finishRes); err != nil {
		return "", fmt.Errorf("wechat: 解析 media_id 失败: %w", err)
	}
	if finishRes.MediaID == "" {
		return "", errors.New("wechat: 上传完成但未返回 media_id")
	}
	return finishRes.MediaID, nil
}

// DownloadMedia 下载并解密入站媒体。
//
// 入站直链内容是 AES-256-CBC 加密的，aeskey 随附件放在
// Extra[ExtraKeyAesKey]；直链 5 分钟过期，必须尽快用。
func (s *wechatSender) DownloadMedia(ctx stdctx.Context, att platform.Attachment) ([]byte, error) {
	if att.URL == "" {
		return nil, errors.New("wechat: 附件没有下载地址")
	}
	raw, err := s.fetchBytes(ctx, att.URL, mediaMaxDownloadBytes)
	if err != nil {
		return nil, err
	}
	aesKey, _ := att.Extra[ExtraKeyAesKey].(string)
	if aesKey == "" {
		// 没有 aeskey 说明资源未加密（或来自老版本回调）：原样返回。
		return raw, nil
	}
	plain, err := DecryptMedia(raw, aesKey)
	if err != nil {
		return nil, fmt.Errorf("wechat: 解密媒体失败: %w", err)
	}
	return plain, nil
}

// fetchBytes 下载一段带体积上限的内容。
func (s *wechatSender) fetchBytes(ctx stdctx.Context, url string, maxBytes int) ([]byte, error) {
	dlCtx, cancel := stdctx.WithTimeout(ctx, mediaDownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("wechat: 构造下载请求失败: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wechat: 下载媒体失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wechat: 下载媒体返回 HTTP %d", resp.StatusCode)
	}
	// 多读 1 字节用于判断是否超限：直接 ReadAll 会让一个超大响应把进程打到 OOM。
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("wechat: 读取媒体失败: %w", err)
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("wechat: 媒体超过 %d 字节上限", maxBytes)
	}
	return data, nil
}

// mediaKindOf 把附件类型映射到上传用的素材类型。
func mediaKindOf(att platform.Attachment) MediaKind {
	switch att.Kind {
	case platform.AttachmentKindImage:
		return MediaKindImage
	case platform.AttachmentKindAudio:
		return MediaKindVoice
	case platform.AttachmentKindVideo:
		return MediaKindVideo
	default:
		return MediaKindFile
	}
}

// loadAttachment 取得附件字节：优先用 Data，其次下载 URL。
//
// URL 分支是"插件给了一个远程图片"的常见情形；企业微信不能用 URL 直接发，
// 只能先下回来再上传。失败不做降级（降级成纯文本会静默丢图片）。
func (s *wechatSender) loadAttachment(ctx stdctx.Context, att platform.Attachment) ([]byte, MediaKind, string, error) {
	kind := mediaKindOf(att)
	if len(att.Data) > 0 {
		return att.Data, kind, att.Name, nil
	}
	if att.URL == "" {
		return nil, kind, "", errors.New("wechat: 附件既无 Data 也无 URL")
	}
	data, err := s.fetchBytes(ctx, att.URL, limitFor(kind))
	if err != nil {
		return nil, kind, "", err
	}
	if att.Name != "" {
		return data, kind, att.Name, nil
	}
	name := nameFromURL(att.URL)
	if name == "" {
		name = "file"
	}
	return data, kind, name, nil
}

// nameFromURL 从 URL 路径里猜一个文件名。
func nameFromURL(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		raw = raw[i+1:]
	}
	return raw
}

// md5Hex 计算十六进制 MD5（上传初始化可选字段，服务端用于合并后校验）。
func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// unmarshalFrameBody 解析回执帧的 body。
func unmarshalFrameBody(frame *Frame, out any) error {
	if len(frame.Body) == 0 {
		return errors.New("回执 body 为空")
	}
	return jsonx.Unmarshal(frame.Body, out)
}
