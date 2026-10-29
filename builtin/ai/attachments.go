// attachments.go — 会话历史只存附件引用，请求构建时按引用补齐二进制。
//
// 设计目标：`session.Messages` 里永远不出现图片/音频的字节。历史消息只保留
// `ContentPart{Type, MimeType, SourceURL, AttachmentID}`，字节放在会话级缓存
// （有字节预算，见 session.MaxContentCacheBytes）或按引用重新取回。
//
// 补齐的时机是"每回合构建请求副本时"，只作用于副本：不会写回会话，工具轮复用
// 同一份副本，回合结束副本释放。这样"历史里有几张图"与常驻内存彻底解耦。

package ai

import (
	"context"
	"net/http"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// 附件无法按引用取回时的文本占位（写进请求副本，让模型知道这里曾有附件）。
const (
	unavailableImagePlaceholder = "[历史图片已不可用]"
	unavailableAudioPlaceholder = "[历史音频已不可用]"
)

// 补齐来源（同时是 metric 的 source 标签值）。
const (
	hydrateSourceCache   = "cache"
	hydrateSourceStorage = "storage"
	hydrateSourceURL     = "url"
)

// hydrateAttachmentParts 为请求副本补齐附件二进制，返回 (补齐数, 不可用数)。
//
// 取值顺序：会话缓存 → 本机附件存储（AttachmentID）→ 原始 URL 重下。
// 3 个来源都失败时把该片段降级为文本占位，避免"模型看不到图也不知道有图"。
// ctx 应为回合上下文（受 /ai stop 与超时约束）。
func (p *Plugin) hydrateAttachmentParts(ctx context.Context, msgs []protocol.Message, sess *session.Session) (filled, unavailable int) {
	if sess == nil {
		return 0, 0
	}
	for i := range msgs {
		msg := &msgs[i]
		if len(msg.ContentParts) == 0 || !needsHydration(msg.ContentParts) {
			continue
		}
		var (
			rebuilt  []protocol.ContentPart
			modified bool
		)
		for _, part := range msg.ContentParts {
			if part.Type == protocol.ContentPartText || len(part.Data) > 0 {
				rebuilt = append(rebuilt, part)
				continue
			}

			hydrated, source := p.hydrateAttachmentPart(ctx, part, sess)
			if hydrated != nil {
				rebuilt = append(rebuilt, *hydrated)
				modified = true
				filled++
				recordAttachmentHydration(source)
				continue
			}

			// 取不回：降级为文本占位（保留"这里曾有附件"的语义）。
			rebuilt = append(rebuilt, protocol.ContentPart{
				Type: protocol.ContentPartText,
				Text: unavailablePlaceholder(part.Type),
			})
			modified = true
			unavailable++
			recordAttachmentUnavailable(attachmentKindLabel(part.Type))
			logger.Debugf("[AI] attachment unavailable for session %s (source=%q attachment_id=%d): %s",
				sess.ID, part.SourceURL, part.AttachmentID, msgSummary(msg))
		}
		if modified {
			msg.ContentParts = rebuilt
		}
	}
	return filled, unavailable
}

// needsHydration 判断这条消息是否还有未补齐二进制的媒体片段。
// 纯文本消息与已补齐的消息直接跳过，避免每轮工具调用都重建 parts 切片。
func needsHydration(parts []protocol.ContentPart) bool {
	for _, part := range parts {
		if part.Type != protocol.ContentPartText && len(part.Data) == 0 {
			return true
		}
	}
	return false
}

// hydrateAttachmentPart 按引用取回单个附件的二进制（命中缓存时零 I/O）。
// 返回补齐后的片段与来源标签；取不回返回 (nil, "")。
func (p *Plugin) hydrateAttachmentPart(ctx context.Context, part protocol.ContentPart, sess *session.Session) (*protocol.ContentPart, string) {
	// 1. 会话缓存（按 SourceURL 索引，与下载路径共用）。
	if part.SourceURL != "" {
		if cached := sess.CachedContent(part.SourceURL); cached != nil {
			return cachedPartFrom(part, cached.Data, cached.MimeType, cached.AudioFormat), hydrateSourceCache
		}
	}

	// 2. 本机附件存储：重启后、缓存过期后唯一稳定的来源。
	if part.AttachmentID > 0 {
		if data, ok := p.readStoredAttachment(ctx, part.AttachmentID); ok {
			mimeType := part.MimeType
			if mimeType == "" {
				mimeType = http.DetectContentType(data)
			}
			p.cachePartBytes(sess, part.SourceURL, data, mimeType, part.AudioFormat)
			return cachedPartFrom(part, data, mimeType, part.AudioFormat), hydrateSourceStorage
		}
	}

	// 3. 原始 URL 重下（平台直链可能已过期，作为最后兜底）。
	if part.SourceURL != "" {
		if data, mimeType, ok := p.downloadAttachmentBytes(part.SourceURL, part.MimeType); ok {
			audioFormat := part.AudioFormat
			if audioFormat == "" {
				audioFormat = runtime.InferAudioFormat(mimeType)
			}
			p.cachePartBytes(sess, part.SourceURL, data, mimeType, audioFormat)
			return cachedPartFrom(part, data, mimeType, audioFormat), hydrateSourceURL
		}
	}
	return nil, ""
}

// cachePartBytes 把补齐的字节写回会话缓存（受字节预算约束，evict 由缓存负责）。
func (p *Plugin) cachePartBytes(sess *session.Session, sourceURL string, data []byte, mimeType, audioFormat string) {
	if sess == nil || sourceURL == "" {
		return
	}
	sess.SetCachedContentWithBudget(sourceURL, data, mimeType, audioFormat, p.imageCacheBudget())
}

// cachedPartFrom 用补到的字节填充片段副本，保留原有的引用信息。
func cachedPartFrom(part protocol.ContentPart, data []byte, mimeType, audioFormat string) *protocol.ContentPart {
	out := part
	out.Data = data
	if mimeType != "" {
		out.MimeType = mimeType
	}
	if audioFormat != "" {
		out.AudioFormat = audioFormat
	}
	return &out
}

// unavailablePlaceholder 返回附件不可用时的文本占位。
func unavailablePlaceholder(kind protocol.ContentPartType) string {
	if kind == protocol.ContentPartAudio {
		return unavailableAudioPlaceholder
	}
	return unavailableImagePlaceholder
}

// attachmentKindLabel 返回 metric 用的附件类型标签。
func attachmentKindLabel(kind protocol.ContentPartType) string {
	if kind == protocol.ContentPartAudio {
		return "audio"
	}
	return "image"
}

// msgSummary 为日志提供"这条消息在说什么"的最小上下文（不含附件内容）。
func msgSummary(msg *protocol.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Content != "" {
		r := []rune(msg.Content)
		if len(r) > 40 {
			r = r[:40]
		}
		return string(r)
	}
	return string(msg.Role)
}
