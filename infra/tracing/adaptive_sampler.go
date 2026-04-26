package tracing

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// AdaptiveSampler 自适应采样器
//
// 根据系统状态动态调整采样率：
// - 正常情况下使用基础采样率
// - 错误率高时提高采样率（捕获更多错误trace）
// - 负载高时降低采样率（减轻系统压力）
// - 始终采样错误的span
type AdaptiveSampler struct {
	// 基础采样率
	baseSamplingRate float64

	// 当前动态采样率
	currentSamplingRate atomic.Value // float64

	// 错误率统计
	totalSpans atomic.Int64
	errorSpans atomic.Int64
	lastReset  time.Time

	// 配置
	config AdaptiveSamplerConfig

	// 缓存 ParentBased + TraceIDRatioBased 动态采样器，仅在 currentSamplingRate 变化时重建
	// 避免每次 ShouldSample 调用都分配 2 个堆对象
	dynamicSampler atomic.Value // sdktrace.Sampler

	// 保护 lastReset 和统计重置的互斥锁
	// 与 StartMonitor 共享，避免双重重置
	resetMu sync.Mutex
}

// AdaptiveSamplerConfig 自适应采样器配置
type AdaptiveSamplerConfig struct {
	// BaseSamplingRate 基础采样率 (0.0-1.0)
	BaseSamplingRate float64

	// MinSamplingRate 最小采样率
	MinSamplingRate float64

	// MaxSamplingRate 最大采样率
	MaxSamplingRate float64

	// ErrorThreshold 错误率阈值 (0.0-1.0)
	// 超过此阈值时提高采样率
	ErrorThreshold float64

	// HighErrorSamplingRate 高错误率时的采样率
	HighErrorSamplingRate float64

	// AdjustInterval 调整间隔
	AdjustInterval time.Duration

	// AlwaysSampleErrors 是否始终采样错误
	AlwaysSampleErrors bool
}

// DefaultAdaptiveSamplerConfig 返回默认配置
func DefaultAdaptiveSamplerConfig() AdaptiveSamplerConfig {
	return AdaptiveSamplerConfig{
		BaseSamplingRate:      0.1,  // 10% 基础采样率
		MinSamplingRate:       0.01, // 最低 1%
		MaxSamplingRate:       1.0,  // 最高 100%
		ErrorThreshold:        0.05, // 5% 错误率阈值
		HighErrorSamplingRate: 0.5,  // 高错误率时 50% 采样
		AdjustInterval:        1 * time.Minute,
		AlwaysSampleErrors:    true,
	}
}

// NewAdaptiveSampler 创建自适应采样器
func NewAdaptiveSampler(config AdaptiveSamplerConfig) *AdaptiveSampler {
	// 验证配置
	if config.BaseSamplingRate <= 0 {
		config.BaseSamplingRate = 0.1
	}
	if config.MinSamplingRate <= 0 {
		config.MinSamplingRate = 0.01
	}
	if config.MaxSamplingRate <= 0 || config.MaxSamplingRate > 1.0 {
		config.MaxSamplingRate = 1.0
	}
	if config.ErrorThreshold <= 0 {
		config.ErrorThreshold = 0.05
	}
	if config.HighErrorSamplingRate <= 0 {
		config.HighErrorSamplingRate = 0.5
	}
	if config.AdjustInterval == 0 {
		config.AdjustInterval = 1 * time.Minute
	}

	sampler := &AdaptiveSampler{
		baseSamplingRate: config.BaseSamplingRate,
		lastReset:        time.Now(),
		config:           config,
	}

	sampler.currentSamplingRate.Store(config.BaseSamplingRate)
	sampler.rebuildDynamicSampler(config.BaseSamplingRate)

	return sampler
}

// rebuildDynamicSampler 重建缓存的动态采样器。仅在采样率变化时调用。
func (as *AdaptiveSampler) rebuildDynamicSampler(rate float64) {
	as.dynamicSampler.Store(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(rate)))
}

// ShouldSample 实现 sdktrace.Sampler 接口
func (as *AdaptiveSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	as.totalSpans.Add(1)

	// 检查 span 是否包含错误标记（合并 AlwaysSampleErrors 检查和错误计数，O(1n) 替代 O(2n)）
	hasError := false
	for _, attr := range p.Attributes {
		if attr.Key == "error" || attr.Key == "exception" {
			hasError = true
			break
		}
	}

	// 始终采样错误（如果配置启用）
	if hasError && as.config.AlwaysSampleErrors {
		as.errorSpans.Add(1)
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Tracestate: trace.SpanContextFromContext(p.ParentContext).TraceState(),
		}
	}

	// 使用缓存的动态采样器（避免每次调用分配）
	ds := as.dynamicSampler.Load().(sdktrace.Sampler)
	result := ds.ShouldSample(p)

	// 如果是错误span，记录统计
	if hasError && result.Decision == sdktrace.RecordAndSample {
		as.errorSpans.Add(1)
	}

	return result
}

// Description 实现 sdktrace.Sampler 接口
func (as *AdaptiveSampler) Description() string {
	ds := as.dynamicSampler.Load().(sdktrace.Sampler)
	return ds.Description()
}

// AdjustSamplingRate 调整采样率（由监控协程调用）
func (as *AdaptiveSampler) AdjustSamplingRate() {
	total := as.totalSpans.Load()
	errors := as.errorSpans.Load()

	if total == 0 {
		return
	}

	errorRate := float64(errors) / float64(total)

	var newRate float64
	if errorRate > as.config.ErrorThreshold {
		// 错误率高，提高采样率
		newRate = as.config.HighErrorSamplingRate
		logger.WithFields(logger.Fields{
			"error_rate":  errorRate,
			"old_rate":    as.currentSamplingRate.Load().(float64),
			"new_rate":    newRate,
			"total_spans": total,
			"errors":      errors,
		}).Warn("[Tracing] High error rate detected, increasing sampling rate")
	} else {
		// 错误率正常，使用基础采样率
		newRate = as.baseSamplingRate
	}

	// 限制采样率范围
	if newRate < as.config.MinSamplingRate {
		newRate = as.config.MinSamplingRate
	}
	if newRate > as.config.MaxSamplingRate {
		newRate = as.config.MaxSamplingRate
	}

	oldRate := as.currentSamplingRate.Load().(float64)
	if oldRate != newRate {
		as.currentSamplingRate.Store(newRate)
		as.rebuildDynamicSampler(newRate)
		logger.WithFields(logger.Fields{
			"old_rate": oldRate,
			"new_rate": newRate,
		}).Info("[Tracing] Sampling rate adjusted")
	}
}

// GetCurrentSamplingRate 获取当前采样率
func (as *AdaptiveSampler) GetCurrentSamplingRate() float64 {
	return as.currentSamplingRate.Load().(float64)
}

// GetStats 获取统计信息
func (as *AdaptiveSampler) GetStats() AdaptiveSamplerStats {
	total := as.totalSpans.Load()
	errors := as.errorSpans.Load()

	var errorRate float64
	if total > 0 {
		errorRate = float64(errors) / float64(total)
	}

	return AdaptiveSamplerStats{
		TotalSpans:          total,
		ErrorSpans:          errors,
		ErrorRate:           errorRate,
		CurrentSamplingRate: as.GetCurrentSamplingRate(),
		BaseSamplingRate:    as.baseSamplingRate,
	}
}

// AdaptiveSamplerStats 采样器统计信息
type AdaptiveSamplerStats struct {
	TotalSpans          int64
	ErrorSpans          int64
	ErrorRate           float64
	CurrentSamplingRate float64
	BaseSamplingRate    float64
}

// StartMonitor 启动监控协程（自动调整采样率）
//
// 这是"重置统计 + 调整采样率"的唯一路径，不再有 Hot Path（ShouldSample 内）的
// maybeResetStats 重置逻辑，避免双重重置。
func (as *AdaptiveSampler) StartMonitor(ctx context.Context) {
	ticker := time.NewTicker(as.config.AdjustInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			as.resetMu.Lock()
			as.AdjustSamplingRate()
			as.totalSpans.Store(0)
			as.errorSpans.Store(0)
			as.lastReset = time.Now()
			as.resetMu.Unlock()
		}
	}
}
