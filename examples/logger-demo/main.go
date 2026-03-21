package main

import (
	"context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	qqplatform "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func main() {
	// 初始化日志 - 同时输出到终端（彩色）和文件
	err := logger.Init(logger.Config{
		Level:      "debug",               // 日志级别: trace, debug, info, warn, error, fatal, panic
		Console:    true,                  // 启用终端输出（彩色）
		File:       true,                  // 启用文件输出
		FilePath:   "logs/bot.log",        // 日志文件路径
		TimeFormat: "2006-01-02 15:04:05", // 时间格式
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}

	logger.Info("Logger initialized successfully")
	logger.Info("Bot starting...")

	// 创建 engine
	eng := engine.NewEngine()

	// 创建适配器 - 这里使用模拟适配器作为示例
	mockAdapter := &MockAdapter{}

	// 创建 Bot
	bot := remilia.NewBot(mockAdapter, eng)

	// 注册一个简单的消息处理器
	eng.OnAny().Handle(func(ctx *eventctx.Context) error {
		// 使用 logger.WithFields 添加结构化字段
		eventID := ""
		if pe := ctx.GetPlatformEvent(); pe != nil {
			eventID = pe.ID()
		}
		logger.WithFields(logger.Fields{
			"user_id":  "example_user",
			"event_id": eventID,
		}).Info("Received C2C message")

		return nil
	})

	// 启动 Bot
	if err := bot.Start(); err != nil {
		logger.WithError(err).Error("Failed to start bot")
		return
	}

	logger.Info("Bot started, press Ctrl+C to stop")

	// 等待停止信号
	bot.WaitForShutdown()

	logger.Info("Bot stopped")
}

// MockAdapter 模拟适配器（实现 engine.Adapter）
type MockAdapter struct{}

func (m *MockAdapter) Platform() string        { return "qq" }
func (m *MockAdapter) Sender() platform.Sender { return &platform.NoopSender{} }
func (m *MockAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}

func (m *MockAdapter) Start(ctx context.Context, handler func(platform.Event)) error {
	logger.Info("[MockAdapter] Starting...")
	go func() {
		time.Sleep(1 * time.Second)
		handler(qqplatform.NewEvent(&dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test_event_1",
		}))
	}()
	return nil
}

func (m *MockAdapter) Stop(ctx context.Context) error {
	logger.Info("[MockAdapter] Stopping...")
	return nil
}

func (m *MockAdapter) IsRunning() bool { return false }
