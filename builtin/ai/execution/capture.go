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

// Merge 把另一次调用捕获到的文本与附件并入本发送器（线程安全，nil 安全）。
//
// 命令通道为每次调用使用独立的捕获器（避免共享捕获器的文本串味），执行结束后
// 用本方法把局部结果汇总回回合级发送器：文本仅在非空时覆盖（保留"最近一次
// 非空回复"的兜底语义），附件按调用顺序累积（随最终回复发送）。
func (s *CaptureSender) Merge(other *CaptureSender) {
	if s == nil || other == nil {
		return
	}
	text, atts := other.snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	if text != "" {
		s.CapturedText = text
	}
	if len(atts) > 0 {
		s.CapturedAttachments = append(s.CapturedAttachments, atts...)
	}
}

// snapshot 返回当前捕获内容的副本（线程安全）。
func (s *CaptureSender) snapshot() (string, []platform.Attachment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CapturedText, append([]platform.Attachment(nil), s.CapturedAttachments...)
}
