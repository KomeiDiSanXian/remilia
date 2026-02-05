package main

import (
	"fmt"
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// 性能监控示例
// 展示如何监控Bot的性能指标

type Metrics struct {
	totalRequests   atomic.Int64
	successRequests atomic.Int64
	failedRequests  atomic.Int64
	totalLatency    atomic.Int64 // 微秒
	minLatency      atomic.Int64 // 微秒
	maxLatency      atomic.Int64 // 微秒
}

var metrics = &Metrics{}

func main() {
	// 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化日志
	logCfg := logger.Config{
		Level:      cfg.Log.Level,
		Console:    true,
		File:       false,
		TimeFormat: "2006-01-02 15:04:05",
	}
	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}

	// 创建 BotInfo
	botInfo := &dto.BotInfo{
		QQNum:     cfg.Bot.BotID,
		AppID:     cfg.Bot.AppID,
		Token:     cfg.Bot.Token,
		AppSecret: cfg.Bot.Secret,
	}

	// 创建 Bot
	bot, err := remilia.NewBotBuilder().
		WithBotInfo(botInfo).
		WithWebhook(":8080").
		WithName("metrics-monitoring").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用开发环境中间件 + 性能监控中间件
	bot.Engine().Use(middleware.DevelopmentSet()...)
	bot.Engine().Use(metricsMiddleware())
	bot.Engine().Use(performanceMonitoringMiddleware())

	// 注册处理器
	registerHandlers(bot)

	// 启动性能指标定期报告
	go reportMetricsPeriodically()

	// 启动系统监控
	go monitorSystemResources()

	logger.Info("[Metrics] Bot started! Try these commands:")
	logger.Info("[Metrics] /ping - 快速响应")
	logger.Info("[Metrics] /slow - 慢速响应")
	logger.Info("[Metrics] /stats - 查看统计信息")
	logger.Info("[Metrics] /health - 健康检查")

	bot.Start()
	bot.WaitForShutdown()
}

func registerHandlers(bot *remilia.Bot) {
	// 快速响应命令
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/ping").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: "Pong! ⚡",
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	// 慢速响应命令
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/slow").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		// 模拟慢速操作
		time.Sleep(2 * time.Second)

		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: "Slow response 🐌",
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	// 统计信息命令
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/stats").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		stats := getMetricsReport()
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: stats,
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	// 健康检查命令
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/health").Handle(func(ctx *eventctx.Context) error {
		var c2c dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&c2c); err != nil {
			return err
		}

		health := getHealthReport()
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: health,
		}
		_, err := ctx.ReplyPrivate(msg)
		return err
	})

	logger.Info("[Metrics] Handlers registered")
}

// metricsMiddleware 性能指标收集中间件
func metricsMiddleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()

			// 增加总请求数
			metrics.totalRequests.Add(1)

			// 执行处理器
			err := next(ctx)

			// 计算延迟
			latency := time.Since(start).Microseconds()
			metrics.totalLatency.Add(latency)

			// 更新最小/最大延迟
			updateLatencyBounds(latency)

			// 更新成功/失败计数
			if err != nil {
				metrics.failedRequests.Add(1)
			} else {
				metrics.successRequests.Add(1)
			}

			return err
		}
	}
}

// performanceMonitoringMiddleware 性能监控中间件
func performanceMonitoringMiddleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)
			duration := time.Since(start)

			// 记录慢请求
			if duration > 1*time.Second {
				logger.WithFields(logger.Fields{
					"duration": duration,
				}).Warn("[Metrics] Slow request detected")
			}

			return err
		}
	}
}

// updateLatencyBounds 更新延迟边界
func updateLatencyBounds(latency int64) {
	// 更新最小延迟
	for {
		current := metrics.minLatency.Load()
		if current == 0 || latency < current {
			if metrics.minLatency.CompareAndSwap(current, latency) {
				break
			}
		} else {
			break
		}
	}

	// 更新最大延迟
	for {
		current := metrics.maxLatency.Load()
		if latency > current {
			if metrics.maxLatency.CompareAndSwap(current, latency) {
				break
			}
		} else {
			break
		}
	}
}

// getMetricsReport 获取性能指标报告
func getMetricsReport() string {
	total := metrics.totalRequests.Load()
	success := metrics.successRequests.Load()
	failed := metrics.failedRequests.Load()
	totalLatency := metrics.totalLatency.Load()
	minLatency := metrics.minLatency.Load()
	maxLatency := metrics.maxLatency.Load()

	var avgLatency int64
	if total > 0 {
		avgLatency = totalLatency / total
	}

	successRate := float64(0)
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	report := "📊 性能指标报告\n\n"
	report += "请求统计:\n"
	report += fmt.Sprintf("  总请求: %d\n", total)
	report += fmt.Sprintf("  成功: %d\n", success)
	report += fmt.Sprintf("  失败: %d\n", failed)
	report += fmt.Sprintf("  成功率: %.2f%%\n\n", successRate)
	report += "延迟统计:\n"
	report += fmt.Sprintf("  平均延迟: %d μs\n", avgLatency)
	report += fmt.Sprintf("  最小延迟: %d μs\n", minLatency)
	report += fmt.Sprintf("  最大延迟: %d μs\n", maxLatency)

	return report
}

// getHealthReport 获取健康检查报告
func getHealthReport() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	report := "🏥 健康检查报告\n\n"
	report += "系统状态:\n"
	report += fmt.Sprintf("  Goroutines: %d\n", runtime.NumGoroutine())
	report += fmt.Sprintf("  内存分配: %.2f MB\n", float64(m.Alloc)/1024/1024)
	report += fmt.Sprintf("  总分配: %.2f MB\n", float64(m.TotalAlloc)/1024/1024)
	report += fmt.Sprintf("  系统内存: %.2f MB\n", float64(m.Sys)/1024/1024)
	report += fmt.Sprintf("  GC次数: %d\n", m.NumGC)

	return report
}

// reportMetricsPeriodically 定期报告性能指标
func reportMetricsPeriodically() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		total := metrics.totalRequests.Load()
		success := metrics.successRequests.Load()
		failed := metrics.failedRequests.Load()

		logger.WithFields(logger.Fields{
			"total_requests":   total,
			"success_requests": success,
			"failed_requests":  failed,
		}).Info("[Metrics] Periodic report")
	}
}

// monitorSystemResources 监控系统资源
func monitorSystemResources() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		goroutines := runtime.NumGoroutine()
		memAlloc := float64(m.Alloc) / 1024 / 1024

		// 记录资源使用情况
		logger.WithFields(logger.Fields{
			"goroutines": goroutines,
			"memory_mb":  memAlloc,
			"gc_count":   m.NumGC,
		}).Debug("[Metrics] System resources")

		// 检测异常情况
		if goroutines > 1000 {
			logger.WithFields(logger.Fields{
				"goroutines": goroutines,
			}).Warn("[Metrics] High goroutine count")
		}

		if memAlloc > 500 {
			logger.WithFields(logger.Fields{
				"memory_mb": memAlloc,
			}).Warn("[Metrics] High memory usage")
		}
	}
}
