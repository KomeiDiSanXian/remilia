package main

import (
	"context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/tracing"
)

func main() {
	// 初始化日志
	_ = logger.Init(logger.Config{
		Level:   "info",
		Console: true,
	})

	logger.Info("=== Remilia 分布式追踪示例 ===")

	// 1. 初始化追踪提供者
	tracingConfig := tracing.Config{
		Enable:         true,
		ServiceName:    "remilia-tracing-example",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		Exporter:       "stdout", // 使用控制台输出，便于查看
		SamplingRate:   1.0,      // 100% 采样
	}

	tracingProvider, err := tracing.NewProvider(tracingConfig)
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize tracing")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracingProvider.Shutdown(ctx); err != nil {
			logger.WithError(err).Error("Failed to shutdown tracing")
		}
	}()

	logger.Info("✅ Tracing initialized successfully")

	// 2. 获取 Tracer 并创建示例 span
	tracer := tracingProvider.Tracer("example")

	ctx := context.Background()
	_, span := tracer.Start(ctx, "demo-operation")
	defer span.End()

	logger.Info("✅ Created demo span")

	// 3. 模拟一些操作
	time.Sleep(100 * time.Millisecond)

	// 打印使用说明
	fmt.Println("\n" + `
╔════════════════════════════════════════════════════════════════╗
║        Remilia 分布式追踪示例                                     ║
╠════════════════════════════════════════════════════════════════╣
║                                                                ║
║  ✅ 追踪已成功初始化！                                            ║
║                                                                ║
║  在实际 Bot 中使用：                                              ║
║                                                                ║
║  1. 在 config.yaml 中配置 tracing:                              ║
║     tracing:                                                   ║
║       enable: true                                             ║
║       service_name: "my-bot"                                   ║
║       exporter: "otlp"                                         ║
║       endpoint: "http://localhost:4318"                        ║
║       sampling_rate: 0.1                                       ║
║                                                                ║
║  2. 添加追踪中间件到 engine:                                      ║
║     engine.Use(middleware.Tracing(                             ║
║         middleware.DefaultTracingConfig()                      ║
║     ))                                                         ║
║                                                                ║
║  3. 支持的追踪后端：                                              ║
║     - Grafana Tempo (推荐)                                     ║
║     - Zipkin                                                   ║
║     - Grafana Cloud                                            ║
║                                                                ║
║  4. 查看文档了解更多：                                            ║
║     docs/02-user-guides/tracing.md                             ║
║                                                                ║
╚════════════════════════════════════════════════════════════════╝`)

	logger.Info("示例运行完成")
}
