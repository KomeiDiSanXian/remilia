// Package hotreload 提供配置热更新到中间件的传播桥接。
//
// 将 config.Watcher 的变更回调与各中间件的 UpdateConfig 方法连接起来，
// 实现配置文件修改后中间件参数自动生效，无需重启 Bot。
//
// 使用示例:
//
//	bridge := hotreload.NewBridge()
//	bridge.WatchAdaptive(adaptiveLimiter)
//	bridge.WatchRetry(configurableRetry)
//	bridge.WatchCircuitBreaker(circuitBreaker)
//	bridge.WatchDedup(dedupFilter)
//	bridge.WatchDegradation(degradationCtrl)
//	token := config.Subscribe(bridge.OnConfigChange)
//	defer token.Cancel()
package hotreload

import (
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware/dedup"
	"github.com/KomeiDiSanXian/remilia/middleware/degradation"
	"github.com/KomeiDiSanXian/remilia/middleware/ratelimit"
	"github.com/KomeiDiSanXian/remilia/middleware/resilience"
)

// Bridge 配置热更新桥接器
type Bridge struct {
	mu              sync.RWMutex
	adaptives       []*ratelimit.AdaptiveRateLimiter
	retries         []*resilience.ConfigurableRetry
	circuitBreakers []*resilience.CircuitBreaker
	dedups          []*dedup.DedupFilter
	degradations    []*degradation.AdaptiveDegradation
}

// NewBridge 创建桥接器
func NewBridge() *Bridge {
	return &Bridge{}
}

// WatchAdaptive 注册 AdaptiveRateLimiter 接收热更新
func (b *Bridge) WatchAdaptive(arl *ratelimit.AdaptiveRateLimiter) *Bridge {
	b.mu.Lock()
	b.adaptives = append(b.adaptives, arl)
	b.mu.Unlock()
	return b
}

// WatchRetry 注册 ConfigurableRetry 接收热更新
func (b *Bridge) WatchRetry(cr *resilience.ConfigurableRetry) *Bridge {
	b.mu.Lock()
	b.retries = append(b.retries, cr)
	b.mu.Unlock()
	return b
}

// WatchCircuitBreaker 注册 CircuitBreaker 接收热更新
func (b *Bridge) WatchCircuitBreaker(cb *resilience.CircuitBreaker) *Bridge {
	b.mu.Lock()
	b.circuitBreakers = append(b.circuitBreakers, cb)
	b.mu.Unlock()
	return b
}

// WatchDedup 注册 DedupFilter 接收热更新（MaxSize、DefaultTTL 立即生效）
func (b *Bridge) WatchDedup(df *dedup.DedupFilter) *Bridge {
	b.mu.Lock()
	b.dedups = append(b.dedups, df)
	b.mu.Unlock()
	return b
}

// WatchDegradation 注册 AdaptiveDegradation 接收热更新
// （CPUThreshold、MemoryThreshold、LatencyThreshold 等下一监控周期生效）
func (b *Bridge) WatchDegradation(ad *degradation.AdaptiveDegradation) *Bridge {
	b.mu.Lock()
	b.degradations = append(b.degradations, ad)
	b.mu.Unlock()
	return b
}

// OnConfigChange 实现 config.ChangeListener，将新配置推送到所有已注册的中间件。
// 通过 config.Subscribe(bridge.OnConfigChange) 注册到 config 包。
func (b *Bridge) OnConfigChange(newCfg *config.Config) {
	if newCfg == nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	mc := newCfg.Middleware
	rc := newCfg.Retry

	// 更新 AdaptiveRateLimiter
	if mc.RateLimit.Enable {
		for _, arl := range b.adaptives {
			arl.UpdateConfig(ratelimit.AdaptiveConfig{
				AdjustStep: mc.RateLimit.Burst,
			})
		}
	}

	// 更新 ConfigurableRetry
	if rc.Enable {
		base := parseDuration(rc.BackoffBase, 200*time.Millisecond)
		duration := parseDuration(rc.BackoffMax, 2*time.Second)
		for _, cr := range b.retries {
			cr.UpdateConfig(resilience.RetryConfig{
				MaxAttempts: rc.MaxAttempts,
				BackoffBase: base,
				BackoffMax:  duration,
			})
		}
	}

	// 更新 DedupFilter（仅在启用时）
	if mc.Dedup.Enable {
		ttl := parseDuration(mc.Dedup.DefaultTTL, 5*time.Minute)
		for _, df := range b.dedups {
			df.UpdateConfig(dedup.DedupConfig{
				MaxSize:    mc.Dedup.MaxSize,
				DefaultTTL: ttl,
			})
		}
	}

	// 更新 AdaptiveDegradation（仅在启用时）
	if mc.Degradation.Enable {
		for _, ad := range b.degradations {
			ad.UpdateConfig(degradation.DegradationConfig{
				CPUThreshold:    mc.Degradation.CPUThreshold,
				MemoryThreshold: mc.Degradation.MemoryThreshold,
			})
		}
	}

	logger.Info("[HotReload] Middleware configs updated from config file")
}

// Subscribe 便捷方法：注册到 config 包并返回 token
func (b *Bridge) Subscribe() *config.ListenerToken {
	return config.Subscribe(b.OnConfigChange)
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
