// attachment.go — 附件二进制内存缓存，以及"先发图再发字"的图片合并窗口。
package session

import (
	"time"
)

// CachedContent 内存中缓存的附件二进制数据（按 URL 索引，限本轮会话有效）。
type CachedContent struct {
	Data        []byte
	MimeType    string
	AudioFormat string
	ExpireAt    time.Time
}

// PendingImageRef 未消费纯图片消息的引用（不持有二进制）。
// 消费时经 messagelog 附件存储解析为多模态 ContentPart（URL 过期免疫）；
// 引用持久化于会话记录，重启后窗口内 follow-up 仍可合并。
type PendingImageRef struct {
	ChatID        string // 会话 ID（消费时解析 messagelog）
	PlatformMsgID string // 平台消息 ID（= messagelog platform_message_id）
	URL           string // 直链兜底（存储未落库/不可用时回退下载）
	MimeType      string // 直链兜底时的类型提示
}

// PendingImageState 未消费的纯图片消息（合并窗口状态）。
type PendingImageState struct {
	Parts     []PendingImageRef // 待合并的图片引用（不持有二进制）
	Extra     int               // 超出二进制保留上限的额外图片数（仅计数，不持有二进制）
	Timestamp time.Time         // 最近一张图片的到达时间
}

// ExtendPendingImage 追加图片到当前合并窗口；无有效窗口或窗口已过期时新建。
// 纯图片消息本身不回复，仅记录等待后续文字合并（表情包防误触发）。
// maxKeep 限制 pending 持有的二进制图片数（超出部分仅计数，不下载/持有），
// 防止群里连发表情包导致内存无界增长；合并时用计数补足总数判断超限。
func (s *Session) ExtendPendingImage(refs []PendingImageRef, at time.Time, window time.Duration, maxKeep int) {
	if len(refs) == 0 {
		return
	}
	s.Lock()
	defer s.Unlock()
	if PendingImageWithin(s.pendingImage, window, at) {
		keep := min(maxKeep-len(s.pendingImage.Parts), len(refs))
		if keep > 0 {
			s.pendingImage.Parts = append(s.pendingImage.Parts, refs[:keep]...)
		}
		s.pendingImage.Extra += len(refs) - keep
		s.pendingImage.Timestamp = at
		return
	}
	keep := min(maxKeep, len(refs))
	s.pendingImage = &PendingImageState{
		Parts:     append([]PendingImageRef(nil), refs[:keep]...),
		Extra:     len(refs) - keep,
		Timestamp: at,
	}
}

// ConsumePendingImage 返回并清除窗口内的未消费图片；窗口不存在或已过期返回 nil。
// 返回值 (parts, extra)：parts 为持有的二进制图片，extra 为仅计数的额外图片数。
// 每次处理真实回合时调用：无论是否合并，pending 都被消费（防止跨回合误合并）。
func (s *Session) ConsumePendingImage(window time.Duration, now time.Time) ([]PendingImageRef, int) {
	s.Lock()
	defer s.Unlock()
	p := s.pendingImage
	s.pendingImage = nil
	if !PendingImageWithin(p, window, now) {
		return nil, 0
	}
	return p.Parts, p.Extra
}

// MarkImageOverflowNotified 标记已提示过图片数量超限；返回是否为首次提示。
func (s *Session) MarkImageOverflowNotified() bool {
	s.Lock()
	defer s.Unlock()
	if s.imageOverflowNotified {
		return false
	}
	s.imageOverflowNotified = true
	return true
}

// PendingImageWithin 判断 pending 是否处于合并窗口内。
// 同时持有 Part 与 Extra 的空窗口（maxKeep<=0 时仅计数）同样有效：
// 此前 len(Parts)==0 即判 false，会把"仅计数"的窗口整体作废，
// 超限反馈与重置标记都不会触发。
func PendingImageWithin(p *PendingImageState, window time.Duration, now time.Time) bool {
	if p == nil || (len(p.Parts) == 0 && p.Extra == 0) {
		return false
	}
	if window <= 0 || p.Timestamp.IsZero() {
		return true
	}
	return now.Sub(p.Timestamp) <= window
}

// MaxContentCacheEntries 附件内容缓存的条目上限。
// base64 图片常驻内存：多模态会话反复处理图片时若无上限，10 分钟 TTL 内
// 不相交的 URL 永不清理，会成为活跃群的内存尖峰来源。超上限时先清过期项，
// 仍满则按插入序淘汰最旧的条目（map 遍历序不保证，但这是纯缓存，可接受）。
const MaxContentCacheEntries = 32

// CachedContent 从内存缓存中获取附件内容。已过期或不存在返回 nil。
// 线程安全，持有 session 读锁。
func (s *Session) CachedContent(url string) *CachedContent {
	s.Lock()
	defer s.Unlock()
	if s.contentCache == nil {
		return nil
	}
	c, ok := s.contentCache[url]
	if !ok {
		return nil
	}
	if time.Now().After(c.ExpireAt) {
		delete(s.contentCache, url)
		return nil
	}
	return c
}

// SetCachedContent 将附件内容存入内存缓存，TTL 默认 10 分钟。
// 线程安全，持有 session 写锁。条目数有硬上限（见 MaxContentCacheEntries）：
// 写入前先清扫过期项，仍满则淘汰最早过期的条目，防止长跑会话内存无界增长。
func (s *Session) SetCachedContent(url string, data []byte, mimeType, audioFormat string) {
	s.Lock()
	defer s.Unlock()
	if s.contentCache == nil {
		s.contentCache = make(map[string]*CachedContent)
	}
	s.sweepContentCacheLocked()
	for len(s.contentCache) >= MaxContentCacheEntries {
		if !s.evictOldestContentLocked() {
			break
		}
	}
	s.contentCache[url] = &CachedContent{
		Data:        data,
		MimeType:    mimeType,
		AudioFormat: audioFormat,
		ExpireAt:    time.Now().Add(10 * time.Minute),
	}
}

// sweepContentCacheLocked 清除全部已过期条目（须持会话写锁）。
func (s *Session) sweepContentCacheLocked() {
	if len(s.contentCache) == 0 {
		return
	}
	now := time.Now()
	for url, c := range s.contentCache {
		if now.After(c.ExpireAt) {
			delete(s.contentCache, url)
		}
	}
}

// evictOldestContentLocked 淘汰一个最早过期的条目（须持会话写锁）。
// 没有条目可淘汰时返回 false（防御空 map）。
func (s *Session) evictOldestContentLocked() bool {
	var oldestURL string
	var oldest time.Time
	found := false
	for url, c := range s.contentCache {
		if !found || c.ExpireAt.Before(oldest) {
			oldestURL, oldest, found = url, c.ExpireAt, true
		}
	}
	if !found {
		return false
	}
	delete(s.contentCache, oldestURL)
	return true
}
