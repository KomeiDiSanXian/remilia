package qq

import (
	"bytes"
	stdctx "context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/httpclient"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// material.go — 素材管理（2026-10-21 官方新增能力）。
//
// 用途：把 Markdown 消息要用的图片先上传到平台，换取 `qqbot-media://` 引用，
// 之后在 Markdown 里按普通图片语法引用，不依赖公网 URL，也不受"外部图床挂了
// / 链接带鉴权参数"影响。
//
// 上游流程是三步（prepare → PUT 预签名 URL → complete），这里收敛成一步
// UploadMediaImage，并在 complete 时用 PUT 响应的 ETag 作为 md5（平台会拿它
// 与 COS 对象的实际 ETag 比对，不一致返回 40035003）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/material-management.html

// materialMaxImageBytes 单张素材图片的大小上限（平台硬限制 10MB）。
const materialMaxImageBytes = 10 * 1024 * 1024

// MaterialMaxImageCount 单机器人素材图片数量上限（平台默认 100 张）。
//
// 达到上限后 prepare/complete 返回 40035007，需要先删掉不再使用的图片。
// 批量上传（如每分钟生成一张图表）前可以用 ListMediaImages 核对剩余配额。
const MaterialMaxImageCount = 100

// materialAllowedTypes 平台允许的素材图片 MIME 类型 → 推荐扩展名。
//
// 扩展名与 MIME 必须一一对应，否则 prepare 直接返回 40035001。
var materialAllowedTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// MediaImage 一张已上传到平台的素材图片。
type MediaImage struct {
	// URL Markdown 引用链接（`qqbot-media://image/v1/<token>`）。
	// 直接用 MarkdownImage 渲染成图片语法，或自行拼进 Markdown。
	URL string
	// ImageID 图片 ID，用于删除（DeleteMediaImage）。
	ImageID string
	// FileName 上传时登记的文件名。
	FileName string
	// TTL 滑动有效期（秒）：每次被消息引用后顺延，连续 90 天未被引用则失效。
	// 来自上传响应的 ttl 字段（查询列表里由 ExpiresAt 表达剩余期限）。
	TTL uint32
	// PreviewURL 临时预览链接（短时效），仅供发送前自查，勿持久化。
	PreviewURL string
	// CreatedAt 上传时间（Unix 秒）。仅查询列表（ListMediaImages）填充。
	CreatedAt int64
	// ExpiresAt 过期时间（Unix 秒），每次被引用后自动顺延。
	// 仅查询列表（ListMediaImages）填充。
	ExpiresAt int64
}

// MarkdownImage 把素材图片渲染成 QQ Markdown 图片语法。
//
// width/height 为显示尺寸（像素）。QQ 原生 markdown 缺 `#宽px #高px` 时
// 图片不渲染（只剩文字/链接），因此这里总会带上尺寸；两者传 0 时用 600px
// 作为长边，与本地图片内嵌路径（sendMarkdownWithImage）保持一致。
func (m MediaImage) MarkdownImage(width, height int) string {
	if m.URL == "" {
		return ""
	}
	if width <= 0 || height <= 0 {
		width, height = qqMarkdownImageMaxDisplay, qqMarkdownImageMaxDisplay
	}
	return fmt.Sprintf("![image #%dpx #%dpx](%s)", width, height, m.URL)
}

// MediaImageUploader 是 QQ Sender 的素材管理扩展能力。
//
// qq.NewSender / Adapter.Sender() 返回的是 platform.Sender 接口，素材管理是
// QQ 专属能力，因此需要一次类型断言（与 platform.GroupInfoProvider 的用法一致）：
//
//	sender := adapter.Sender()
//	if up, ok := sender.(qq.MediaImageUploader); ok {
//	    img, err := up.UploadMediaImage(ctx, platform.Attachment{
//	        Kind: platform.AttachmentKindImage, Name: "chart.png", Data: pngBytes,
//	    })
//	    if err != nil { ... }
//	    ctx.Reply(platform.MarkdownMessage("## 图表\n\n" + img.MarkdownImage(800, 400)))
//	}
type MediaImageUploader interface {
	// UploadMediaImage 上传图片二进制为平台素材，返回可复用的 Markdown 引用。
	UploadMediaImage(ctx stdctx.Context, att platform.Attachment) (MediaImage, error)
	// ListMediaImages 查询有效素材图片，自动跟随 next_cursor 翻页。
	ListMediaImages(ctx stdctx.Context, limit int) ([]MediaImage, error)
	// DeleteMediaImage 删除素材图片并释放数量配额。
	DeleteMediaImage(ctx stdctx.Context, imageID string) error
}

var _ MediaImageUploader = (*qqSender)(nil)

// UploadMediaImage 把图片二进制上传为平台素材，返回可复用的 Markdown 引用。
//
// 图片类型由二进制魔数嗅探决定（声明不可靠：pic 压缩链路会把 PNG 转成 JPEG），
// 嗅探不出来时退回附件自带的 MIME。平台只接受 png / jpeg / gif / webp，
// 其余类型直接报错，而不是发一个必然失败的请求。
//
// 返回的 MediaImage.URL 有 90 天滑动有效期，可缓存复用；不再被引用的图片应
// 通过 DeleteMediaImage 释放配额（单机器人上限 100 张）。
func (s *qqSender) UploadMediaImage(ctx stdctx.Context, att platform.Attachment) (MediaImage, error) {
	if s.api == nil {
		return MediaImage{}, fmt.Errorf("qq sender: openAPI client is nil")
	}
	return uploadMediaImage(ctx, s.api, att)
}

// ListMediaImages 查询当前机器人的有效素材图片，自动跟随 next_cursor 翻页。
//
// limit 为单页数量（传 0 用平台默认值）。图片按上传时间倒序返回，
// 调用方可据 ExpiresAt 清理即将过期的条目。
func (s *qqSender) ListMediaImages(ctx stdctx.Context, limit int) ([]MediaImage, error) {
	if s.api == nil {
		return nil, fmt.Errorf("qq sender: openAPI client is nil")
	}
	return listMediaImages(ctx, s.api, limit)
}

// DeleteMediaImage 删除素材图片并释放数量配额。
//
// 删除后引用链接立即失效（已发出的消息不受影响——发送时平台已转存副本）。
// 删除不存在的 imageID 返回 40035008。
func (s *qqSender) DeleteMediaImage(ctx stdctx.Context, imageID string) error {
	if s.api == nil {
		return fmt.Errorf("qq sender: openAPI client is nil")
	}
	return deleteMediaImage(ctx, s.api, imageID)
}

// UploadMediaImage 是不经过 Sender 的等价入口：调用方只有 openapi 客户端
// （例如在做离线素材预上传）时可以直接用它。
//
//	api := qq.NewSender(...).PlatformAPI() → openapi.MediaImageManager
//	img, err := qq.UploadMediaImage(ctx, api, att)
func UploadMediaImage(ctx stdctx.Context, api openapi.MediaImageManager, att platform.Attachment) (MediaImage, error) {
	if api == nil {
		return MediaImage{}, fmt.Errorf("qq: openAPI client is nil")
	}
	return uploadMediaImage(ctx, api, att)
}

// uploadMediaImage 是 UploadMediaImage 的实现：prepare → PUT → complete。
func uploadMediaImage(ctx stdctx.Context, api openapi.MediaImageManager, att platform.Attachment) (MediaImage, error) {
	if len(att.Data) == 0 {
		return MediaImage{}, fmt.Errorf("qq sender: media image upload requires binary data (URL 附件直接用公网链接即可)")
	}
	if len(att.Data) > materialMaxImageBytes {
		return MediaImage{}, fmt.Errorf(
			"qq sender: media image is %d bytes, exceeds the 10MB platform limit", len(att.Data))
	}

	contentType, ext := materialImageType(att)
	if contentType == "" {
		return MediaImage{}, fmt.Errorf(
			"qq sender: unsupported media image type (platform accepts png/jpeg/gif/webp only)")
	}

	md5Hex := md5Sum(att.Data)
	fileName := att.Name
	if fileName == "" {
		fileName = "image" + ext
	} else if strings.ToLower(filepath.Ext(fileName)) == "" {
		// 平台按扩展名与 MIME 的匹配情况校验，缺扩展名会 40035001。
		fileName += ext
	}

	prepare, err := api.PrepareMediaImage(ctx, &dto.MediaImagePrepareRequest{
		FileName:    fileName,
		FileSize:    uint64(len(att.Data)),
		ContentType: contentType,
		MD5:         md5Hex,
		SHA1:        sha1Sum(att.Data),
	})
	if err != nil {
		return MediaImage{}, fmt.Errorf("qq sender: media image prepare failed: %w", err)
	}

	uploadID := prepare.Get("upload_id").String()
	presignedURL := prepare.Get("presigned_url").String()
	if uploadID == "" || presignedURL == "" {
		return MediaImage{}, fmt.Errorf(
			"qq sender: media image prepare returned no upload_id/presigned_url (response: %s)", prepare.Raw)
	}

	// PUT 直传：Content-Type 必须与 prepare 声明的一致，否则 COS 直接 403。
	etag, err := putPresignedImage(ctx, presignedURL, contentType, att.Data)
	if err != nil {
		return MediaImage{}, fmt.Errorf("qq sender: media image PUT failed: %w", err)
	}
	// complete 以 PUT 响应的 ETag 为准（服务端会与 COS 对象实际 ETag 比对，
	// 不一致返回 40035003）；COS 未回 ETag 时退回本地计算的 MD5。
	if etag == "" {
		etag = md5Hex
	}

	complete, err := api.CompleteMediaImage(ctx, &dto.MediaImageCompleteRequest{
		UploadID: uploadID,
		MD5:      etag,
	})
	if err != nil {
		return MediaImage{}, fmt.Errorf("qq sender: media image complete failed: %w", err)
	}

	img := MediaImage{
		URL:        complete.Get("url").String(),
		ImageID:    complete.Get("image_id").String(),
		FileName:   fileName,
		TTL:        uint32(complete.Get("ttl").Uint()),
		PreviewURL: complete.Get("preview_url").String(),
	}
	if img.URL == "" {
		return MediaImage{}, fmt.Errorf(
			"qq sender: media image complete returned no url (response: %s)", complete.Raw)
	}
	return img, nil
}

// listMediaImages 查询素材列表并跟随 next_cursor 翻页。
func listMediaImages(ctx stdctx.Context, api openapi.MediaImageManager, limit int) ([]MediaImage, error) {
	var (
		out    []MediaImage
		cursor string
	)
	for {
		page, err := api.GetMediaImages(ctx, cursor, limit)
		if err != nil {
			return nil, fmt.Errorf("qq sender: list media images failed: %w", err)
		}
		for _, item := range page.Get("images").Array() {
			out = append(out, MediaImage{
				ImageID:    item.Get("image_id").String(),
				URL:        item.Get("url").String(),
				FileName:   item.Get("file_name").String(),
				PreviewURL: item.Get("preview_url").String(),
				CreatedAt:  item.Get("created_at").Int(),
				ExpiresAt:  item.Get("expires_at").Int(),
			})
		}
		next := page.Get("next_cursor").String()
		// 游标不前进时终止：服务端回环会把调用方卡在死循环里。
		if next == "" || next == cursor {
			return out, nil
		}
		cursor = next
	}
}

// deleteMediaImage 删除素材图片。
func deleteMediaImage(ctx stdctx.Context, api openapi.MediaImageManager, imageID string) error {
	if imageID == "" {
		return fmt.Errorf("qq sender: empty image id")
	}
	if _, err := api.DeleteMediaImage(ctx, imageID); err != nil {
		return fmt.Errorf("qq sender: delete media image failed: %w", err)
	}
	return nil
}

// materialImageType 决定素材图片的 MIME 类型与推荐扩展名。
// 优先二进制魔数嗅探，其次附件自带 MIME；不在平台白名单内时返回空串。
func materialImageType(att platform.Attachment) (contentType, ext string) {
	ct := sniffContentType(att.Data)
	if ct == "" {
		ct = strings.ToLower(strings.TrimSpace(att.MimeType))
	}
	// 归一化同义 MIME：平台只认 image/jpeg，不认 image/jpg。
	if ct == "image/jpg" {
		ct = "image/jpeg"
	}
	e, ok := materialAllowedTypes[ct]
	if !ok {
		return "", ""
	}
	return ct, e
}

// putPresignedImage 把图片二进制 PUT 到预签名 URL，返回响应的 ETag（已去引号）。
//
// 与富媒体分片上传的 putPresignedChunk 类似，但需要拿到 ETag：complete 阶段
// 平台要用它做一致性校验。COS/S3 的 ETag 带双引号，需去掉。
func putPresignedImage(ctx stdctx.Context, presignedURL, contentType string, data []byte) (string, error) {
	resp, err := httpclient.Put(presignedURL).
		SetContext(ctx).
		SetTimeout(chunkPutTimeout).
		SetHeader("Content-Type", contentType).
		SetBody(bytes.NewReader(data)).
		Do()
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := resp.Bytes()
		return "", fmt.Errorf("PUT presigned url status %d: %s", resp.StatusCode, truncateForLog(body))
	}
	return strings.Trim(resp.Header.Get("ETag"), `"`), nil
}
