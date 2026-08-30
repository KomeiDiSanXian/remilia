package attachments

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"gorm.io/gorm"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// downloadTimeout 单次下载整体超时。
const downloadTimeout = 30 * time.Second

// Downloader 执行附件下载：限速 → GET → 流式写 Store → 更新状态机。
type Downloader struct {
	db     *gorm.DB
	store  Store
	cfg    Config
	client *http.Client
	budget *diskBudget

	hostMu    sync.Mutex
	hostLimit map[string]*rate.Limiter
}

// NewDownloader 创建下载器。
func NewDownloader(db *gorm.DB, store Store, cfg Config, budget *diskBudget) *Downloader {
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        64,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	return &Downloader{
		db:        db,
		store:     store,
		cfg:       cfg,
		client:    client,
		budget:    budget,
		hostLimit: make(map[string]*rate.Limiter),
	}
}

// limiterFor 返回 host 对应的限速器（按配置速率懒创建）。
func (d *Downloader) limiterFor(host string) *rate.Limiter {
	d.hostMu.Lock()
	defer d.hostMu.Unlock()
	if l, ok := d.hostLimit[host]; ok {
		return l
	}
	var l *rate.Limiter
	if d.cfg.RatePerHost > 0 {
		l = rate.NewLimiter(rate.Limit(d.cfg.RatePerHost), 1)
	} else {
		l = rate.NewLimiter(rate.Inf, 0)
	}
	d.hostLimit[host] = l
	return l
}

// downloadOne 下载附件行并完整更新状态机。explicit=true 表示用户显式 Fetch：
// 可突破磁盘预算限制（仍受单文件硬上限）。
//
// 返回 (是否已就绪, 下次重试时间, 错误)：
//   - ready=true：二进制已可用；
//   - retryAt 非零：临时失败（5xx/超时/预算满），建议在该时间后重试；
//   - err 为 permanentError：4xx/无来源/超限，行已被降级（expired/pending_lazy）；
//   - ErrNotFound：行不存在。
func (d *Downloader) downloadOne(ctx context.Context, id int64, explicit bool) (bool, time.Time, error) {
	var row Row
	if err := d.db.First(&row, id).Error; err != nil {
		return false, time.Time{}, ErrNotFound
	}
	if row.Status == StatusReady {
		return true, time.Time{}, nil
	}
	if row.URL == "" {
		// 无来源（出站 Data 附件元数据落库后未随二进制写入的兜底）
		d.fail(row, StatusPendingLazy, "no download source")
		return false, time.Time{}, &permanentError{err: fmt.Errorf("attachment %d has no URL", id)}
	}

	// 磁盘预算：非显式下载在预算满时暂停
	if !explicit && d.budget != nil && d.budget.overHard() {
		return false, time.Now().Add(30 * time.Second), errBudgetFull
	}

	// 置为 downloading（瞬态）
	if err := d.db.Model(&Row{}).Where("id = ?", id).
		Updates(map[string]any{"status": StatusDownloading, "last_error": ""}).Error; err != nil {
		logger.WithError(err).Warn("[attachments] failed to mark downloading")
	}

	u, err := url.Parse(row.URL)
	if err != nil {
		d.fail(row, StatusPendingLazy, "invalid URL: "+truncateErr(err, 256))
		return false, time.Time{}, &permanentError{err: err}
	}
	lim := d.limiterFor(u.Host)
	if err := lim.Wait(ctx); err != nil {
		return false, time.Time{}, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, row.URL, nil)
	if err != nil {
		d.fail(row, StatusPendingLazy, "build request: "+truncateErr(err, 256))
		return false, time.Time{}, &permanentError{err: err}
	}
	resp, err := d.client.Do(req)
	if err != nil {
		// 传输层错误（超时/断连）→ 临时错误
		metricsInst.downloadsFailedTotal.Inc()
		ce := classifyNetError(err)
		return false, d.finishFailure(row, ce), ce
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		metricsInst.downloadsFailedTotal.Inc()
		cerr := classifyHTTPError(resp.StatusCode, resp.Status)
		return false, d.finishFailure(row, cerr), cerr
	}

	// 单文件硬上限：优先用 Content-Length 快速拒绝
	maxSize := d.cfg.MaxSize
	if maxSize > 0 {
		if cl := resp.ContentLength; cl > maxSize {
			d.fail(row, StatusPendingLazy, fmt.Sprintf("attachment too large (%d bytes)", cl))
			return false, time.Time{}, &permanentError{err: ErrTooLarge}
		}
	}

	key, size, tmp, err := d.store.PutTemp(resp.Body, maxSize)
	if err != nil {
		metricsInst.downloadsFailedTotal.Inc()
		if errors.Is(err, ErrTooLarge) {
			d.fail(row, StatusPendingLazy, "attachment too large")
			return false, time.Time{}, &permanentError{err: err}
		}
		ce := classifyNetError(err)
		return false, d.finishFailure(row, ce), ce
	}

	// 两阶段提交：rename 临时文件与标记 ready 必须在同一 SQLite 写锁事务内，
	// 与 GC 的 deleteIfUnreferenced（BEGIN IMMEDIATE）串行化，杜绝
	// 「文件可见后、引用行就绪前」被回收的悬空窗口（内容去重复用时
	// 目标文件可能因旧引用删除而成为孤儿，必须在锁内重新建立）。
	sqlDB, err := d.db.DB()
	if err != nil {
		_ = d.store.DiscardTemp(tmp)
		ce := classifyNetError(err)
		return false, d.finishFailure(row, ce), ce
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		_ = d.store.DiscardTemp(tmp)
		ce := classifyNetError(err)
		return false, d.finishFailure(row, ce), ce
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = d.store.DiscardTemp(tmp)
		ce := classifyNetError(err)
		return false, d.finishFailure(row, ce), ce
	}
	// 失败路径先显式回滚释放写锁，再走 finishFailure（后者经 d.db 写行状态，
	// 若锁未释放会自锁至 busy_timeout）。
	rollback := func() {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
	}

	if err := d.store.CommitTemp(key, tmp); err != nil {
		rollback()
		metricsInst.downloadsFailedTotal.Inc()
		ce := classifyNetError(err)
		return false, d.finishFailure(row, ce), ce
	}
	now := time.Now().Unix()
	if _, err := conn.ExecContext(ctx,
		"UPDATE message_attachments SET status = ?, storage_key = ?, sha256 = ?, size = ?, fetched_at = ?, retry_count = 0, last_error = '' WHERE id = ?",
		StatusReady, key, key, size, now, id); err != nil {
		rollback()
		metricsInst.downloadsFailedTotal.Inc()
		ce := classifyNetError(err)
		return false, d.finishFailure(row, ce), ce
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		rollback()
		metricsInst.downloadsFailedTotal.Inc()
		ce := classifyNetError(err)
		return false, d.finishFailure(row, ce), ce
	}
	metricsInst.downloadsReadyTotal.Inc()
	metricsInst.downloadsTotal.Inc()
	if d.budget != nil {
		d.budget.refresh(metricsInst)
	}
	return true, time.Time{}, nil
}

// finishFailure 把失败错误落到行状态机：
//   - permanentError：expired（URL 永久失效）或 pending_lazy（权限/其他 4xx/无来源）；
//   - temporaryError：pending_retry + 指数退避，返回下次重试时间；
//   - errBudgetFull：不修改状态，返回 30s 后重试。
func (d *Downloader) finishFailure(row Row, err error) time.Time {
	switch e := err.(type) {
	case *permanentError:
		if e.gone {
			d.fail(row, StatusExpired, e.Error())
		} else {
			d.fail(row, StatusPendingLazy, e.Error())
		}
		return time.Time{}
	case *temporaryError:
		if next, ok := d.scheduleRetry(row, e); ok {
			return next
		}
		return time.Time{}
	default:
		return time.Time{}
	}
}

// fail 更新行状态与错误摘要（不增加 retry_count）。
func (d *Downloader) fail(row Row, status, lastError string) {
	if err := d.db.Model(&Row{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status":     status,
		"last_error": truncateErrString(lastError),
	}).Error; err != nil {
		logger.WithError(err).Warn("[attachments] failed to update status")
	}
}

// scheduleRetry 临时失败：更新为 pending_retry 并递增重试次数，返回下次重试时间。
// 超过最大重试次数时降级 pending_lazy（放弃自动重试，仅懒加载/回填）。
func (d *Downloader) scheduleRetry(row Row, err error) (time.Time, bool) {
	retryCount := row.RetryCount + 1
	maxRetries := max(d.cfg.Retries, 0)
	if retryCount > maxRetries {
		d.fail(row, StatusPendingLazy, "retries exhausted: "+truncateErrString(err.Error()))
		return time.Time{}, false
	}
	backoff := retryBackoff(d.cfg.Backoff, retryCount)
	next := time.Now().Add(backoff)
	if dbErr := d.db.Model(&Row{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status":      StatusPendingRetry,
		"retry_count": retryCount,
		"last_error":  truncateErrString(err.Error()),
	}).Error; dbErr != nil {
		logger.WithError(dbErr).Warn("[attachments] failed to mark pending_retry")
	}
	return next, true
}

// retryBackoff 按重试次数取退避档位（超出序列取最后一档）。
func retryBackoff(seq []time.Duration, retryCount int) time.Duration {
	if len(seq) == 0 {
		return 30 * time.Second
	}
	idx := retryCount - 1
	if idx >= len(seq) {
		idx = len(seq) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return seq[idx]
}

// truncateErrString 截断错误摘要。
func truncateErrString(s string) string {
	const maxLen = 512
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// truncateErr 截断 error 的字符串形式。
func truncateErr(err error, n int) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
