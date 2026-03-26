package qq

import "time"

// SendResult QQ 平台消息发送后的完整响应，存于 platform.SendResult.Raw。
//
// 普通消息（单聊/群聊/频道）仅有 MessageID 和 Timestamp，富媒体字段为零值。
// 富媒体两步发送时，上传阶段的响应（FileInfo、FileUUID、TTL）与
// 最终发送阶段的响应（MessageID、Timestamp）会合并到同一个结构体中。
//
// 用法示例：
//
//	result, err := ctx.Reply(msg)
//	if err != nil { ... }
//	if r, ok := result.Raw.(*qq.SendResult); ok {
//	    // 普通消息：使用 r.MessageID 撤回
//	    // 富媒体：使用 r.FileInfo 缓存，避免重复上传
//	    if r.FileInfo != "" {
//	        cache.Set(key, r.FileInfo, time.Duration(r.TTL)*time.Second)
//	    }
//	}
type SendResult struct {
	// MessageID 已发送消息的唯一 ID。
	// 用于调用 MessageDeleter.Delete 撤回消息。
	// 富媒体上传（srv_send_msg=false）场景不返回此 ID。
	MessageID string

	// Timestamp 平台确认的消息发送时间。
	Timestamp time.Time

	// FileUUID 富媒体文件的平台内部 ID（仅富媒体上传响应有效，普通消息为零值）。
	FileUUID string

	// FileInfo 富媒体文件 token，用于后续发送消息接口的 media 字段。
	// TTL > 0 时会在 TTL 秒后失效，需重新上传；TTL == 0 时永久有效。
	// 仅富媒体上传响应有效，普通消息为零值。
	FileInfo string

	// TTL FileInfo 的有效期（秒）。0 表示永久有效。
	// 仅富媒体上传响应有效，普通消息为零值。
	TTL int
}
