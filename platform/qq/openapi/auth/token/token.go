package token

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/httpclient"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/constant"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

const (
	// tokenRequestTimeout 是单次获取 access token 的 HTTP 超时。
	tokenRequestTimeout = 10 * time.Second

	// minRefreshInterval 是两次刷新之间的最小间隔，用于兜底异常的
	// expires_in（缺失、为 0、非数字），避免退化成零延迟热循环。
	minRefreshInterval = 30 * time.Second

	// defaultWaitReadyTimeout 是等待 token 就绪的默认上限。
	defaultWaitReadyTimeout = 30 * time.Second
)

// Manager 管理 access token
//
// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/api-use.html#%E8%8E%B7%E5%8F%96%E8%B0%83%E7%94%A8%E5%87%AD%E8%AF%81
type Manager struct {
	mu sync.Mutex
	// readyCh 在 token 首次就绪或 Manager 停止时关闭。
	//
	// 用 channel 而非 sync.Cond 做等待，是为了让 WaitReady* 变成一次纯 select，
	// 不再为每个调用方起一个 goroutine：调用方普遍带有几秒的 deadline，
	// 一旦 token 长期取不到（例如 AppSecret 配错），它们会不断超时重试，
	// 每次都留下一个永远停在 cond.Wait() 上的 goroutine，无上限累积。
	readyCh chan struct{}
	// readyOnce 保证 readyCh 只被关闭一次。
	readyOnce   sync.Once
	accessToken string
	expiresAt   time.Time
	ready       bool
	stopped     atomic.Bool // 标记 Manager 是否已停止

	// 停止控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup // 用于等待 goroutine 退出

	// 配置
	retryDelay      time.Duration // Token 获取失败重试延迟
	refreshAdvance  time.Duration // 提前多久刷新 Token
	minRefreshRatio float64       // 最小刷新时间比例
}

// NewManager 初始化并启动后台刷新（使用默认配置，context.Background() 作为根 context）
func NewManager(info *dto.BotInfo) *Manager {
	return NewManagerWithContext(context.Background(), info)
}

// NewManagerWithContext 创建与外部 context 联动的 Manager（默认 token 配置）。
//
// 当 parent ctx 被取消时（如 Bot 关闭），后台 token 刷新 goroutine 将自动退出，
// 无需手动调用 Stop()。
//
// 推荐在 Bot 生命周期中使用：
//
//	mgr := token.NewManagerWithContext(bot.Context(), info)
func NewManagerWithContext(parent context.Context, info *dto.BotInfo) *Manager {
	return NewManagerFromConfigWithContext(parent, info, config.TokenManagerConfig{
		RetryDelay:      "10s",
		RefreshAdvance:  "30s",
		MinRefreshRatio: 0.5,
	})
}

// NewManagerFromConfig 使用指定配置初始化并启动后台刷新（context.Background() 作为根 context）
//
// 参数:
//   - info: Bot 信息
//   - cfg: Token 配置
//
// 示例:
//
//	cfg, _ := config.LoadDefault()
//	mgr := token.NewManagerFromConfig(global.Info, cfg.Token)
func NewManagerFromConfig(info *dto.BotInfo, cfg config.TokenManagerConfig) *Manager {
	return NewManagerFromConfigWithContext(context.Background(), info, cfg)
}

// NewManagerFromConfigWithContext 创建与外部 context 联动的 Manager（自定义配置）。
//
// 当 parent ctx 被取消时，后台刷新 goroutine 自动退出，无需手动调用 Stop()。
func NewManagerFromConfigWithContext(parent context.Context, info *dto.BotInfo, cfg config.TokenManagerConfig) *Manager {
	logger.Debugf("[Token] Initializing Token Manager with config: %+v", cfg)
	// 不可打印整个 BotInfo：其中的 AppSecret 是长期凭据，
	// 一旦开启 debug 日志就会被永久留存到日志文件/采集系统中。
	if info != nil {
		logger.Debugf("[Token] Bot Info: QQNum=%d AppID=%d", info.QQNum, info.AppID)
	}
	ctx, cancel := context.WithCancel(parent)

	// 解析配置
	retryDelay := 10 * time.Second
	if cfg.RetryDelay != "" {
		if d, err := time.ParseDuration(cfg.RetryDelay); err == nil {
			retryDelay = d
		} else {
			logger.WithError(err).Warn("[Token] Invalid retry_delay config, using default 10s")
		}
	}

	refreshAdvance := 30 * time.Second
	if cfg.RefreshAdvance != "" {
		if d, err := time.ParseDuration(cfg.RefreshAdvance); err == nil {
			refreshAdvance = d
		} else {
			logger.WithError(err).Warn("[Token] Invalid refresh_advance config, using default 30s")
		}
	}

	minRefreshRatio := 0.5
	if cfg.MinRefreshRatio > 0 && cfg.MinRefreshRatio <= 1 {
		minRefreshRatio = cfg.MinRefreshRatio
	}

	logger.Infof("[Token] Config: retry_delay=%v, refresh_advance=%v, min_refresh_ratio=%.2f",
		retryDelay, refreshAdvance, minRefreshRatio)

	m := &Manager{
		ctx:             ctx,
		cancel:          cancel,
		retryDelay:      retryDelay,
		refreshAdvance:  refreshAdvance,
		minRefreshRatio: minRefreshRatio,
	}
	m.readyCh = make(chan struct{})
	m.wg.Add(1)
	go m.autoRefresh(info)
	return m
}

// Stop 停止 token 自动刷新
// 调用此方法后，Manager 将不再刷新 token
// 此方法是幂等的，多次调用安全
func (m *Manager) Stop() {
	if m.stopped.Swap(true) {
		return // 已经停止
	}

	if m.cancel != nil {
		m.cancel()
	}

	// 唤醒所有等待者：关闭 readyCh 让阻塞中的 WaitReady* 立即返回。
	// 缺少这一步时，token 始终取不到的部署（例如 AppSecret 配错）
	// 会让每个等待者一直挂到进程退出。
	m.signalReady()

	// 添加超时保护，防止永久阻塞
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("[Token] Token manager stopped")
	case <-time.After(5 * time.Second):
		logger.Warn("[Token] Token manager stop timeout, some goroutines may still be running")
	}
}

// WaitReady 阻塞直到 access token 可用
// 使用默认超时时间（30秒）
func (m *Manager) WaitReady() error {
	return m.WaitReadyWithTimeout(defaultWaitReadyTimeout)
}

// signalReady 关闭 readyCh，唤醒所有等待者（幂等）。
func (m *Manager) signalReady() {
	m.readyOnce.Do(func() {
		close(m.readyCh)
	})
}

// WaitReadyWithContext 阻塞直到 access token 可用、调用方 ctx 结束或超时。
//
// 相比 WaitReadyWithTimeout，它额外监听调用方的 ctx：
// API 调用方普遍会给自己设置几秒的 deadline，而固定 30 秒的等待完全无视它，
// 一次"token 尚未就绪"就能让调用方的超时被违反十几倍，
// 并在此期间每个在途请求各占住一个 goroutine。
func (m *Manager) WaitReadyWithContext(ctx context.Context) error {
	if m.stopped.Load() {
		return fmt.Errorf("token manager has been stopped")
	}

	timer := time.NewTimer(defaultWaitReadyTimeout)
	defer timer.Stop()

	select {
	case <-m.readyCh:
		// readyCh 在"首次就绪"和"Stop"两种情况下都会关闭，需区分。
		if !m.Ready() {
			return fmt.Errorf("token manager stopped")
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("wait ready timeout after %v", defaultWaitReadyTimeout)
	case <-ctx.Done():
		return fmt.Errorf("wait ready canceled: %w", ctx.Err())
	case <-m.ctx.Done():
		return fmt.Errorf("token manager stopped")
	}
}

// WaitReadyWithTimeout 阻塞直到 access token 可用或超时。
func (m *Manager) WaitReadyWithTimeout(timeout time.Duration) error {
	if m.stopped.Load() {
		return fmt.Errorf("token manager has been stopped")
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-m.readyCh:
		if !m.Ready() {
			return fmt.Errorf("token manager stopped")
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("wait ready timeout after %v", timeout)
	case <-m.ctx.Done():
		return fmt.Errorf("token manager stopped")
	}
}

// Ready 返回是否已成功获取到 access token（非阻塞）。
func (m *Manager) Ready() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ready
}

// TokenExpiresAt 返回当前 token 的过期时间。未就绪时返回零值 time.Time。
func (m *Manager) TokenExpiresAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.expiresAt
}

// GetToken 获取当前的 access token
// 如果 Manager 已停止，返回空字符串
// 调用者应该检查返回的 token 是否为空
func (m *Manager) GetToken() string {
	// 快速检查是否已停止（无锁）
	if m.stopped.Load() {
		logger.Warn("[Token] GetToken called after manager stopped")
		return ""
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查：锁内再次验证 ready 状态
	if !m.ready {
		logger.Warn("[Token] GetToken called but token not ready")
		return ""
	}

	// 可选：检查 token 是否即将过期（提供更好的调试信息）
	if time.Now().After(m.expiresAt) {
		logger.Warn("[Token] GetToken returning expired token")
	}

	return m.accessToken
}

func (m *Manager) autoRefresh(info *dto.BotInfo) {
	defer m.wg.Done()

	var firstSuccess bool

	for {
		// 检查是否需要停止
		select {
		case <-m.ctx.Done():
			logger.Info("[Token] Auto refresh stopped")
			return
		default:
		}

		resp, err := requestToken(info)
		if err != nil {
			logger.WithError(err).Warn("[Token] Failed to get access token")

			// 失败后等待一段时间再重试，并检查停止信号
			select {
			case <-m.ctx.Done():
				logger.Info("[Token] Auto refresh stopped during retry wait")
				return
			case <-time.After(m.retryDelay): // 使用配置的重试延迟
				continue
			}
		}

		m.mu.Lock()
		m.accessToken = resp.Get("access_token").Str
		expiresIn := resp.Get("expires_in").Int()
		m.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
		// 首次成功获取到 token 后，设置 ready 并唤醒等待者
		notifyReady := false
		if !firstSuccess {
			m.ready = true
			firstSuccess = true
			notifyReady = true
		}
		tokenLen := len(m.accessToken)
		m.mu.Unlock()

		// 先解锁再唤醒：close(readyCh) 提供 happens-before 边，
		// 保证被唤醒者随后经 Ready() 读到的一定是已置位的 m.ready。
		if notifyReady {
			m.signalReady()
		}
		// 只记录 token 长度而非内容：access_token 是可直接调用 API 的活凭据，
		// 每次刷新（约 2 小时）都写一遍等于把凭据持续泄露到日志中。
		// 长度在锁内取，避免与其他 goroutine 的写入竞争。
		logger.WithField("token_len", tokenLen).WithField("expires_in", expiresIn).Debug("[Token] Updated")

		// 计算刷新时间，使用配置的 refreshAdvance
		refreshAfter := time.Duration(expiresIn)*time.Second - m.refreshAdvance
		if refreshAfter <= 0 {
			// 如果 expires_in 太小，则使用最小刷新比例
			refreshAfter = time.Duration(float64(expiresIn) * m.minRefreshRatio * float64(time.Second))
		}
		// 下限保护：expires_in 缺失/为 0/非数字时（gjson 一律折算成 0），
		// 上面两个表达式都会得到 <=0，time.After(0) 立即触发，
		// 形成不带任何退避的取 token 热循环——持续向 bots.qq.com 发送携带
		// appId+clientSecret 的请求，跑满 CPU 并刷爆日志，直至被限流封禁。
		if refreshAfter < minRefreshInterval {
			clamped := minRefreshInterval
			// 下限不得越过 token 自身的有效期：expires_in 很短时，
			// 无条件抬到 30s 反而会把刷新排到过期之后。
			if half := time.Duration(expiresIn) * time.Second / 2; expiresIn > 0 && half < clamped {
				clamped = half
			}
			if clamped <= 0 {
				clamped = minRefreshInterval
			}
			logger.Warnf("[Token] Computed refresh interval %v is too small (expires_in=%ds), clamping to %v",
				refreshAfter, expiresIn, clamped)
			refreshAfter = clamped
		}

		logger.Debugf("[Token] Next refresh in %v (expires_in=%ds, advance=%v)",
			refreshAfter, expiresIn, m.refreshAdvance)

		// 等待刷新时间或停止信号
		select {
		case <-m.ctx.Done():
			logger.Info("[Token] Auto refresh stopped during refresh wait")
			return
		case <-time.After(refreshAfter):
			// 继续下一次刷新
		}
	}

}

func requestToken(info *dto.BotInfo) (gjson.Result, error) {
	bodyMap := map[string]string{
		"appId":        strconv.FormatUint(info.AppID, 10),
		"clientSecret": info.AppSecret,
	}
	// 必须显式设置超时：httpclient 的默认 client 未设置 http.Client.Timeout，
	// 默认请求 context 也是 Background，即这次调用原本**没有任何截止时间**。
	// 对端建连成功却不回包（黑洞连接 / 半开 LB）时，autoRefresh 会永久卡在
	// 这里：token 过期后不再刷新，所有 API 401 且无自愈路径，
	// 同时 Stop() 的 wg.Wait() 也永远等不到该 goroutine 退出。
	result, err := httpclient.Post(constant.AccessTokenURL).
		SetTimeout(tokenRequestTimeout).
		SetJSON(bodyMap).
		DoJSON()
	if err != nil {
		return gjson.Result{}, err
	}
	if result.Get("access_token").Exists() {
		return result, nil
	}
	return gjson.Result{}, fmt.Errorf("invalid token response: %s", result.Raw)
}
