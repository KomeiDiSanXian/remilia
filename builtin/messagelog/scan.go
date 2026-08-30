package messagelog

import "time"

// ScanAfter 按 (timestamp, id) 全序遍历 (afterTS, afterID) 之后落库的全部记录
// （不限定会话），逐条调用 fn 供派生数据消费者（统计插件）全量重建聚合。
//
// 与查询 API 不同，本方法面向「重建」而非「展示」：包含入站/出站/已撤回全部事实，
// 并按落库顺序升序遍历，保证同一批内 event 顺序与广播一致。Mentions / Attachments
// 一并补齐，使重建结果与事件订阅增量完全一致。
// afterTS / afterID 为零值时从最早记录开始（全量重建）。
func (l *Logger) ScanAfter(afterTS time.Time, afterID int64, fn func(RecordEntry) error) error {
	if l.db == nil {
		return nil
	}
	const batchSize = 500
	afterNanos := afterTS.UnixNano()
	lastID := afterID
	lastTS := afterNanos

	for {
		var models []MessageRecord
		err := l.db.Where("(timestamp > ?) OR (timestamp = ? AND id > ?)", lastTS, lastTS, lastID).
			Order("timestamp ASC, id ASC").Limit(batchSize).Find(&models).Error
		if err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}

		eventIDs := make([]string, len(models))
		for i, m := range models {
			eventIDs[i] = m.EventID
		}
		mentionsMap := l.loadMentions(eventIDs)
		attachmentsMap := l.loadAttachments(models)

		for _, m := range models {
			entry := modelToEntry(m, mentionsMap[m.EventID])
			entry.Attachments = attachmentsMap[m.EventID]
			if err := fn(entry); err != nil {
				return err
			}
			lastTS = m.Timestamp
			lastID = m.ID
		}

		if len(models) < batchSize {
			return nil
		}
	}
}
