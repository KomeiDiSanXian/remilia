package token

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"

	"github.com/KomeiDiSanXian/remilia/httpcilent"
	"github.com/KomeiDiSanXian/remilia/openapi/constant"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// Manager 管理 access token
//
// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/api-use.html#%E8%8E%B7%E5%8F%96%E8%B0%83%E7%94%A8%E5%87%AD%E8%AF%81
type Manager struct {
	mu          sync.Mutex
	cond        *sync.Cond
	accessToken string
	expiresAt   time.Time
	ready       bool

	// 停止控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup // 用于等待 goroutine 退出

	// 配置
	retryDelay      time.Duration // Token 获取失败重试延迟
	refreshAdvance  time.Duration // 提前多久刷新 Token
	minRefreshRatio float64       // 最小刷新时间比例
}

// NewManager 初始化并启动后台刷新（使用默认配置）
func NewManager(info *dto.BotInfo) *Manager {
	return NewManagerFromConfig(info, config.TokenConfig{
		RetryDelay:      "10s",
		RefreshAdvance:  "30s",
		MinRefreshRatio: 0.5,
	})
}

// NewManagerFromConfig 使用指定配置初始化并启动后台刷新
//
// 参数:
//   - info: Bot 信息
//   - cfg: Token 配置
//
// 示例:
//
//	cfg, _ := config.LoadDefault()
//	mgr := token.NewManagerFromConfig(global.Info, cfg.Token)
func NewManagerFromConfig(info *dto.BotInfo, cfg config.TokenConfig) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	// 解析配置
	retryDelay := 10 * time.Second
	if cfg.RetryDelay != "" {
		if d, err := time.ParseDuration(cfg.RetryDelay); err == nil {
			retryDelay = d
		} else {
			logrus.WithError(err).Warn("[Token] Invalid retry_delay config, using default 10s")
		}
	}

	refreshAdvance := 30 * time.Second
	if cfg.RefreshAdvance != "" {
		if d, err := time.ParseDuration(cfg.RefreshAdvance); err == nil {
			refreshAdvance = d
		} else {
			logrus.WithError(err).Warn("[Token] Invalid refresh_advance config, using default 30s")
		}
	}

	minRefreshRatio := 0.5
	if cfg.MinRefreshRatio > 0 && cfg.MinRefreshRatio <= 1 {
		minRefreshRatio = cfg.MinRefreshRatio
	}

	logrus.Infof("[Token] Config: retry_delay=%v, refresh_advance=%v, min_refresh_ratio=%.2f",
		retryDelay, refreshAdvance, minRefreshRatio)

	m := &Manager{
		ctx:             ctx,
		cancel:          cancel,
		retryDelay:      retryDelay,
		refreshAdvance:  refreshAdvance,
		minRefreshRatio: minRefreshRatio,
	}
	m.cond = sync.NewCond(&m.mu)
	m.wg.Add(1)
	go m.autoRefresh(info)
	return m
}

// Stop 停止 token 自动刷新
// 调用此方法后，Manager 将不再刷新 token
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait() // 等待 goroutine 退出
	logrus.Info("[Token] Token manager stopped")
}

// WaitReady 阻塞直到 access token 可用
func (m *Manager) WaitReady() {
	m.mu.Lock()
	for !m.ready {
		m.cond.Wait() // 等到ready为true时被唤醒
	}
	m.mu.Unlock()
}

// GetToken 获取当前的 access token
func (m *Manager) GetToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accessToken
}

func (m *Manager) autoRefresh(info *dto.BotInfo) {
	defer m.wg.Done()

	var firstSuccess bool

	for {
		// 检查是否需要停止
		select {
		case <-m.ctx.Done():
			logrus.Info("[Token] Auto refresh stopped")
			return
		default:
		}

		resp, err := requestToken(info)
		if err != nil {
			logrus.WithError(err).Warn("[Token] Failed to get access token")

			// 失败后等待一段时间再重试，并检查停止信号
			select {
			case <-m.ctx.Done():
				logrus.Info("[Token] Auto refresh stopped during retry wait")
				return
			case <-time.After(m.retryDelay): // 使用配置的重试延迟
				continue
			}
		}

		m.mu.Lock()
		m.accessToken = resp.Get("access_token").Str
		expiresIn := resp.Get("expires_in").Int()
		m.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
		// 首次成功获取到 token 后，设置 ready 并广播
		if !firstSuccess {
			m.ready = true
			firstSuccess = true
			m.cond.Broadcast()
		}
		m.mu.Unlock()
		logrus.WithField("access_token", m.accessToken).WithField("expires_in", expiresIn).Debug("[Token] Updated")

		// 计算刷新时间，使用配置的 refreshAdvance
		refreshAfter := time.Duration(expiresIn)*time.Second - m.refreshAdvance
		if refreshAfter <= 0 {
			// 如果 expires_in 太小，则使用最小刷新比例
			refreshAfter = time.Duration(float64(expiresIn) * m.minRefreshRatio * float64(time.Second))
		}

		logrus.Debugf("[Token] Next refresh in %v (expires_in=%ds, advance=%v)",
			refreshAfter, expiresIn, m.refreshAdvance)

		// 等待刷新时间或停止信号
		select {
		case <-m.ctx.Done():
			logrus.Info("[Token] Auto refresh stopped during refresh wait")
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
	return httpcilent.NewPost(constant.AccessTokenURL).SetJSONBody(bodyMap).DoJSON()
}
