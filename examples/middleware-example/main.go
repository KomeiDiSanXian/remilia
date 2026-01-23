//go:build example
// +build example

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetLevel(logrus.InfoLevel)

	// 创建 Engine
	eng := engine.NewEngine()

	// ===== 1. 基础中间件 =====
	eng.Use(
		middleware.Logging(),   // 日志记录
		middleware.Recover(),   // Panic 恢复
		middleware.RequestID(), // 请求 ID
	)

	// ===== 2. 并发限制中间件 =====
	// 限制最大并发数为 100
	eng.Use(middleware.ConcurrencyLimit(
		100,                        // 最大并发数
		middleware.ConcurrencyDrop, // 超过限制时丢弃请求
		0,                          // 无超时
	))

	// ===== 3. 超时中间件 =====
	// 所有处理器必须在 5 秒内完成
	eng.Use(middleware.Timeout(5 * time.Second))

	// ===== 4. 重试中间件 =====
	// 失败时重试最多 3 次
	eng.Use(middleware.Retry(3))

	// ===== 5. 熔断器中间件 =====
	// 连续失败 5 次后熔断
	eng.Use(middleware.CircuitBreaker(
		5,              // 失败阈值
		30*time.Second, // 半开状态超时
	))

	// ===== 6. 降级中间件 =====
	// 系统过载时自动降级
	degradation := middleware.NewAdaptiveDegradation(
		middleware.AdaptiveDegradationConfig{
			ErrorRateThreshold:    0.5,             // 50% 错误率触发降级
			LatencyThreshold:      1 * time.Second, // 1秒延迟触发降级
			SamplingWindow:        1 * time.Minute,
			MinSamplesForDecision: 10,
		},
	)
	eng.Use(degradation.Middleware())

	// ===== 7. 死信队列中间件 =====
	// 失败消息进入死信队列
	dlqConfig := middleware.DeadLetterConfig{
		MaxRetries: 3,
		RetryDelay: 5 * time.Second,
		QueueSize:  1000,
	}
	dlq := middleware.NewDeadLetterQueue(dlqConfig)
	eng.Use(dlq.Middleware())

	// ===== 8. 去重中间件 =====
	// 防止重复消息处理
	dedup := middleware.NewDeduplicator(
		1*time.Minute, // 去重窗口
		10000,         // 最大记录数
	)
	eng.Use(dedup.Middleware())

	// ===== 9. 自适应限流中间件 =====
	// 根据系统负载自动调整并发限制
	adaptiveConfig := middleware.DefaultAdaptiveConfig()
	adaptiveConfig.MinConcurrency = 10
	adaptiveConfig.MaxConcurrency = 200
	adaptiveConfig.InitialLimit = 50

	adaptiveLimiter := middleware.NewAdaptiveRateLimiter(adaptiveConfig)
	adaptiveLimiter.Start()
	defer adaptiveLimiter.Stop()

	eng.Use(adaptiveLimiter.Middleware())

	// ===== 10. Prometheus 指标中间件 =====
	prometheusConfig := middleware.PrometheusConfig{
		Namespace: "remilia",
		Subsystem: "bot",
	}
	eng.Use(middleware.PrometheusMetrics(prometheusConfig))

	// ===== 11. 慢处理器检测中间件 =====
	// 检测超过 1 秒的慢处理器
	eng.Use(middleware.SlowHandlerDetector(1 * time.Second))

	// 注册处理器
	registerHandlers(eng)

	// 注册自定义中间件
	registerCustomMiddleware(eng)

	// 演示定期打印统计
	go printStats(degradation, dedup, dlq, adaptiveLimiter)

	// 创建并启动 Bot
	secret := getEnv("BOT_SECRET", "your-webhook-secret")
	port := getEnv("BOT_PORT", "8080")

	adapter := remilia.NewWebhookAdapter(":"+port, secret)
	bot := remilia.NewBot(adapter, eng)

	logrus.Info("Starting bot with middleware...")
	if err := bot.Start(); err != nil {
		logrus.Fatal(err)
	}

	// 启动 Prometheus HTTP 服务器
	go startPrometheusServer()

	bot.WaitForShutdown()
}

func registerHandlers(eng *engine.Engine) {
	// 正常命令
	eng.OnCommand("/hello", func(ctx *eventctx.Context) error {
		return ctx.Reply("Hello!")
	})

	// 慢命令（演示超时和慢处理器检测）
	eng.OnCommand("/slow", func(ctx *eventctx.Context) error {
		time.Sleep(2 * time.Second)
		return ctx.Reply("Completed after 2 seconds")
	})

	// 失败命令（演示重试和熔断）
	eng.OnCommand("/fail", func(ctx *eventctx.Context) error {
		return fmt.Errorf("intentional failure")
	})

	// Panic 命令（演示 Recover 中间件）
	eng.OnCommand("/panic", func(ctx *eventctx.Context) error {
		panic("intentional panic")
	})

	// 高负载命令（演示降级）
	eng.OnCommand("/heavy", func(ctx *eventctx.Context) error {
		// 模拟高负载
		time.Sleep(500 * time.Millisecond)
		return ctx.Reply("Heavy computation done")
	})
}

func registerCustomMiddleware(eng *engine.Engine) {
	// 自定义鉴权中间件
	authMiddleware := func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			author := ctx.GetAuthor()

			// 检查黑名单
			if isBlacklisted(author) {
				logrus.WithField("author", author).Warn("Blocked blacklisted user")
				return nil // 静默丢弃
			}

			return next(ctx)
		}
	}
	eng.Use(authMiddleware)

	// 自定义计数中间件
	var messageCount int64
	counterMiddleware := func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			messageCount++
			if messageCount%100 == 0 {
				logrus.WithField("count", messageCount).Info("Processed messages")
			}
			return next(ctx)
		}
	}
	eng.Use(counterMiddleware)

	// 自定义响应时间记录
	responseTimeMiddleware := func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			start := time.Now()
			err := next(ctx)
			duration := time.Since(start)

			if duration > 100*time.Millisecond {
				logrus.WithFields(logrus.Fields{
					"duration": duration,
					"type":     ctx.GetEventType(),
				}).Warn("Slow handler detected")
			}

			return err
		}
	}
	eng.Use(responseTimeMiddleware)
}

func printStats(
	degradation *middleware.AdaptiveDegradation,
	dedup *middleware.Deduplicator,
	dlq *middleware.DeadLetterQueue,
	adaptive *middleware.AdaptiveRateLimiter,
) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		logrus.Info("===== Middleware Statistics =====")

		// 降级统计
		degradationStats := degradation.GetStats()
		logrus.WithFields(logrus.Fields{
			"level":       degradationStats.CurrentLevel,
			"error_rate":  fmt.Sprintf("%.2f%%", degradationStats.ErrorRate*100),
			"avg_latency": degradationStats.AvgLatency,
		}).Info("Degradation")

		// 去重统计
		dedupStats := dedup.GetStats()
		logrus.WithFields(logrus.Fields{
			"duplicates": dedupStats.DuplicateCount,
			"processed":  dedupStats.ProcessedCount,
		}).Info("Deduplication")

		// 死信队列统计
		dlqStats := dlq.GetStats()
		logrus.WithFields(logrus.Fields{
			"queued":   dlqStats.QueueSize,
			"retrying": dlqStats.RetryingCount,
			"failed":   dlqStats.PermanentFailures,
		}).Info("Dead Letter Queue")

		// 自适应限流统计
		adaptiveStats := adaptive.GetStats()
		logrus.WithFields(logrus.Fields{
			"limit":    adaptiveStats.CurrentLimit,
			"load":     adaptiveStats.CurrentLoad,
			"rejected": adaptiveStats.RejectedRequests,
			"cpu":      fmt.Sprintf("%.2f%%", adaptiveStats.CPUUsage*100),
		}).Info("Adaptive Rate Limiter")
	}
}

func startPrometheusServer() {
	// 启动 Prometheus HTTP 服务器在 9090 端口
	// 访问 http://localhost:9090/metrics 查看指标
	logrus.Info("Prometheus metrics available at :9090/metrics")
	// 实际实现需要导入 prometheus HTTP handler
}

func isBlacklisted(userID string) bool {
	// 示例黑名单逻辑
	blacklist := []string{"spam-user", "bad-actor"}
	for _, id := range blacklist {
		if id == userID {
			return true
		}
	}
	return false
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
