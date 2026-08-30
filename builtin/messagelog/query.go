package messagelog

import (
	"context"
	"io"
	"slices"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog/attachments"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// Direction 查询方向过滤。
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
	DirectionBoth     Direction = "both"
)

// QueryOptions 查询选项。
//
// 零值语义：方向 = 全部（入站 + 出站）、排除已撤回、包含 pending 出站
// （pending 是「bot 尝试发送了什么」的事实，默认包含）。
type QueryOptions struct {
	Direction          Direction
	IncludeRecalled    bool // true = 包含已撤回的消息
	ExcludePending     bool // true = 排除 send_status=pending 的出站
	RequireAttachments bool // 仅返回带附件的消息
}

// matchQueryOpts 内存条目的查询过滤。
func matchQueryOpts(e RecordEntry, opts QueryOptions) bool {
	if opts.RequireAttachments && len(e.Attachments) == 0 {
		return false
	}
	switch opts.Direction {
	case DirectionInbound:
		if e.IsOutbound {
			return false
		}
	case DirectionOutbound:
		if !e.IsOutbound {
			return false
		}
	}
	if e.IsRecalled && !opts.IncludeRecalled {
		return false
	}
	if e.IsOutbound && e.SendStatus == SendStatusPending && opts.ExcludePending {
		return false
	}
	return true
}

// queryScope 将查询选项映射为 DB 过滤条件。
func queryScope(q *gorm.DB, opts QueryOptions) *gorm.DB {
	switch opts.Direction {
	case DirectionInbound:
		q = q.Where("is_outbound = ?", false)
	case DirectionOutbound:
		q = q.Where("is_outbound = ?", true)
	}
	if !opts.IncludeRecalled {
		q = q.Where("is_recalled = ?", false)
	}
	if opts.ExcludePending {
		q = q.Where("(send_status IS NULL OR send_status != ?)", string(SendStatusPending))
	}
	if opts.RequireAttachments {
		q = q.Where("EXISTS (SELECT 1 FROM message_attachments a WHERE a.event_id = message_records.event_id)")
	}
	return q
}

// QueryChat 返回 chatID 最近 n 条记录（旧→新）。
//
// 隐藏热缓存 / SQLite 边界：内存命中 + DB 补齐后统一按 (timestamp, event_id)
// 稳定排序并按 EventID 去重，调用方不感知缓存是否存在。
func (l *Logger) QueryChat(chatID string, n int, opts QueryOptions) []RecordEntry {
	if chatID == "" || n <= 0 {
		return nil
	}
	seen := make(map[string]bool)
	entries := make([]RecordEntry, 0, n)

	if l.cache != nil {
		entries = l.cache.collect(chatID, n, func(e RecordEntry) bool {
			return matchQueryOpts(e, opts)
		})
		entries = dedupKeepNewest(entries)
		for _, e := range entries {
			seen[e.EventID] = true
		}
	}

	if len(entries) < n && l.db != nil {
		q := queryScope(l.db.Where("chat_id = ?", chatID), opts)
		var models []MessageRecord
		if err := q.Order("timestamp DESC, id DESC").Limit(n).Find(&models).Error; err != nil {
			logger.WithError(err).Warn("[MessageLog] Failed to query recent messages from DB")
		} else {
			attachmentsMap := l.loadAttachments(models)
			for _, m := range slices.Backward(models) {
				if seen[m.EventID] {
					continue
				}
				seen[m.EventID] = true
				entry := modelToEntry(m, nil)
				entry.Attachments = attachmentsMap[m.EventID]
				entries = append(entries, entry)
			}
		}
	}

	sortEntries(entries)
	return lastN(entries, n)
}

// QueryRange 返回 chatID 在 [since, until] 时间窗内最近 limit 条记录（旧→新）。
// 缓存中未落库的条目同样参与合并，保证查询不遗漏异步 flush 间隙。
func (l *Logger) QueryRange(chatID string, since, until time.Time, limit int, opts QueryOptions) ([]RecordEntry, error) {
	if chatID == "" || limit <= 0 || since.After(until) {
		return nil, nil
	}
	seen := make(map[string]bool)
	var entries []RecordEntry

	if l.cache != nil {
		for _, e := range l.cache.snapshotAll(chatID) {
			if e.Timestamp.Before(since) || e.Timestamp.After(until) {
				continue
			}
			if !matchQueryOpts(e, opts) || seen[e.EventID] {
				continue
			}
			seen[e.EventID] = true
			entries = append(entries, e)
		}
	}

	if l.db != nil {
		q := queryScope(
			l.db.Where("chat_id = ? AND timestamp BETWEEN ? AND ?", chatID, since.UnixNano(), until.UnixNano()),
			opts,
		)
		var models []MessageRecord
		if err := q.Order("timestamp DESC, id DESC").Limit(limit).Find(&models).Error; err != nil {
			return nil, err
		}
		if len(models) > 0 {
			eventIDs := make([]string, len(models))
			for i, m := range models {
				eventIDs[i] = m.EventID
			}
			mentionsMap := l.loadMentions(eventIDs)
			attachmentsMap := l.loadAttachments(models)
			for _, m := range models {
				if seen[m.EventID] {
					continue
				}
				seen[m.EventID] = true
				entry := modelToEntry(m, mentionsMap[m.EventID])
				entry.Attachments = attachmentsMap[m.EventID]
				entries = append(entries, entry)
			}
		}
	}

	sortEntries(entries)
	return lastN(entries, limit), nil
}

// QueryByEventID 按逻辑身份（event_id）或平台消息 ID 在指定会话中查找消息。
//
// 查找顺序：热缓存 → SQLite 兜底。chatID 是安全/查询约束（防止跨会话访问），
// 非定位条件（event_id 全局唯一）。
func (l *Logger) QueryByEventID(chatID, eventID string) (RecordEntry, bool) {
	if chatID == "" || eventID == "" {
		return RecordEntry{}, false
	}
	if l.cache != nil {
		if e, ok := l.cache.find(chatID, eventID); ok {
			return e, true
		}
	}
	if l.db != nil {
		var m MessageRecord
		if err := l.db.Where(
			"chat_id = ? AND (event_id = ? OR platform_message_id = ?)",
			chatID, eventID, eventID,
		).First(&m).Error; err == nil {
			entry := modelToEntry(m, nil)
			if atts := l.loadAttachments([]MessageRecord{m}); len(atts) > 0 {
				entry.Attachments = atts[m.EventID]
			}
			return entry, true
		}
	}
	return RecordEntry{}, false
}

// Fetch 按附件行 ID 获取二进制内容（触发懒加载）。
//
//   - ready：直接打开存储文件；
//   - pending_lazy / pending_retry / pending：同步触发下载（显式请求可突破
//     磁盘预算，不突破单文件硬上限）；
//   - failed / expired / deleted：返回错误。
func (l *Logger) Fetch(attachmentID int64) (io.ReadCloser, error) {
	if l.att == nil || l.db == nil {
		return nil, attachments.ErrNotAvailable
	}
	return l.att.Fetch(context.Background(), attachmentID)
}

// AttachmentsReady 判断某条记录的附件是否全部可用（status=ready）。
func (l *Logger) AttachmentsReady(e RecordEntry) bool {
	if len(e.Attachments) == 0 {
		return false
	}
	for _, a := range e.Attachments {
		if a.Status != attachments.StatusReady {
			return false
		}
	}
	return true
}

// --- 旧 API 薄封装（AI 插件零改动） ---

// QueryGroup 返回群 groupID 最近 n 条入站消息（仅内存热缓存，与旧语义一致）。
func (l *Logger) QueryGroup(groupID string, n int) []RecordEntry {
	if groupID == "" || n <= 0 {
		return nil
	}
	var out []RecordEntry
	if l.cache != nil {
		out = l.cache.collect(groupID, n, func(e RecordEntry) bool { return !e.IsOutbound })
		out = dedupKeepNewest(out)
	}
	return out
}

// QueryGroupRecent 返回群 groupID 最近 n 条入站消息（旧→新）。
// 内存不足时以 SQLite 最近记录补齐，重启后仍能读到历史。
func (l *Logger) QueryGroupRecent(groupID string, n int) []RecordEntry {
	return l.QueryChat(groupID, n, QueryOptions{Direction: DirectionInbound})
}

// QueryGroupRecentWithBot 与 QueryGroupRecent 相同，但额外包含机器人的出站回复。
func (l *Logger) QueryGroupRecentWithBot(groupID string, n int) []RecordEntry {
	return l.QueryChat(groupID, n, QueryOptions{Direction: DirectionBoth})
}

// QueryUser 返回用户 userID 最近 n 条入站消息（旧→新）。
//
// 私聊场景 chat_id == user_id，优先走该会话热缓存；DB 兜底按 user_id 查询。
// 群聊内按用户检索不是该 API 的使用范围（历史数据由 QueryChat/QueryRange 提供）。
func (l *Logger) QueryUser(userID string, n int) []RecordEntry {
	if userID == "" || n <= 0 {
		return nil
	}
	seen := make(map[string]bool)
	var entries []RecordEntry

	if l.cache != nil {
		entries = l.cache.collect(userID, n, func(e RecordEntry) bool { return !e.IsOutbound })
		entries = dedupKeepNewest(entries)
		for _, e := range entries {
			seen[e.EventID] = true
		}
	}

	if len(entries) < n && l.db != nil {
		var models []MessageRecord
		if err := l.db.Where("user_id = ? AND is_outbound = ?", userID, false).
			Order("timestamp DESC, id DESC").Limit(n).Find(&models).Error; err != nil {
			logger.WithError(err).Warn("[MessageLog] Failed to query recent messages by user from DB")
		} else {
			for _, m := range slices.Backward(models) {
				if seen[m.EventID] {
					continue
				}
				seen[m.EventID] = true
				entries = append(entries, modelToEntry(m, nil))
			}
		}
	}

	sortEntries(entries)
	return lastN(entries, n)
}

// QueryGroupFromDB 返回群指定时间窗内的入站记录（最新在前，旧语义）。
func (l *Logger) QueryGroupFromDB(chatID string, since, until time.Time, limit int) ([]RecordEntry, error) {
	entries, err := l.QueryRange(chatID, since, until, limit, QueryOptions{Direction: DirectionInbound})
	if err != nil || len(entries) == 0 {
		return entries, err
	}
	slices.Reverse(entries)
	return entries, nil
}

// QueryUserFromDB 返回用户指定时间窗内的入站记录（最新在前，旧语义）。
func (l *Logger) QueryUserFromDB(userID string, since, until time.Time, limit int) ([]RecordEntry, error) {
	if l.db == nil || userID == "" || limit <= 0 {
		return nil, nil
	}
	var models []MessageRecord
	err := l.db.Where(
		"user_id = ? AND is_outbound = ? AND timestamp BETWEEN ? AND ?",
		userID, false, since.UnixNano(), until.UnixNano(),
	).Order("timestamp DESC, id DESC").Limit(limit).Find(&models).Error
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}
	eventIDs := make([]string, len(models))
	for i, m := range models {
		eventIDs[i] = m.EventID
	}
	mentionsMap := l.loadMentions(eventIDs)
	attachmentsMap := l.loadAttachments(models)
	out := make([]RecordEntry, len(models))
	for i, m := range models {
		out[i] = modelToEntry(m, mentionsMap[m.EventID])
		out[i].Attachments = attachmentsMap[m.EventID]
	}
	return out, nil
}

// sortEntries 按 (timestamp, event_id) 稳定排序（旧→新）。
// event_id 为 UUIDv7，天然时间有序，作为平台时间戳相同场景的 tie-breaker。
func sortEntries(entries []RecordEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Timestamp.Equal(entries[j].Timestamp) {
			return entries[i].EventID < entries[j].EventID
		}
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})
}

// lastN 截取切片末尾 n 条（不足 n 条时原样返回）。
func lastN(entries []RecordEntry, n int) []RecordEntry {
	if len(entries) > n {
		return entries[len(entries)-n:]
	}
	return entries
}

// dedupKeepNewest 同 EventID 多条（如 pending + 完成记录）时保留最新一条。
// 输入为旧→新；完成记录晚于 pending，因此从尾部去重保留最新状态。
func dedupKeepNewest(entries []RecordEntry) []RecordEntry {
	if len(entries) <= 1 {
		return entries
	}
	seen := make(map[string]bool, len(entries))
	out := make([]RecordEntry, 0, len(entries))
	for _, e := range slices.Backward(entries) {
		if seen[e.EventID] {
			continue
		}
		seen[e.EventID] = true
		out = append(out, e)
	}
	// out 为倒序（最新在前），反转回旧→新
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
