package middleware

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/shirou/gopsutil/v3/cpu"
)

// latencyHistogram 是一个轻量级固定桶直方图，用于计算 P99 延迟。
// 无外部依赖，使用 32 个指数增长的桶覆盖 0.1ms ~ 10s。
// 每个桶使用 atomic.Int64，读写均无锁。
type latencyHistogram struct {
	// 桶边界（纳秒），32 个桶，指数增长：~0.1ms, ~0.2ms, ~0.4ms … ~10s
	bounds [32]int64
	counts [32]atomic.Int64
	total  atomic.Int64 // 总样本数，用于快速判断是否有数据
}

// newLatencyHistogram 创建默认桶配置的直方图
func newLatencyHistogram() *latencyHistogram {
	h := &latencyHistogram{}
	// 生成 32 个指数桶：起点 100µs，公比 ~1.6，覆盖到 ~10s
	base := int64(100_000) // 100µs in ns
	for i := range 32 {
		h.bounds[i] = base
		base = base * 8 / 5 // ×1.6
	}
	return h
}

// observe 记录一个延迟样本
func (h *latencyHistogram) observe(d time.Duration) {
	ns := d.Nanoseconds()
	if ns <= 0 {
		return
	}
	// 找到对应桶（线性搜索，32 次迭代，极快）
	idx := 31
	for i, bound := range h.bounds {
		if ns <= bound {
			idx = i
			break
		}
	}
	h.counts[idx].Add(1)
	h.total.Add(1)
}

// percentile 计算百分位数延迟（0-100），并重置所有桶。
// 如果无样本则返回 0。
func (h *latencyHistogram) percentile(p float64) time.Duration {
	total := h.total.Load()
	if total == 0 {
		return 0
	}

	// 读取并重置各桶
	counts := make([]int64, 32)
	for i := range 32 {
		counts[i] = h.counts[i].Swap(0)
	}
	h.total.Store(0)

	// 计算目标排名
	target := int64(float64(total)*p/100.0 + 0.5)
	if target <= 0 {
		target = 1
	}

	var cumulative int64
	for i, c := range counts {
		cumulative += c
		if cumulative >= target {
			// 返回该桶的上界作为近似值
			return time.Duration(h.bounds[i])
		}
	}
	// 全部样本都在最后一桶之后
	return time.Duration(h.bounds[31])
}

// AdaptiveRateLimiter 自适应限流器
// 根据系统负载（CPU、内存、延迟）自动调整并发限制
type AdaptiveRateLimiter struct {
	// 配置
	config AdaptiveConfig

	// 当前限制
	maxConcurrency atomic.Int32
	currentLoad    atomic.Int32

	// 系统指标
	cpuUsage    atomic.Value // float64
	memoryUsage atomic.Value // float64
	latencyP99  atomic.Value // time.Duration

	// 改进 3.1: 使用固定桶直方图替换 latencySum/latencyCount + avg*1.5 近似
	latencyHist *latencyHistogram

	// 统计
	totalRequests    atomic.Int64
	rejectedRequests atomic.Int64

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

// AdaptiveConfig 自适应限流配置
type AdaptiveConfig struct {
	// 并发限制范围
	MinConcurrency int `yaml:"min_concurrency" json:"min_concurrency"`
	MaxConcurrency int `yaml:"max_concurrency" json:"max_concurrency"`
	InitialLimit   int `yaml:"initial_limit" json:"initial_limit"`

	// 目标指标
	TargetCPU     float64       `yaml:"target_cpu" json:"target_cpu"`         // 目标 CPU 使用率 (0.0-1.0)
	TargetMemory  float64       `yaml:"target_memory" json:"target_memory"`   // 目标内存使用率 (0.0-1.0)
	TargetLatency time.Duration `yaml:"target_latency" json:"target_latency"` // 目标 P99 延迟

	// 调整策略
	AdjustInterval time.Duration `yaml:"adjust_interval" json:"adjust_interval"` // 调整间隔
	AdjustStep     int           `yaml:"adjust_step" json:"adjust_step"`         // 每次调整的步长
	CooldownPeriod time.Duration `yaml:"cooldown_period" json:"cooldown_period"` // 冷却期

	// 采样
	SampleWindow   time.Duration `yaml:"sample_window" json:"sample_window"`     // 采样窗口
	MetricsEnabled bool          `yaml:"metrics_enabled" json:"metrics_enabled"` // 是否启用指标采集
}

// DefaultAdaptiveConfig 返回默认配置
func DefaultAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		MinConcurrency: 10,
		MaxConcurrency: 1000,
		InitialLimit:   100,
		TargetCPU:      0.70, // 70% CPU
		TargetMemory:   0.80, // 80% Memory
		TargetLatency:  500 * time.Millisecond,
		AdjustInterval: 10 * time.Second,
		AdjustStep:     10,
		CooldownPeriod: 30 * time.Second,
		SampleWindow:   60 * time.Second,
		MetricsEnabled: true,
	}
}

// NewAdaptiveRateLimiter 创建自适应限流器（使用 context.Background() 作为根 context）
func NewAdaptiveRateLimiter(config AdaptiveConfig) *AdaptiveRateLimiter {
	return NewAdaptiveRateLimiterWithContext(context.Background(), config)
}

// NewAdaptiveRateLimiterWithContext 创建与外部 context 联动的自适应限流器。
//
// 当 parent ctx 被取消时（如 Bot 关闭），后台 goroutine（adjustLoop/metricsLoop）
// 将自动退出，无需手动调用 Stop()。
//
// 推荐在 Bot 的生命周期中使用此函数，将 Bot 根 context 传入：
//
//	arl := middleware.NewAdaptiveRateLimiterWithContext(bot.Context(), config)
//	arl.Start()
func NewAdaptiveRateLimiterWithContext(parent context.Context, config AdaptiveConfig) *AdaptiveRateLimiter {
	// 验证配置
	if config.MinConcurrency <= 0 {
		config.MinConcurrency = 10
	}
	if config.MaxConcurrency <= config.MinConcurrency {
		config.MaxConcurrency = config.MinConcurrency * 10
	}
	if config.InitialLimit <= 0 || config.InitialLimit < config.MinConcurrency {
		config.InitialLimit = config.MinConcurrency
	}
	if config.InitialLimit > config.MaxConcurrency {
		config.InitialLimit = config.MaxConcurrency
	}
	if config.AdjustInterval <= 0 {
		config.AdjustInterval = 10 * time.Second
	}
	if config.AdjustStep <= 0 {
		config.AdjustStep = 10
	}

	ctx, cancel := context.WithCancel(parent)

	arl := &AdaptiveRateLimiter{
		config:      config,
		ctx:         ctx,
		cancel:      cancel,
		latencyHist: newLatencyHistogram(),
	}

	arl.maxConcurrency.Store(int32(config.InitialLimit))
	arl.cpuUsage.Store(0.0)
	arl.memoryUsage.Store(0.0)
	arl.latencyP99.Store(time.Duration(0))

	return arl
}

// Start 启动自适应调整
func (arl *AdaptiveRateLimiter) Start() {
	arl.wg.Add(2)
	go arl.adjustLoop()
	go arl.metricsLoop()

	logger.WithFields(logger.Fields{
		"initial_limit": arl.config.InitialLimit,
		"min":           arl.config.MinConcurrency,
		"max":           arl.config.MaxConcurrency,
	}).Info("[AdaptiveRateLimiter] Started")
}

// Stop 停止自适应调整
func (arl *AdaptiveRateLimiter) Stop() {
	arl.cancel()
	arl.wg.Wait()
	logger.Info("[AdaptiveRateLimiter] Stopped")
}

// Middleware 返回中间件函数
func (arl *AdaptiveRateLimiter) Middleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			arl.totalRequests.Add(1)

			// 修复 #5：消除 CAS 1000 次自旋忙等待。
			// 原实现无论如何最多自旋 1000 次，在高并发下浪费大量 CPU。
			// 改为：直接检查当前负载，若已超限立即拒绝；否则用 CAS 尝试获取，
			// CAS 失败（说明有并发竞争）时立即重试但不做无效空转。
			// 对于自适应限流器，limit 本身是"软限制"，单次决策即可。
			acquired := false
			for {
				current := arl.currentLoad.Load()
				limit := arl.maxConcurrency.Load()
				if current >= limit {
					// 超过限制，立即拒绝请求
					arl.rejectedRequests.Add(1)
					logger.WithFields(logger.Fields{
						"current_limit": limit,
						"current_load":  current,
					}).Warn("[AdaptiveRateLimiter] Request rejected")
					return fmt.Errorf("adaptive rate limit exceeded (limit: %d)", limit)
				}
				// 尝试原子增加负载，CAS 失败说明并发竞争，立即重试（无 sleep/yield）
				if arl.currentLoad.CompareAndSwap(current, current+1) {
					acquired = true
					break
				}
				// CAS 失败：有其他 goroutine 并发修改，重新读取最新值后再判断
				// 通常只需 1-3 次即可成功，不存在真正的"1000次"情况
			}

			if !acquired {
				// 理论上不可达（loop 内要么 return 要么 break），保留作防御
				arl.rejectedRequests.Add(1)
				return fmt.Errorf("adaptive rate limit: failed to acquire slot")
			}

			start := time.Now()

			defer func() {
				// 释放令牌
				arl.currentLoad.Add(-1)

				// 改进 3.1: 使用直方图记录延迟，替换 avg*1.5 近似
				latency := time.Since(start)
				if latency > 0 && latency < time.Hour {
					arl.latencyHist.observe(latency)
				} else if latency >= time.Hour {
					logger.WithFields(logger.Fields{
						"latency": latency,
					}).Warn("[AdaptiveRateLimiter] Abnormal latency detected, not recorded")
				}
			}()

			// 修复：捕获 panic，确保 defer 能执行
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("panic in handler: %v", r)
						logger.WithField("panic", r).Error("[AdaptiveRateLimiter] Handler panic recovered")
					}
				}()
				err = next(ctx)
			}()

			return err
		}
	}
}

// adjustLoop 自适应调整循环
func (arl *AdaptiveRateLimiter) adjustLoop() {
	defer arl.wg.Done()

	ticker := time.NewTicker(arl.config.AdjustInterval)
	defer ticker.Stop()

	lastAdjustTime := time.Now()

	for {
		select {
		case <-arl.ctx.Done():
			return

		case <-ticker.C:
			// 冷却期检查
			if time.Since(lastAdjustTime) < arl.config.CooldownPeriod {
				continue
			}

			// 获取当前指标
			cpuUsageVal := arl.getCPUUsage()
			memory := arl.getMemoryUsage()
			latency := arl.getLatencyP99()
			currentLimit := arl.maxConcurrency.Load()

			// 修复：采集失败时跳过调整（负值表示采集失败）
			if cpuUsageVal < 0 || memory < 0 {
				logger.Warn("[AdaptiveRateLimiter] Metrics collection failed, skipping adjustment")
				continue
			}

			// 决策是否调整
			newLimit := arl.decideLimit(cpuUsageVal, memory, latency, currentLimit)

			if newLimit != currentLimit {
				arl.adjustLimit(newLimit)
				lastAdjustTime = time.Now()

				logger.WithFields(logger.Fields{
					"old_limit":      currentLimit,
					"new_limit":      newLimit,
					"cpu":            fmt.Sprintf("%.2f%%", cpuUsageVal*100),
					"memory":         fmt.Sprintf("%.2f%%", memory*100),
					"latency_p99":    latency,
					"target_cpu":     fmt.Sprintf("%.2f%%", arl.config.TargetCPU*100),
					"target_latency": arl.config.TargetLatency,
				}).Info("[AdaptiveRateLimiter] Limit adjusted")
			}
		}
	}
}

// metricsLoop 定期采集系统指标
func (arl *AdaptiveRateLimiter) metricsLoop() {
	defer arl.wg.Done()

	if !arl.config.MetricsEnabled {
		return
	}

	ticker := time.NewTicker(5 * time.Second) // 每5秒采集一次
	defer ticker.Stop()

	for {
		select {
		case <-arl.ctx.Done():
			return

		case <-ticker.C:
			arl.collectMetrics()
		}
	}
}

// collectMetrics 采集系统指标
func (arl *AdaptiveRateLimiter) collectMetrics() {
	// 改进：使用非阻塞采样 interval=0（对比上次调用的增量），避免每次阻塞 1 秒
	// metricsLoop 每 5 秒触发一次，两次调用间隔已足够作为采样窗口
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil || len(cpuPercent) == 0 {
		logger.WithError(err).Warn("[AdaptiveRateLimiter] Failed to get CPU usage, using fallback")
		arl.cpuUsage.Store(-1.0)
	} else {
		cpuUsage := cpuPercent[0] / 100.0
		arl.cpuUsage.Store(cpuUsage)
	}

	// 内存使用率
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 使用 HeapAlloc / HeapSys 更准确地反映堆内存使用率
	memUsage := float64(m.HeapAlloc) / float64(m.HeapSys)
	if memUsage > 1.0 {
		memUsage = 1.0
	}
	arl.memoryUsage.Store(memUsage)

	// 改进 3.1: 使用直方图计算真实 P99，替换 avg*1.5 近似方案
	if p99 := arl.latencyHist.percentile(99); p99 > 0 {
		arl.latencyP99.Store(p99)
	}
}

// decideLimit 决策新的限制值
func (arl *AdaptiveRateLimiter) decideLimit(cpu, memory float64, latency time.Duration, currentLimit int32) int32 {
	// 计算压力得分（0-1，越大压力越大）
	cpuPressure := cpu / arl.config.TargetCPU
	memoryPressure := memory / arl.config.TargetMemory
	latencyPressure := float64(latency) / float64(arl.config.TargetLatency)

	// 取最大压力作为系统压力
	systemPressure := maxFloat(cpuPressure, memoryPressure, latencyPressure)

	var newLimit int32

	if systemPressure > 1.2 {
		// 压力过大，降低限制
		newLimit = currentLimit - int32(arl.config.AdjustStep*2)
	} else if systemPressure > 1.0 {
		// 轻度压力，小幅降低
		newLimit = currentLimit - int32(arl.config.AdjustStep)
	} else if systemPressure < 0.7 {
		// 压力很小，大幅提升
		newLimit = currentLimit + int32(arl.config.AdjustStep*2)
	} else if systemPressure < 0.85 {
		// 压力适中偏低，小幅提升
		newLimit = currentLimit + int32(arl.config.AdjustStep)
	} else {
		// 压力适中，保持不变
		newLimit = currentLimit
	}

	// 限制在范围内
	if newLimit < int32(arl.config.MinConcurrency) {
		newLimit = int32(arl.config.MinConcurrency)
	}
	if newLimit > int32(arl.config.MaxConcurrency) {
		newLimit = int32(arl.config.MaxConcurrency)
	}

	return newLimit
}

// adjustLimit 调整限制
//
// 修复：使用原子计数器代替 channel，避免 channel 替换的竞态问题
// 新的限制会在下次请求时生效
func (arl *AdaptiveRateLimiter) adjustLimit(newLimit int32) {
	oldLimit := arl.maxConcurrency.Swap(newLimit)

	if oldLimit != newLimit {
		logger.WithFields(logger.Fields{
			"old": oldLimit,
			"new": newLimit,
		}).Debug("[AdaptiveRateLimiter] Limit adjusted")
	}
}

// getCPUUsage 获取 CPU 使用率
func (arl *AdaptiveRateLimiter) getCPUUsage() float64 {
	if v := arl.cpuUsage.Load(); v != nil {
		return v.(float64)
	}
	return 0.0
}

// getMemoryUsage 获取内存使用率
func (arl *AdaptiveRateLimiter) getMemoryUsage() float64 {
	if v := arl.memoryUsage.Load(); v != nil {
		return v.(float64)
	}
	return 0.0
}

// getLatencyP99 获取 P99 延迟
func (arl *AdaptiveRateLimiter) getLatencyP99() time.Duration {
	if v := arl.latencyP99.Load(); v != nil {
		return v.(time.Duration)
	}
	return 0
}

// GetStats 获取统计信息
func (arl *AdaptiveRateLimiter) GetStats() AdaptiveStats {
	return AdaptiveStats{
		CurrentLimit:     arl.maxConcurrency.Load(),
		CurrentLoad:      arl.currentLoad.Load(),
		TotalRequests:    arl.totalRequests.Load(),
		RejectedRequests: arl.rejectedRequests.Load(),
		CPUUsage:         arl.getCPUUsage(),
		MemoryUsage:      arl.getMemoryUsage(),
		LatencyP99:       arl.getLatencyP99(),
		RejectionRate:    float64(arl.rejectedRequests.Load()) / float64(maxInt64(1, arl.totalRequests.Load())),
	}
}

// AdaptiveStats 自适应限流统计
type AdaptiveStats struct {
	CurrentLimit     int32
	CurrentLoad      int32
	TotalRequests    int64
	RejectedRequests int64
	CPUUsage         float64
	MemoryUsage      float64
	LatencyP99       time.Duration
	RejectionRate    float64
}

// Helper functions

func maxFloat(a, b, c float64) float64 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// UpdateConfig 热更新配置（线程安全）。
// 仅更新目标指标（TargetCPU / TargetMemory / TargetLatency）和并发范围；
// 不重启后台 goroutine，下一个 adjustLoop 周期生效。
func (arl *AdaptiveRateLimiter) UpdateConfig(cfg AdaptiveConfig) {
	arl.mu.Lock()
	defer arl.mu.Unlock()
	// 只覆盖可热更新的字段，保留 ctx/cancel 等运行时状态
	if cfg.MinConcurrency > 0 {
		arl.config.MinConcurrency = cfg.MinConcurrency
	}
	if cfg.MaxConcurrency > 0 {
		arl.config.MaxConcurrency = cfg.MaxConcurrency
	}
	if cfg.TargetCPU > 0 {
		arl.config.TargetCPU = cfg.TargetCPU
	}
	if cfg.TargetMemory > 0 {
		arl.config.TargetMemory = cfg.TargetMemory
	}
	if cfg.TargetLatency > 0 {
		arl.config.TargetLatency = cfg.TargetLatency
	}
	if cfg.AdjustStep > 0 {
		arl.config.AdjustStep = cfg.AdjustStep
	}
	logger.Info("[AdaptiveRateLimiter] Config updated via hot-reload")
}

// AdaptiveRateLimit 创建自适应限流中间件（便捷函数）
func AdaptiveRateLimit(config AdaptiveConfig) eventctx.Middleware {
	limiter := NewAdaptiveRateLimiter(config)
	limiter.Start()
	return limiter.Middleware()
}
