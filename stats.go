package remilia

import "time"

// BatchStats 批量处理统计信息
type BatchStats struct {
	TotalBatches    uint64        // 总批次数
	TotalEvents     uint64        // 总事件数
	TotalDuration   time.Duration // 总耗时
	AvgBatchSize    float64       // 平均批量大小
	AvgDuration     time.Duration // 平均每批次耗时
	EventsPerSecond float64       // 吞吐量（事件/秒）
}
