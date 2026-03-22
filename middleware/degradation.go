package middleware

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// degradationMetrics 持有单个 AdaptiveDegradation 实例的 Prometheus 指标。
//
// 使用实例级注册（而非包级 promauto）避免多次 import 或多个实例时重复注册 panic。
// 测试时可通过传入 prometheus.NewRegistry() 完全隔离。
type degradationMetrics struct {
	levelGauge      prometheus.Gauge
	activeGauge     prometheus.Gauge
	eventsTotal     *prometheus.CounterVec
	triggersTotal   *prometheus.CounterVec
	cpuGauge        prometheus.Gauge
	memoryGauge     prometheus.Gauge
	goroutinesGauge prometheus.Gauge
	recoveriesTotal *prometheus.CounterVec
}

// newDegradationMetrics 创建并注册降级指标。
//
// reg 为 nil 时使用 prometheus.DefaultRegisterer（生产环境）。
// 测试时传入 prometheus.NewRegistry() 实现完全隔离。
func newDegradationMetrics(reg prometheus.Registerer) *degradationMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &degradationMetrics{
		levelGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "remilia", Subsystem: "degradation", Name: "level",
			Help: "Current degradation level (0=normal, 1=light, 2=moderate, 3=severe)",
		}),
		activeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "remilia", Subsystem: "degradation", Name: "active",
			Help: "Whether degradation is currently active (1=active, 0=inactive)",
		}),
		eventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "remilia", Subsystem: "degradation", Name: "events_total",
			Help: "Total number of events by action (processed/dropped/delayed)",
		}, []string{"action"}),
		triggersTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "remilia", Subsystem: "degradation", Name: "triggers_total",
			Help: "Total number of degradation triggers by reason",
		}, []string{"reason"}),
		cpuGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "remilia", Subsystem: "degradation", Name: "cpu_usage_percent",
			Help: "Current CPU usage percentage",
		}),
		memoryGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "remilia", Subsystem: "degradation", Name: "memory_usage_percent",
			Help: "Current memory usage percentage",
		}),
		goroutinesGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "remilia", Subsystem: "degradation", Name: "goroutines_total",
			Help: "Current number of goroutines",
		}),
		recoveriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "remilia", Subsystem: "degradation", Name: "recoveries_total",
			Help: "Total number of degradation level recoveries",
		}, []string{"from_level", "to_level"}),
	}
	// 忽略 AlreadyRegisteredError：允许同名指标的多实例（取已注册的那个）
	mustOrGet := func(c prometheus.Collector) prometheus.Collector {
		if err := reg.Register(c); err != nil {
			if are, ok := errors.AsType[*prometheus.AlreadyRegisteredError](err); ok {
				return are.ExistingCollector
			}
			// 其他错误（如命名非法）直接 panic，开发期即可发现
			panic(err)
		}
		return c
	}
	m.levelGauge = mustOrGet(m.levelGauge).(prometheus.Gauge)
	m.activeGauge = mustOrGet(m.activeGauge).(prometheus.Gauge)
	m.eventsTotal = mustOrGet(m.eventsTotal).(*prometheus.CounterVec)
	m.triggersTotal = mustOrGet(m.triggersTotal).(*prometheus.CounterVec)
	m.cpuGauge = mustOrGet(m.cpuGauge).(prometheus.Gauge)
	m.memoryGauge = mustOrGet(m.memoryGauge).(prometheus.Gauge)
	m.goroutinesGauge = mustOrGet(m.goroutinesGauge).(prometheus.Gauge)
	m.recoveriesTotal = mustOrGet(m.recoveriesTotal).(*prometheus.CounterVec)
	return m
}

// DegradationStrategy 降级策略
type DegradationStrategy int

const (
	// DegradationDrop 丢弃策略：直接丢弃低优先级事件
	DegradationDrop DegradationStrategy = iota
	// DegradationDelay 延迟策略：延迟处理低优先级事件
	DegradationDelay
	// DegradationSimplify 简化策略：简化处理逻辑
	DegradationSimplify
)

// DegradationLevel 降级级别
type DegradationLevel int

const (
	// LevelNormal 正常状态
	LevelNormal DegradationLevel = iota
	// LevelLight 轻度降级：只处理高优先级事件
	LevelLight
	// LevelModerate 中度降级：只处理关键事件
	LevelModerate
	// LevelSevere 重度降级：只处理必要事件
	LevelSevere
)

// EventPriority 事件优先级
type EventPriority int

const (
	// PriorityLow 低优先级事件（如普通消息）
	PriorityLow EventPriority = iota
	// PriorityNormal 普通优先级事件
	PriorityNormal
	// PriorityHigh 高优先级事件（如@消息）
	PriorityHigh
	// PriorityCritical 关键事件（如系统消息、管理员消息）
	PriorityCritical
)

// AdaptiveDegradation 自适应降级控制器
//
// 根据系统负载（CPU、内存、延迟等）自动调整降级级别，
// 在系统过载时自动丢弃或延迟处理低优先级事件，保护系统稳定性。
//
// 使用示例：
//
//	deg := middleware.NewAdaptiveDegradation(middleware.DegradationConfig{
//	    CPUThreshold:    80.0,
//	    MemoryThreshold: 85.0,
//	    LatencyThreshold: 500 * time.Millisecond,
//	    Strategy: middleware.DegradationDrop,
//	})
//	go deg.StartMonitor(context.Background())
//	engine.Use(deg.Middleware())
type AdaptiveDegradation struct {
	config DegradationConfig

	// 当前降级级别
	level atomic.Value // DegradationLevel

	// 统计信息
	totalEvents   atomic.Int64
	droppedEvents atomic.Int64
	delayedEvents atomic.Int64

	// 延迟队列（用于延迟策略）
	delayQueue chan *eventctx.Context

	// 监控指标
	lastCPU     atomic.Value // float64
	lastMemory  atomic.Value // float64
	lastLatency atomic.Value // time.Duration

	// Prometheus 指标（实例级，避免包级 promauto 重复注册）
	metrics *degradationMetrics
}

// DegradationConfig 降级配置
type DegradationConfig struct {
	// CPUThreshold CPU 使用率阈值（百分比，0-100）
	// 超过此值开始降级
	CPUThreshold float64

	// MemoryThreshold 内存使用率阈值（百分比，0-100）
	MemoryThreshold float64

	// LatencyThreshold 延迟阈值
	// 超过此值开始降级
	LatencyThreshold time.Duration

	// Strategy 降级策略
	Strategy DegradationStrategy

	// MonitorInterval 监控间隔
	MonitorInterval time.Duration

	// RecoveryInterval 恢复检查间隔
	RecoveryInterval time.Duration

	// PriorityClassifier 事件优先级分类器
	// 如果未设置，使用默认分类器
	PriorityClassifier func(*eventctx.Context) EventPriority

	// OnLevelChange 降级级别变化回调
	OnLevelChange func(from, to DegradationLevel)

	// DelayQueueSize 延迟队列大小（延迟策略使用）
	DelayQueueSize int

	// EnableGoroutineLimit 是否启用协程数量监控
	EnableGoroutineLimit bool

	// GoroutineThreshold 协程数量阈值
	GoroutineThreshold int
}

// NewAdaptiveDegradation 创建自适应降级控制器
//
// reg 为 nil 时使用 prometheus.DefaultRegisterer。
// 测试时可传入 prometheus.NewRegistry() 隔离指标注册，避免多实例 panic。
func NewAdaptiveDegradation(config DegradationConfig) *AdaptiveDegradation {
	return NewAdaptiveDegradationWithRegistry(config, nil)
}

// NewAdaptiveDegradationWithRegistry 创建带自定义 Prometheus 注册器的降级控制器。
// 生产环境传 nil（使用默认注册器）；测试时传 prometheus.NewRegistry() 完全隔离。
func NewAdaptiveDegradationWithRegistry(config DegradationConfig, reg prometheus.Registerer) *AdaptiveDegradation {
	// 设置默认值
	if config.CPUThreshold == 0 {
		config.CPUThreshold = 80.0
	}
	if config.MemoryThreshold == 0 {
		config.MemoryThreshold = 85.0
	}
	if config.LatencyThreshold == 0 {
		config.LatencyThreshold = 500 * time.Millisecond
	}
	if config.MonitorInterval == 0 {
		config.MonitorInterval = 5 * time.Second
	}
	if config.RecoveryInterval == 0 {
		config.RecoveryInterval = 10 * time.Second
	}
	if config.PriorityClassifier == nil {
		config.PriorityClassifier = defaultPriorityClassifier
	}
	if config.DelayQueueSize == 0 {
		config.DelayQueueSize = 1000
	}
	if config.GoroutineThreshold == 0 {
		config.GoroutineThreshold = 10000
	}

	ad := &AdaptiveDegradation{
		config:     config,
		delayQueue: make(chan *eventctx.Context, config.DelayQueueSize),
		metrics:    newDegradationMetrics(reg),
	}

	ad.level.Store(LevelNormal)
	ad.lastCPU.Store(0.0)
	ad.lastMemory.Store(0.0)
	ad.lastLatency.Store(time.Duration(0))

	// 初始化指标初始值
	ad.metrics.levelGauge.Set(0) // LevelNormal
	ad.metrics.activeGauge.Set(0)

	return ad
}

// StartMonitor 启动监控
func (ad *AdaptiveDegradation) StartMonitor(ctx context.Context) {
	ticker := time.NewTicker(ad.config.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ad.checkAndAdjustLevel()
		}
	}
}

// checkAndAdjustLevel 检查并调整降级级别
func (ad *AdaptiveDegradation) checkAndAdjustLevel() {
	// 获取系统指标
	cpuPercent := ad.getCPUUsage()
	memPercent := ad.getMemoryUsage()
	goroutines := runtime.NumGoroutine()

	// 更新监控指标
	ad.lastCPU.Store(cpuPercent)
	ad.lastMemory.Store(memPercent)

	// 更新 Prometheus metrics
	ad.metrics.cpuGauge.Set(cpuPercent)
	ad.metrics.memoryGauge.Set(memPercent)
	ad.metrics.goroutinesGauge.Set(float64(goroutines))

	currentLevel := ad.GetLevel()
	newLevel := ad.calculateLevel(cpuPercent, memPercent, goroutines)

	if newLevel != currentLevel {
		ad.setLevel(newLevel)
		logger.WithFields(logger.Fields{
			"from":       currentLevel,
			"to":         newLevel,
			"cpu":        cpuPercent,
			"memory":     memPercent,
			"goroutines": goroutines,
		}).Warn("[Degradation] Level changed")

		if ad.config.OnLevelChange != nil {
			ad.config.OnLevelChange(currentLevel, newLevel)
		}
	}
}

// setLevel 设置降级级别并更新 metrics
func (ad *AdaptiveDegradation) setLevel(level DegradationLevel) {
	oldLevel := ad.GetLevel()
	ad.level.Store(level)

	// 更新 Prometheus metrics
	ad.metrics.levelGauge.Set(float64(level))

	if level == LevelNormal {
		ad.metrics.activeGauge.Set(0)
	} else {
		ad.metrics.activeGauge.Set(1)

		// 记录触发原因
		reason := "unknown"
		if cpuVal, ok := ad.lastCPU.Load().(float64); ok && cpuVal > ad.config.CPUThreshold {
			reason = "cpu"
			ad.metrics.triggersTotal.WithLabelValues(reason).Inc()
		}
		if memVal, ok := ad.lastMemory.Load().(float64); ok && memVal > ad.config.MemoryThreshold {
			reason = "memory"
			ad.metrics.triggersTotal.WithLabelValues(reason).Inc()
		}
		if ad.config.EnableGoroutineLimit && runtime.NumGoroutine() > ad.config.GoroutineThreshold {
			reason = "goroutines"
			ad.metrics.triggersTotal.WithLabelValues(reason).Inc()
		}
	}

	// 记录恢复事件
	if oldLevel != LevelNormal && level == LevelNormal {
		ad.metrics.recoveriesTotal.WithLabelValues(
			fmt.Sprintf("%d", oldLevel),
			fmt.Sprintf("%d", level),
		).Inc()
	}
}

// calculateLevel 计算降级级别
func (ad *AdaptiveDegradation) calculateLevel(cpuPercent, memPercent float64, goroutines int) DegradationLevel {
	// 计算负载分数（0-100）
	score := 0.0

	// CPU 权重 40%
	if cpuPercent > ad.config.CPUThreshold {
		score += (cpuPercent - ad.config.CPUThreshold) * 0.4
	}

	// 内存权重 40%
	if memPercent > ad.config.MemoryThreshold {
		score += (memPercent - ad.config.MemoryThreshold) * 0.4
	}

	// 协程数量权重 20%
	if ad.config.EnableGoroutineLimit && goroutines > ad.config.GoroutineThreshold {
		goroutinePercent := float64(goroutines-ad.config.GoroutineThreshold) / float64(ad.config.GoroutineThreshold) * 100
		score += goroutinePercent * 0.2
	}

	// 根据分数确定降级级别
	switch {
	case score >= 30:
		return LevelSevere
	case score >= 20:
		return LevelModerate
	case score >= 10:
		return LevelLight
	default:
		return LevelNormal
	}
}

// getCPUUsage 获取 CPU 使用率
func (ad *AdaptiveDegradation) getCPUUsage() float64 {
	percent, err := cpu.Percent(time.Second, false)
	if err != nil || len(percent) == 0 {
		logger.WithError(err).Debug("[Degradation] Failed to get CPU usage")
		return 0
	}
	return percent[0]
}

// getMemoryUsage 获取内存使用率
func (ad *AdaptiveDegradation) getMemoryUsage() float64 {
	v, err := mem.VirtualMemory()
	if err != nil {
		logger.WithError(err).Debug("[Degradation] Failed to get memory usage")
		return 0
	}
	return v.UsedPercent
}

// GetLevel 获取当前降级级别
func (ad *AdaptiveDegradation) GetLevel() DegradationLevel {
	return ad.level.Load().(DegradationLevel)
}

// Middleware 返回降级中间件
func (ad *AdaptiveDegradation) Middleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			ad.totalEvents.Add(1)

			currentLevel := ad.GetLevel()
			if currentLevel == LevelNormal {
				// 正常状态，直���处理
				ad.metrics.eventsTotal.WithLabelValues("processed").Inc()
				return next(ctx)
			}

			// 获取事件优先级
			priority := ad.config.PriorityClassifier(ctx)

			// 根据降级级别和事件优先级决定是否处理
			if ad.shouldDrop(currentLevel, priority) {
				ad.droppedEvents.Add(1)
				ad.metrics.eventsTotal.WithLabelValues("dropped").Inc()
				logger.WithFields(logger.Fields{
					"level":    currentLevel,
					"priority": priority,
					"type":     ctx.GetEventType(),
				}).Debug("[Degradation] Event dropped")
				return nil
			}

			// 根据策略处理
			switch ad.config.Strategy {
			case DegradationDelay:
				if priority < PriorityHigh {
					ad.delayedEvents.Add(1)
					ad.metrics.eventsTotal.WithLabelValues("delayed").Inc()
					// 延迟处理：使用 context-aware 等待，避免 goroutine 在高并发下堆积
					delayTimer := time.NewTimer(100 * time.Millisecond)
					select {
					case <-delayTimer.C:
						// 延迟完成，继续处理
					case <-ctx.Context().Done():
						delayTimer.Stop()
						return ctx.Context().Err()
					}
					delayTimer.Stop()
				}
			case DegradationSimplify:
				// 简化处理（可以在业务层实现）
				SetDegraded(ctx)
			default:
				// DegradationDrop or unknown: no-op here.
			}

			ad.metrics.eventsTotal.WithLabelValues("processed").Inc()
			return next(ctx)
		}
	}
}

// shouldDrop 判断是否应该丢弃事件
func (ad *AdaptiveDegradation) shouldDrop(level DegradationLevel, priority EventPriority) bool {
	switch level {
	case LevelLight:
		// 轻度降级：丢弃低优先级事件
		return priority < PriorityNormal
	case LevelModerate:
		// 中度降级：只处理高优先级事件
		return priority < PriorityHigh
	case LevelSevere:
		// 重度降级：只处理关键事件
		return priority < PriorityCritical
	default:
		return false
	}
}

// defaultPriorityClassifier 默认优先级分类器
func defaultPriorityClassifier(ctx *eventctx.Context) EventPriority {
	eventType := ctx.GetEventType()

	// 根据事件类型分类
	switch eventType {
	case "GROUP_AT_MESSAGE_CREATE":
		// 群 @ 消息（高优先级）
		return PriorityHigh
	case "MESSAGE_CREATE":
		// 普通消息
		return PriorityNormal
	case "C2C_MESSAGE_CREATE":
		// 私聊消息（高优先级）
		return PriorityHigh
	case "GUILD_CREATE", "GUILD_DELETE":
		// 关键系统事件
		return PriorityCritical
	case "GROUP_ADD_ROBOT", "GROUP_DEL_ROBOT":
		// 机器人加入/退出群聊
		return PriorityCritical
	case "INTERACTION_CREATE":
		// 交互事件
		return PriorityHigh
	default:
		return PriorityLow
	}
}

// Stats 获取统计信息
func (ad *AdaptiveDegradation) Stats() DegradationStats {
	total := ad.totalEvents.Load()
	dropped := ad.droppedEvents.Load()
	delayed := ad.delayedEvents.Load()

	var dropRate float64
	if total > 0 {
		dropRate = float64(dropped) / float64(total) * 100
	}

	return DegradationStats{
		Level:         ad.GetLevel(),
		TotalEvents:   total,
		DroppedEvents: dropped,
		DelayedEvents: delayed,
		DropRate:      dropRate,
		CPU:           ad.lastCPU.Load().(float64),
		Memory:        ad.lastMemory.Load().(float64),
		Goroutines:    runtime.NumGoroutine(),
	}
}

// DegradationStats 降级统计信息
type DegradationStats struct {
	Level         DegradationLevel
	TotalEvents   int64
	DroppedEvents int64
	DelayedEvents int64
	DropRate      float64
	CPU           float64
	Memory        float64
	Goroutines    int
}

// Reset 重置统计信息
func (ad *AdaptiveDegradation) Reset() {
	ad.totalEvents.Store(0)
	ad.droppedEvents.Store(0)
	ad.delayedEvents.Store(0)
}

// UpdateConfig 热更新降级控制器配置（线程安全，下一个监控周期生效）。
//
// 支持运行时更新：CPUThreshold、MemoryThreshold、LatencyThreshold、
// GoroutineThreshold、EnableGoroutineLimit。
// 不支持热更新：MonitorInterval、RecoveryInterval、Strategy、PriorityClassifier
// （这些参数修改需要重建控制器）。
func (ad *AdaptiveDegradation) UpdateConfig(cfg DegradationConfig) {
	// AdaptiveDegradation 的 config 字段整体替换是安全的：
	// checkAndAdjustLevel 读取 config 字段均为值类型（float64/Duration/bool/int），
	// Go 赋值是原子的对于值类型结构，但为保险起见用单独字段赋值。
	if cfg.CPUThreshold > 0 {
		ad.config.CPUThreshold = cfg.CPUThreshold
	}
	if cfg.MemoryThreshold > 0 {
		ad.config.MemoryThreshold = cfg.MemoryThreshold
	}
	if cfg.LatencyThreshold > 0 {
		ad.config.LatencyThreshold = cfg.LatencyThreshold
	}
	if cfg.GoroutineThreshold > 0 {
		ad.config.GoroutineThreshold = cfg.GoroutineThreshold
	}
	if cfg.EnableGoroutineLimit {
		ad.config.EnableGoroutineLimit = cfg.EnableGoroutineLimit
	}
}

// ForceLevel 强制设置降级级别（用于测试或手动控制）
func (ad *AdaptiveDegradation) ForceLevel(level DegradationLevel) {
	oldLevel := ad.GetLevel()
	ad.setLevel(level)
	logger.WithFields(logger.Fields{
		"from": oldLevel,
		"to":   level,
	}).Info("[Degradation] Force level change")

	if ad.config.OnLevelChange != nil {
		ad.config.OnLevelChange(oldLevel, level)
	}
}
