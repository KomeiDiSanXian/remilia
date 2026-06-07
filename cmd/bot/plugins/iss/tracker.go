package iss

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/kv"
)

const historyKey = "alt_history"

// AltRecord 一条轨道高度历史记录。
type AltRecord struct {
	Time     time.Time
	Altitude float64
}

// Tracker 提供 ISS 轨道高度的后台轮询和累积历史。
//
// 每 5 分钟轮询一次 wheretheiss.at 获取当前高度，
// 累积历史数据供高度趋势面积图使用。
type Tracker struct {
	mu      sync.Mutex
	records []AltRecord
	maxSize int
}

const defaultMaxRecords = 288

// NewTracker 创建一个新的高度历史追踪器。
func NewTracker() *Tracker {
	return &Tracker{maxSize: defaultMaxRecords}
}

// Start 启动后台轮询，定期获取 ISS 高度并追加到历史记录。
// onNewRecord 为可选的每次新记录回调（用于持久化等场景）。
// interval <= 0 时使用默认值 5 分钟。
func (t *Tracker) Start(ctx context.Context, interval time.Duration, onNewRecord func()) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			pos, err := fetchPosition(ctx)
			if err != nil {
				continue
			}
			t.mu.Lock()
			t.records = append(t.records, AltRecord{
				Time:     pos.Timestamp,
				Altitude: pos.Altitude,
			})
			if len(t.records) > t.maxSize {
				t.records = t.records[len(t.records)-t.maxSize:]
			}
			t.mu.Unlock()
			if onNewRecord != nil {
				onNewRecord()
			}
		case <-ctx.Done():
			return
		}
	}
}

// GetRecent 返回最近 n 条高度记录。n <= 0 或 >= 总量时返回全部。
func (t *Tracker) GetRecent(n int) []AltRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n <= 0 || n >= len(t.records) {
		out := make([]AltRecord, len(t.records))
		copy(out, t.records)
		return out
	}
	out := make([]AltRecord, n)
	copy(out, t.records[len(t.records)-n:])
	return out
}

// Count 返回当前历史记录数量。
func (t *Tracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.records)
}

// Records 返回所有历史记录的副本。
func (t *Tracker) Records() []AltRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]AltRecord, len(t.records))
	copy(out, t.records)
	return out
}

// SetRecords 替换全部历史记录（从持久化存储加载时使用）。
func (t *Tracker) SetRecords(records []AltRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(records) > t.maxSize {
		records = records[len(records)-t.maxSize:]
	}
	t.records = records
}

// Save 将历史记录序列化并写入 KV 存储。
func (t *Tracker) Save(store *kv.DB) error {
	records := t.Records()
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return store.Set([]byte(historyKey), data)
}

// Load 从 KV 存储加载历史记录。
func (t *Tracker) Load(store *kv.DB) error {
	data, err := store.Get([]byte(historyKey))
	if err != nil {
		if err == kv.ErrNotFound {
			return nil
		}
		return err
	}
	var records []AltRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	t.SetRecords(records)
	return nil
}

// MinMax 返回历史记录中的最小和最大高度。
func (t *Tracker) MinMax() (min, max float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.records) == 0 {
		return 0, 0
	}
	min = math.MaxFloat64
	max = -math.MaxFloat64
	for _, r := range t.records {
		if r.Altitude < min {
			min = r.Altitude
		}
		if r.Altitude > max {
			max = r.Altitude
		}
	}
	return
}
