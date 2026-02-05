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
)

// AdaptiveRateLimiter 自适应限流器
// 根据系统负载（CPU、内存、延迟）自动调整并发限制
type AdaptiveRateLimiter struct {
	// 配置
	config AdaptiveConfig

	// 当前限制
	maxConcurrency atomic.Int32
	currentLoad    atomic.Int32

	// 系统指标
	cpuUsage     atomic.Value // float64
	memoryUsage  atomic.Value // float64
	latencyP99   atomic.Value // time.Duration
	latencySum   atomic.Int64 // 总延迟（纳秒）
	latencyCount atomic.Int64 // 请求计数

	// 统计
	totalRequests    atomic.Int64
	rejectedRequests atomic.Int64

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex

	// 信号量
	sema chan struct{}
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

// NewAdaptiveRateLimiter 创建自适应限流器
func NewAdaptiveRateLimiter(config AdaptiveConfig) *AdaptiveRateLimiter {
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

	ctx, cancel := context.WithCancel(context.Background())

	arl := &AdaptiveRateLimiter{
		config: config,
		ctx:    ctx,
		cancel: cancel,
		sema:   make(chan struct{}, config.InitialLimit),
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

			// 尝试获取令牌
			select {
			case arl.sema <- struct{}{}:
				// 获取成功
				arl.currentLoad.Add(1)

				start := time.Now()

				defer func() {
					// 释放令牌
					<-arl.sema
					arl.currentLoad.Add(-1)

					// 记录延迟（添加合理性检查，防止统计溢出）
					latency := time.Since(start)

					// 只记录合理范围内的延迟（< 1小时），避免异常值和溢出
					if latency > 0 && latency < time.Hour {
						arl.latencySum.Add(latency.Nanoseconds())
						arl.latencyCount.Add(1)
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

			default:
				// 超过限制，拒绝请求
				arl.rejectedRequests.Add(1)

				logger.WithFields(logger.Fields{
					"current_limit": arl.maxConcurrency.Load(),
					"current_load":  arl.currentLoad.Load(),
					"rejected":      arl.rejectedRequests.Load(),
				}).Warn("[AdaptiveRateLimiter] Request rejected")

				return fmt.Errorf("adaptive rate limit exceeded (limit: %d)", arl.maxConcurrency.Load())
			}
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
			cpu := arl.getCPUUsage()
			memory := arl.getMemoryUsage()
			latency := arl.getLatencyP99()
			currentLimit := arl.maxConcurrency.Load()

			// 修复：采集失败时跳过调整（负值表示采集失败）
			if cpu < 0 || memory < 0 {
				logger.Warn("[AdaptiveRateLimiter] Metrics collection failed, skipping adjustment")
				continue
			}

			// 决策是否调整
			newLimit := arl.decideLimit(cpu, memory, latency, currentLimit)

			if newLimit != currentLimit {
				arl.adjustLimit(newLimit)
				lastAdjustTime = time.Now()

				logger.WithFields(logger.Fields{
					"old_limit":      currentLimit,
					"new_limit":      newLimit,
					"cpu":            fmt.Sprintf("%.2f%%", cpu*100),
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
	// 采集 CPU 使用率
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// CPU 使用率（简化计算，实际应该使用更准确的方法）
	numCPU := runtime.NumCPU()
	numGoroutine := runtime.NumGoroutine()
	cpuUsage := float64(numGoroutine) / float64(numCPU*100) // 简化估算
	if cpuUsage > 1.0 {
		cpuUsage = 1.0
	}
	arl.cpuUsage.Store(cpuUsage)

	// 内存使用率
	memUsage := float64(m.Alloc) / float64(m.Sys)
	if memUsage > 1.0 {
		memUsage = 1.0
	}
	arl.memoryUsage.Store(memUsage)

	// 计算 P99 延迟
	latencySum := arl.latencySum.Load()
	latencyCount := arl.latencyCount.Load()
	if latencyCount > 0 {
		avgLatency := time.Duration(latencySum / latencyCount)
		// 简化：使用平均值的1.5倍作为P99（实际应该使用直方图）
		p99 := avgLatency * 3 / 2
		arl.latencyP99.Store(p99)

		// 重置计数器
		arl.latencySum.Store(0)
		arl.latencyCount.Store(0)
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
func (arl *AdaptiveRateLimiter) adjustLimit(newLimit int32) {
	arl.mu.Lock()
	defer arl.mu.Unlock()

	oldLimit := arl.maxConcurrency.Load()
	arl.maxConcurrency.Store(newLimit)

	// 创建新的信号量 channel
	newSema := make(chan struct{}, newLimit)

	// 迁移现有的令牌
	currentLoad := int32(len(arl.sema))
	for i := int32(0); i < currentLoad && i < newLimit; i++ {
		newSema <- struct{}{}
	}

	// 原子替换
	arl.sema = newSema

	logger.WithFields(logger.Fields{
		"old": oldLimit,
		"new": newLimit,
	}).Debug("[AdaptiveRateLimiter] Semaphore adjusted")
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

// AdaptiveRateLimit 创建自适应限流中间件（便捷函数）
func AdaptiveRateLimit(config AdaptiveConfig) eventctx.Middleware {
	limiter := NewAdaptiveRateLimiter(config)
	limiter.Start()
	return limiter.Middleware()
}
