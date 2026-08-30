package messagelog

import "uuid"

// Record 统一事实入口：分配 event_id（UUIDv7）、写热缓存、入队持久化。
// 入站/出站共用；EventID 为空时自动分配，返回最终 event_id。
//
// 入队：内存队列满时走 durable spool（不丢）；未 Start 时静默跳过（测试场景）。
// 热缓存：按会话写入同一有界缓存，查询时按方向过滤。
func (l *Logger) Record(e RecordEntry) string {
	if e.EventID == "" {
		e.EventID = uuid.NewV7().String()
	}
	if l.queue != nil {
		l.queue.enqueue(e)
	}
	if l.cache != nil {
		l.cache.add(e.ChatID, e)
	}
	return e.EventID
}
