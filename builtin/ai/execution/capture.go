// capture.go — 拦截命令 handler 输出的发送器。

package execution

import (
	"context"
	"sync"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// CaptureSender 实现 platform.Sender，拦截 Send 调用并记录消息文本内容和附件。
// 命令 handler 的回复仅作为工具结果回填给 AI（不转发给真实用户），
// 同时捕获生成的附件。
//
// 捕获结果以导出字段暴露，因为读侧在别的包（回合运行时按"命令是否真的回复了"
// 决定结果回填）。写入只发生在 Send 内，读侧按只读使用即可。
type CaptureSender struct {
	platform.NoopSender

	// CapturedText 最近一次非空回复的文本（Message.Text 优先，Markdown 兜底）。
	CapturedText string
	// CapturedAttachments 按发送顺序累积的附件。
	CapturedAttachments []platform.Attachment

	mu sync.Mutex
}

// Send 记录回复文本与附件，但不真正投递给用户。
func (s *CaptureSender) Send(_ context.Context, req platform.SendRequest) (platform.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text := req.Message.Text
	if text == "" {
		text = req.Message.Markdown
	}
	if text != "" {
		s.CapturedText = text
	}
	if len(req.Message.Attachments) > 0 {
		s.CapturedAttachments = append(s.CapturedAttachments, req.Message.Attachments...)
	}
	return platform.SendResult{}, nil
}
