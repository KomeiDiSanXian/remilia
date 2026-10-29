package dto

// 素材管理（2026-10-21 新增）。
//
// 开发者可先把 Markdown 消息所需的图片上传到平台，获得 qqbot-media:// 引用，
// 之后在 Markdown 中直接引用，无需依赖公网 URL。图片按滑动窗口过期：
// 每次被消息引用后有效期顺延，连续 90 天未被引用则失效。
//
// 注意：素材与机器人绑定，其他机器人不能引用（会返回 40035010）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/material-management.html

// MediaImagePrepareRequest 申请图片上传（POST /v2/media-images/prepare）。
//
// 上传任务（upload_id）有效期 4 小时，过期需重新申请。
type MediaImagePrepareRequest struct {
	// FileName 文件名（含扩展名，如 kline.png）。
	// 扩展名必须与 ContentType 一致，否则平台拒绝（40035001）。
	FileName string `json:"file_name"`
	// FileSize 文件大小（字节）。
	FileSize uint64 `json:"file_size"`
	// ContentType 图片 MIME 类型：image/png / image/jpeg / image/gif / image/webp。
	ContentType string `json:"content_type"`
	// MD5 文件内容 MD5（32 位十六进制）。
	MD5 string `json:"md5"`
	// SHA1 文件内容 SHA1（40 位十六进制）。
	SHA1 string `json:"sha1"`
}

// MediaImagePrepareResponse 申请图片上传的响应。
type MediaImagePrepareResponse struct {
	// UploadID 上传任务 ID（4 小时内有效）。
	UploadID string `json:"upload_id"`
	// PresignedURL 预签名上传 URL，用 HTTP PUT 直传图片二进制。
	// PUT 时的 Content-Type 必须与 prepare 声明的一致，否则 COS 返回 403。
	PresignedURL string `json:"presigned_url"`
	// ExpiresIn 预签名 URL 有效期（秒）。
	ExpiresIn uint32 `json:"expires_in"`
}

// MediaImageCompleteRequest 完成图片上传（POST /v2/media-images/complete）。
type MediaImageCompleteRequest struct {
	// UploadID prepare 返回的上传任务 ID。
	UploadID string `json:"upload_id"`
	// MD5 上传返回的 ETag（文件 MD5，32 位十六进制）。
	// 服务端会与 COS 对象实际 ETag 比对，不一致返回 40035003。
	MD5 string `json:"md5"`
}

// MediaImageCompleteResponse 完成图片上传的响应。
type MediaImageCompleteResponse struct {
	// ImageID 图片 ID，用于查询与删除。
	ImageID string `json:"image_id"`
	// URL Markdown 引用链接：qqbot-media://image/v1/<token>。
	URL string `json:"url"`
	// TTL 滑动有效期窗口（秒），默认 90 天。
	TTL uint32 `json:"ttl"`
	// PreviewURL 临时预览链接（短时效），仅供预览与自查，请勿持久化。
	PreviewURL string `json:"preview_url"`
}

// MediaImage 一张已上传的有效图片（查询列表的元素）。
type MediaImage struct {
	// ImageID 图片 ID。
	ImageID string `json:"image_id"`
	// URL Markdown 引用链接。
	URL string `json:"url"`
	// FileName 上传时的文件名。
	FileName string `json:"file_name"`
	// PreviewURL 临时预览链接（短时效）。
	PreviewURL string `json:"preview_url"`
	// CreatedAt 上传时间（Unix 秒）。
	CreatedAt int64 `json:"created_at"`
	// ExpiresAt 过期时间（Unix 秒），每次被引用后自动顺延。
	ExpiresAt int64 `json:"expires_at"`
}

// MediaImageList 查询有效图片的响应（GET /v2/media-images）。
type MediaImageList struct {
	// Images 当前页图片列表，按上传时间倒序。
	Images []MediaImage `json:"images"`
	// NextCursor 下一页游标，为空表示已到末页。
	NextCursor string `json:"next_cursor"`
	// Total 当前有效图片总数（与数量上限同一口径，默认上限 100 张）。
	Total uint64 `json:"total"`
}
