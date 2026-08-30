// Package attachments 实现消息附件的存储与下载生命周期（best-effort 层）。
//
// 设计定位（附件生命周期，详见 CHANGELOG v1.47.0）：
//   - 元数据始终落库（message_attachments），二进制按近期消息窗口异步下载；
//   - 状态机：pending → downloading → ready；
//     pending_retry（5xx/超时指数退避）→ pending_lazy（4xx/放弃）→
//     ready / expired / failed / deleted；
//   - 二进制存放于 AttachmentStore（默认文件存储 data/attachments/<sha256>，
//     内容哈希去重，不落 SQLite / LevelDB）；
//   - 本层可靠性为 best-effort：下载失败/队列满/URL 过期/超限都可接受，
//     所有「丢」都必须显式降级状态并留下指标，不允许静默丢弃。
package attachments

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 附件状态（对应 message_attachments.status 列）。
const (
	// StatusPending 已决定下载、尚未开始。
	StatusPending = "pending"
	// StatusDownloading 下载中（瞬态；进程崩溃后由启动扫描按 pending_retry 处理）。
	StatusDownloading = "downloading"
	// StatusPendingRetry 暂时失败（5xx/超时/断连），指数退避后重试。
	StatusPendingRetry = "pending_retry"
	// StatusPendingLazy 当前无下载计划，仅在懒加载（Fetch）/空闲回填时下载。
	StatusPendingLazy = "pending_lazy"
	// StatusReady 二进制已就绪（storage_key 指向 Store 中的有效文件）。
	StatusReady = "ready"
	// StatusExpired URL 已过期/永久不可用，元数据保留。
	StatusExpired = "expired"
	// StatusFailed 下载失败且放弃，元数据保留。
	StatusFailed = "failed"
	// StatusDeleted 曾下载成功，后被 GC/磁盘预算策略删除。
	StatusDeleted = "deleted"
)

// Row message_attachments 表的局部模型。
//
// attachments 包独立读写同一张表（与父包 AttachmentRecord 共用表结构），
// 避免父包导入本包造成 import cycle；两处字段定义必须保持一致。
type Row struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	MessageID    int64  `gorm:"index"`
	EventID      string `gorm:"index"`
	Type         string
	URL          string
	Name         string
	MimeType     string
	Size         int64
	Width        int
	Height       int
	SHA256       string `gorm:"index"`
	StorageKey   string `gorm:"index"`
	Status       string `gorm:"index"`
	RetryCount   int
	LastError    string
	URLExpiresAt int64
	FetchedAt    int64
	CreatedAt    int64
}

// TableName 固定为 message_attachments。
func (Row) TableName() string { return "message_attachments" }

// Config 附件生命周期配置（由父包 messagelog.Options.Attachments 翻译而来）。
type Config struct {
	Dir              string          // 二进制存储目录
	Scope            string          // hot_window / none / all
	HotWindowAge     time.Duration   // scope=hot_window 时的时间窗口
	MaxDiskUsage     int64           // 附件磁盘预算（soft limit）
	MaxPending       int             // 待下载任务上限（内存队列）
	Concurrency      int             // 下载并发 worker 数
	Retries          int             // 临时失败重试次数（指数退避）
	Backoff          []time.Duration // 退避序列（按 retry_count 取档）
	MaxSize          int64           // 单文件硬上限
	RatePerHost      float64         // 按 host 每秒请求上限（0 = 不限）
	LazyFallback     bool            // 队列满时降级 pending_lazy（默认 true）
	GCEnabled        bool            // 引用计数 GC 开关
	GracePeriod      time.Duration   // 引用归零后的二进制宽限期
	BackfillEnabled  bool            // 空闲回填开关
	BackfillBatch    int             // 每批回填条数
	IdleThreshold    float64         // 队列占用率低于该值视为空闲
	BackfillAttempts int             // 回填对同一行的最大尝试次数
	BackfillInterval time.Duration   // 回填扫描周期
	GCInterval       time.Duration   // GC 周期
}

// InitialStatus 按下载范围策略决定附件的初始状态。
//   - scope=all：一律 pending（全部下载）；
//   - scope=hot_window：消息时间落在 hot_window_age 窗口内 → pending，否则 pending_lazy；
//   - scope=none / 其他：一律 pending_lazy。
func InitialStatus(scope string, hotWindow time.Duration, msgTime time.Time) string {
	switch scope {
	case "all":
		return StatusPending
	case "hot_window":
		if hotWindow > 0 && time.Since(msgTime) <= hotWindow {
			return StatusPending
		}
		return StatusPendingLazy
	default:
		return StatusPendingLazy
	}
}

// URLExpiryUnix 解析 URL 签名过期时间（Unix 秒；解析不出返回 0）。
func URLExpiryUnix(rawURL string) int64 {
	t := parseURLExpiry(rawURL)
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// permanentError 表示不应自动重试的下载错误（4xx 等）。
// gone=true 表示 URL 永久失效（404/410），状态降级 expired；
// 否则降级 pending_lazy（懒加载可再试）。
type permanentError struct {
	err  error
	gone bool
}

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// temporaryError 表示应走 pending_retry 退避的下载错误（5xx/超时/断连）。
type temporaryError struct{ err error }

func (e *temporaryError) Error() string { return e.err.Error() }
func (e *temporaryError) Unwrap() error { return e.err }

// errBudgetFull 磁盘预算已满（非显式下载暂停）；不消耗重试次数。
var errBudgetFull = errors.New("attachment disk budget full")

// ErrNotFound 附件行不存在。
var ErrNotFound = errors.New("attachment not found")

// ErrNotAvailable 附件二进制不可用（未下载/已过期/已删除）。
var ErrNotAvailable = errors.New("attachment not available")

// classifyHTTPError 按 HTTP 状态码分类下载错误。
//
//   - 401/403：权限类，降级 pending_lazy（可能临时，懒加载可再试）；
//   - 404/410：URL 失效，视为 expired；
//   - 其他 4xx：不自动重试，降级 pending_lazy；
//   - 5xx/其他：走 pending_retry 指数退避。
func classifyHTTPError(status int, statusText string) error {
	switch {
	case status == 401 || status == 403:
		return &permanentError{err: errors.New("attachment HTTP " + strconv.Itoa(status) + " " + statusText)}
	case status == 404 || status == 410:
		return &permanentError{err: errors.New("attachment URL gone HTTP " + strconv.Itoa(status)), gone: true}
	case status >= 400 && status < 500:
		return &permanentError{err: errors.New("attachment HTTP " + strconv.Itoa(status) + " " + statusText)}
	default:
		return &temporaryError{err: errors.New("attachment HTTP " + strconv.Itoa(status) + " " + statusText)}
	}
}

// classifyNetError 把传输层错误映射为临时错误（超时/断连可重试）。
func classifyNetError(err error) error {
	return &temporaryError{err: err}
}

// parseURLExpiry 从 URL 中解析显式过期时间（仅用于回填排序）。
//
// 只解析 URL 中明确携带的签名参数，不猜测：
//   - expires / x-expires / se / e（Unix 秒）；
//   - AWS S3：X-Amz-Date + X-Amz-Expires。
//
// 解析不出返回零值（unknown，回填排序时排最后，以真实下载失败为过期信号）。
func parseURLExpiry(rawURL string) time.Time {
	u, err := url.Parse(rawURL)
	if err != nil {
		return time.Time{}
	}
	q := u.Query()
	for _, key := range []string{"expires", "x-expires", "se", "e"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			if sec, err := strconv.ParseInt(v, 10, 64); err == nil && sec > 0 {
				return time.Unix(sec, 0)
			}
		}
	}
	if date := q.Get("X-Amz-Date"); date != "" {
		if t, err := time.Parse("20060102T150405Z", date); err == nil {
			if sec, err := strconv.ParseInt(q.Get("X-Amz-Expires"), 10, 64); err == nil && sec > 0 {
				return t.Add(time.Duration(sec) * time.Second)
			}
		}
	}
	return time.Time{}
}
