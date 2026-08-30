package attachments

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// diskBudget 附件磁盘预算（soft limit，分级响应）：
//   - usage < 80%：正常；
//   - 80% ~ 100%：仅监控（指标 + 日志）；
//   - >= 100%：停止低优先级回填并触发 GC；GC 后仍超限则停止所有非显式下载。
type diskBudget struct {
	mu        sync.Mutex
	max       int64
	soft      int64 // 80% 阈值
	store     Store
	triggerGC chan struct{}
}

func newDiskBudget(max int64, store Store, triggerGC chan struct{}) *diskBudget {
	return &diskBudget{
		max:       max,
		soft:      int64(float64(max) * 0.8),
		store:     store,
		triggerGC: triggerGC,
	}
}

func (b *diskBudget) usage() int64 {
	if b == nil || b.store == nil {
		return 0
	}
	return b.store.TotalSize()
}

// overSoft 是否超过 80% 软阈值（停止回填）。
func (b *diskBudget) overSoft() bool {
	if b == nil || b.max <= 0 {
		return false
	}
	return b.usage() >= b.soft
}

// overHard 是否超过硬预算（停止非显式下载，并触发 GC）。
func (b *diskBudget) overHard() bool {
	if b == nil || b.max <= 0 {
		return false
	}
	over := b.usage() >= b.max
	if over && b.triggerGC != nil {
		select {
		case b.triggerGC <- struct{}{}:
		default:
		}
	}
	return over
}

// refresh 更新磁盘指标。
func (b *diskBudget) refresh(m *attMetrics) {
	if m == nil || b == nil {
		return
	}
	m.diskUsageBytes.Set(float64(b.usage()))
}

// Manager 附件生命周期管理器：有界任务队列 + 下载 worker + 重试调度 +
// 空闲回填 + 引用计数 GC。
type Manager struct {
	db        *gorm.DB
	store     Store
	cfg       Config
	dl        *Downloader
	budget    *diskBudget
	queue     chan int64
	triggerGC chan struct{}

	active  int64 // 在途下载数（原子）
	started bool
	wg      sync.WaitGroup
	mu      sync.Mutex
	retryAt map[int64]time.Time // id → 下次重试时间（pending_retry 调度）
}

// NewManager 创建附件管理器（不启动，需调用 Start）。
// db 为 nil 时仅提供 store 能力（Fetch/GC 不可用）。
func NewManager(db *gorm.DB, cfg Config) (*Manager, error) {
	if cfg.Dir == "" {
		return nil, errors.New("attachments dir is empty")
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = 10000
	}
	if cfg.BackfillBatch <= 0 {
		cfg.BackfillBatch = 50
	}
	if cfg.BackfillInterval <= 0 {
		cfg.BackfillInterval = 60 * time.Second
	}
	if cfg.GCInterval <= 0 {
		cfg.GCInterval = 30 * time.Minute
	}
	store, err := NewFileStore(cfg.Dir)
	if err != nil {
		return nil, err
	}
	trigger := make(chan struct{}, 1)
	m := &Manager{
		db:        db,
		store:     store,
		cfg:       cfg,
		queue:     make(chan int64, cfg.MaxPending),
		triggerGC: trigger,
		retryAt:   make(map[int64]time.Time),
	}
	m.budget = newDiskBudget(cfg.MaxDiskUsage, store, trigger)
	m.dl = NewDownloader(db, store, cfg, m.budget)
	return m, nil
}

// Store 返回底层存储（供父包写入出站 Data 附件）。
func (m *Manager) Store() Store { return m.store }

// Start 启动 worker 与后台循环，并扫描恢复未完成任务。
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()

	for range m.cfg.Concurrency {
		m.wg.Go(func() {
			m.worker(ctx)
		})
	}
	m.wg.Go(func() {
		m.retryLoop(ctx)
	})
	if m.cfg.BackfillEnabled {
		m.wg.Go(func() {
			m.backfillLoop(ctx)
		})
	}
	if m.cfg.GCEnabled {
		m.wg.Go(func() {
			m.gcLoop(ctx)
		})
	}
	// 启动扫描：恢复 pending / pending_retry（上次运行遗留或提交后未入队的任务）
	m.resumePending(ctx)
	m.budget.refresh(metricsInst)
}

// Stop 等待全部后台 goroutine 退出（父包 Stop 时调用；ctx 取消后应快速返回）。
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.wg.Wait()
}

// Enqueue 入队附件下载任务（非阻塞）。队列满时：
//   - LazyFallback=true：将行降级 pending_lazy（best-effort，懒加载再取）；
//   - LazyFallback=false：丢弃任务并计数（显式设计决策，不静默）。
func (m *Manager) Enqueue(ids []int64) {
	if m == nil || m.queue == nil {
		return
	}
	for _, id := range ids {
		select {
		case m.queue <- id:
		default:
			// 队列满：降级
			if m.cfg.LazyFallback && m.db != nil {
				if err := m.db.Model(&Row{}).Where("id = ? AND status = ?", id, StatusPending).
					Update("status", StatusPendingLazy).Error; err != nil {
					logger.WithError(err).Warn("[attachments] failed to degrade pending task")
				}
			}
			metricsInst.queueDepth.Set(float64(len(m.queue)))
		}
	}
	metricsInst.queueDepth.Set(float64(len(m.queue)))
}

// TriggerGC 请求一轮附件 GC（非阻塞）。
func (m *Manager) TriggerGC() {
	if m == nil || m.triggerGC == nil {
		return
	}
	select {
	case m.triggerGC <- struct{}{}:
	default:
	}
}

// Fetch 显式获取附件二进制（懒加载）。
// 未就绪（pending_lazy/pending_retry/pending/expired 可再试）时同步触发下载。
func (m *Manager) Fetch(ctx context.Context, id int64) (io.ReadCloser, error) {
	if m == nil || m.db == nil {
		return nil, ErrNotAvailable
	}
	var row Row
	if err := m.db.First(&row, id).Error; err != nil {
		return nil, ErrNotFound
	}
	if row.Status == StatusReady && row.StorageKey != "" {
		rc, err := m.store.Open(row.StorageKey)
		if err == nil {
			return rc, nil
		}
		// 文件丢失（GC race/外部删除）：重新下载
	}
	switch row.Status {
	case StatusFailed:
		return nil, ErrNotAvailable
	case StatusExpired:
		return nil, ErrNotAvailable
	case StatusDeleted:
		return nil, ErrNotAvailable
	}
	// pending_lazy / pending_retry / pending / downloading（文件缺失）→ 显式同步下载
	ok, _, err := m.dl.downloadOne(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotAvailable
	}
	// 重新读取行：下载完成后 storage_key 已更新
	var fresh Row
	if err := m.db.First(&fresh, id).Error; err != nil {
		return nil, err
	}
	return m.store.Open(fresh.StorageKey)
}

// worker 下载 worker：从队列取任务执行；临时失败调度退避重试。
func (m *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-m.queue:
			m.process(ctx, id, false)
		}
	}
}

// process 处理单个下载任务。
func (m *Manager) process(ctx context.Context, id int64, explicit bool) {
	m.mu.Lock()
	m.active++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.active--
		m.mu.Unlock()
	}()

	var row Row
	if err := m.db.First(&row, id).Error; err != nil {
		return // 行已被删除（retention）
	}
	// 仅处理可下载状态（startup 恢复时可能有重复入队）
	if row.Status != StatusPending && row.Status != StatusPendingRetry && row.Status != StatusPendingLazy {
		return
	}

	_, retryAt, err := m.dl.downloadOne(ctx, id, explicit)
	if err == nil {
		return
	}
	if errors.Is(err, ErrNotFound) {
		return // 行已被删除（retention）
	}
	if !retryAt.IsZero() {
		// 临时失败（5xx/超时/预算满）：退避后重入队
		m.scheduleDelay(id, time.Until(retryAt))
	}
	// 永久失败（4xx/无来源/超限）已在 downloadOne 内完成状态降级，无需调度
}

// scheduleDelay 把任务加入内存退避表（重试/预算恢复），由 retryLoop 到期重入队。
func (m *Manager) scheduleDelay(id int64, delay time.Duration) {
	if delay <= 0 {
		delay = time.Second
	}
	m.mu.Lock()
	m.retryAt[id] = time.Now().Add(delay)
	m.mu.Unlock()
}

// retryLoop 每秒检查退避表，到期任务重新入队（pending_retry 由 worker 再验证）。
func (m *Manager) retryLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			m.mu.Lock()
			var due []int64
			for id, at := range m.retryAt {
				if !at.After(now) {
					due = append(due, id)
					delete(m.retryAt, id)
				}
			}
			m.mu.Unlock()
			for _, id := range due {
				select {
				case m.queue <- id:
				default:
					// 队列又满：重新延迟
					m.scheduleDelay(id, 30*time.Second)
				}
			}
		}
	}
}

// backfillLoop 空闲回填：队列占用率低于阈值时，按 url_expires_at 升序
// 补下载 pending_lazy 行（优先救快过期的 URL）。
func (m *Manager) backfillLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.BackfillInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !m.idle() || m.budget.overSoft() {
				continue
			}
			rows := m.pickLazy(m.cfg.BackfillBatch)
			m.Enqueue(idsOf(rows))
		}
	}
}

// idle 判断内部队列是否空闲（占用率低于 idle_threshold）。
func (m *Manager) idle() bool {
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	queued := int64(len(m.queue))
	cap := int64(m.cfg.MaxPending)
	if cap <= 0 {
		cap = 1
	}
	threshold := m.cfg.IdleThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = 0.3
	}
	return float64(queued+active)/float64(cap) < threshold
}

// pickLazy 选取待回填行：pending_lazy 且重试次数未超限，按 url_expires_at 升序
// （NULL 排最后）。
func (m *Manager) pickLazy(limit int) []Row {
	if m.db == nil || limit <= 0 {
		return nil
	}
	var rows []Row
	err := m.db.Where("status = ? AND retry_count < ?", StatusPendingLazy, m.cfg.BackfillAttempts).
		Order("url_expires_at ASC NULLS LAST, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		logger.WithError(err).Warn("[attachments] backfill pick failed")
		return nil
	}
	return rows
}

// resumePending 启动扫描：把 pending / pending_retry 行重新入队。
func (m *Manager) resumePending(ctx context.Context) {
	if m.db == nil {
		return
	}
	var rows []Row
	if err := m.db.Where("status IN ?", []string{StatusPending, StatusPendingRetry}).
		Order("id ASC").Limit(m.cfg.MaxPending).Find(&rows).Error; err != nil {
		logger.WithError(err).Warn("[attachments] resume scan failed")
		return
	}
	if len(rows) == 0 {
		return
	}
	m.Enqueue(idsOf(rows))
	// 恢复完成后立即刷新一次状态指标
	m.refreshStatusMetrics()
}

// refreshStatusMetrics 统计各状态行数（低频：启动/GC/回填后）。
func (m *Manager) refreshStatusMetrics() {
	if m.db == nil {
		return
	}
	for status, g := range map[string]prometheus.Gauge{
		StatusPending:      metricsInst.pendingGauge,
		StatusPendingRetry: metricsInst.retryGauge,
		StatusPendingLazy:  metricsInst.lazyGauge,
		StatusReady:        metricsInst.readyGauge,
		StatusFailed:       metricsInst.failedGauge,
		StatusExpired:      metricsInst.expiredGauge,
		StatusDeleted:      metricsInst.deletedGauge,
	} {
		var n int64
		if err := m.db.Model(&Row{}).Where("status = ?", status).Count(&n).Error; err == nil {
			g.Set(float64(n))
		}
	}
}

func idsOf(rows []Row) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}
