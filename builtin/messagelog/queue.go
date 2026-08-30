package messagelog

import (
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// queuedRecord 一条待落盘记录；spoolNext > 0 表示来自 spool，
// SQLite 提交成功后 ack 到该偏移。
type queuedRecord struct {
	job       RecordEntry
	spoolNext int64
}

// durableQueue 消息事实队列：有界内存 + spool 兜底（Durable，不主动丢）。
// 内存满 → spool；spool 满 → 阻塞生产者并告警（Durable 语义的显式权衡）。
type durableQueue struct {
	cap     int
	mu      sync.Mutex
	pending []RecordEntry
	spool   *spool // nil = 未启用 spool
	notify  chan struct{}
}

func newDurableQueue(cap int, sp *spool) *durableQueue {
	if cap <= 0 {
		cap = 50000
	}
	return &durableQueue{cap: cap, spool: sp, notify: make(chan struct{}, 1)}
}

// enqueue 入队。返回 false 表示未入队（未启用 spool 且内存满——指标计数）。
// 生产环境 spool 启用时恒返回 true（满时阻塞）。
func (q *durableQueue) enqueue(e RecordEntry) bool {
	q.mu.Lock()
	if len(q.pending) < q.cap {
		q.pending = append(q.pending, e)
		q.mu.Unlock()
		q.signal()
		return true
	}
	q.mu.Unlock()

	if q.spool == nil {
		mlMetricsInst.queueDroppedTotal.Inc()
		logger.Warn("[MessageLog] queue full and spool disabled, record dropped")
		return false
	}
	// spool 满/写失败：阻塞生产者并告警，直到可写入（不丢）
	for {
		err := q.spool.append(e)
		if err == nil {
			q.signal()
			return true
		}
		mlMetricsInst.spoolWriteFailures.Inc()
		logger.WithError(err).Warn("[MessageLog] spool unavailable, blocking producer")
		time.Sleep(100 * time.Millisecond)
	}
}

// drain 取出最多 n 条待处理记录（先内存后 spool）。不推进 spool cursor。
func (q *durableQueue) drain(n int) []queuedRecord {
	if n <= 0 {
		return nil
	}
	q.mu.Lock()
	var out []queuedRecord
	if len(q.pending) > 0 {
		take := min(n, len(q.pending))
		out = make([]queuedRecord, 0, take)
		for _, e := range q.pending[:take] {
			out = append(out, queuedRecord{job: e})
		}
		q.pending = q.pending[take:]
		n -= take
	}
	q.mu.Unlock()
	q.updateDepth()

	if n > 0 && q.spool != nil {
		recs, _ := q.spool.read(q.spool.cursor, n)
		for _, r := range recs {
			out = append(out, queuedRecord{job: r.entry, spoolNext: r.next})
		}
	}
	return out
}

// ack 在 SQLite 提交成功后推进 spool cursor（仅处理来自 spool 的记录）。
func (q *durableQueue) ack(recs []queuedRecord) {
	if q.spool == nil {
		return
	}
	var ackTo int64
	consumed := 0
	for _, r := range recs {
		if r.spoolNext > ackTo {
			ackTo = r.spoolNext
		}
		if r.spoolNext > 0 {
			consumed++
		}
	}
	if ackTo > 0 {
		if err := q.spool.ack(ackTo, consumed); err != nil {
			logger.WithError(err).Warn("[MessageLog] failed to advance spool cursor")
		}
		q.updateDepth()
	}
}

// requeue 在 SQLite 提交失败时，把来自内存的记录写入 spool 保底
// （来自 spool 的记录保持原位，cursor 未推进，下轮重读）。
func (q *durableQueue) requeue(recs []queuedRecord) {
	for _, r := range recs {
		if r.spoolNext > 0 {
			continue
		}
		if q.spool != nil {
			if err := q.spool.append(r.job); err != nil {
				mlMetricsInst.spoolWriteFailures.Inc()
				mlMetricsInst.queueDroppedTotal.Inc()
				logger.WithError(err).Error("[MessageLog] requeue to spool failed, record lost")
			}
		} else {
			mlMetricsInst.queueDroppedTotal.Inc()
		}
	}
	q.updateDepth()
}

// depth 返回内存队列长度（指标辅助）。
func (q *durableQueue) depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

func (q *durableQueue) updateDepth() {
	d := float64(q.depth())
	if q.spool != nil {
		d += float64(q.spool.unacked())
	}
	mlMetricsInst.queueDepth.Set(d)
}

// signal 非阻塞通知 flushLoop。
func (q *durableQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// close 释放 spool 文件句柄。
func (q *durableQueue) close() {
	if q.spool != nil {
		if err := q.spool.close(); err != nil {
			logger.WithError(err).Warn("[MessageLog] failed to close spool")
		}
	}
}
