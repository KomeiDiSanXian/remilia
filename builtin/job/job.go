package job

import (
	stdctx "context"
	"time"
)

// ID 作业唯一标识（字符串，方便与外部系统集成）。
type ID string

// Status 作业当前状态。
type Status int

const (
	// StatusPending 已提交，等待执行（包括延迟等待期）。
	StatusPending Status = iota
	// StatusRunning 正在执行（或正在进行重试间等待）。
	StatusRunning
	// StatusDone 成功完成。
	StatusDone
	// StatusFailed 所有尝试均失败（最终失败状态）。
	StatusFailed
	// StatusCanceled 被 Cancel 取消。
	StatusCanceled
)

// String 返回状态的可读字符串。
func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusRunning:
		return "running"
	case StatusDone:
		return "done"
	case StatusFailed:
		return "failed"
	case StatusCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// Func 作业执行函数签名。ctx 用于取消/超时传递。
type Func func(ctx stdctx.Context) error

// Info 作业状态快照（不可变）。
type Info struct {
	// ID 作业唯一标识
	ID ID
	// Name 作业名称（调用者设置，用于日志/调试）
	Name string
	// Status 当前状态
	Status Status
	// Attempts 已尝试次数（首次执行为 1）
	Attempts int
	// LastError 最近一次失败的错误（成功完成时为 nil）
	LastError error
	// SubmittedAt 提交时间
	SubmittedAt time.Time
	// StartedAt 首次开始执行时间（仍在 Pending 时为零值）
	StartedAt time.Time
	// FinishedAt 最终完成/失败/取消时间（仍在运行时为零值）
	FinishedAt time.Time
}

// BackoffFunc 退避策略函数：根据已失败次数返回下次重试前的等待时间。
// attempt 从 1 开始（第一次失败后返回第一次退避时间）。
type BackoffFunc func(attempt int) time.Duration

// FixedBackoff 返回固定等待时间的退避策略。
func FixedBackoff(d time.Duration) BackoffFunc {
	return func(_ int) time.Duration { return d }
}

// ExponentialBackoff 返回指数退避策略：base * 2^(attempt-1)，上限为 max。
func ExponentialBackoff(base, max time.Duration) BackoffFunc {
	return func(attempt int) time.Duration {
		d := base
		for i := 1; i < attempt; i++ {
			d *= 2
			if d > max {
				return max
			}
		}
		return d
	}
}

// ─── 配置 ─────────────────────────────────────────────────────────────────

// jobConfig 内部作业配置。
type jobConfig struct {
	delay      time.Duration
	timeout    time.Duration
	maxRetries int
	backoff    BackoffFunc
	onDone     func(Info)
}

// Option 配置作业行为的函数式选项。
type Option func(*jobConfig)

// WithDelay 设置作业首次执行前的延迟时间（默认立即执行）。
func WithDelay(d time.Duration) Option {
	return func(c *jobConfig) { c.delay = d }
}

// WithTimeout 设置单次执行的超时时间（默认无超时）。
// 对重试作业，超时限制每次尝试，而非整体链路。
func WithTimeout(t time.Duration) Option {
	return func(c *jobConfig) { c.timeout = t }
}

// WithMaxRetries 设置最大重试次数（默认 0，即不重试）。
// 总执行次数 = 1（首次）+ MaxRetries。
func WithMaxRetries(n int) Option {
	return func(c *jobConfig) { c.maxRetries = n }
}

// WithFixedBackoff 设置固定间隔退避策略。
func WithFixedBackoff(d time.Duration) Option {
	return func(c *jobConfig) { c.backoff = FixedBackoff(d) }
}

// WithExponentialBackoff 设置指数退避策略（base 为初始等待，max 为上限）。
func WithExponentialBackoff(base, max time.Duration) Option {
	return func(c *jobConfig) { c.backoff = ExponentialBackoff(base, max) }
}

// WithBackoff 设置自定义退避策略。
func WithBackoff(fn BackoffFunc) Option {
	return func(c *jobConfig) { c.backoff = fn }
}

// WithOnDone 设置作业完成回调（无论成功、失败或取消均触发）。
// 回调在作业运行 goroutine 中调用，请勿在回调内长时间阻塞。
func WithOnDone(fn func(Info)) Option {
	return func(c *jobConfig) { c.onDone = fn }
}

func defaultConfig() jobConfig {
	return jobConfig{
		maxRetries: 0,
		backoff:    FixedBackoff(time.Second),
	}
}
