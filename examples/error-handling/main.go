package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// 错误处理示例
// 展示如何优雅地处理各种错误情况

// 自定义错误类型
var (
	ErrInvalidInput      = errors.New("invalid input")
	ErrPermissionDenied  = errors.New("permission denied")
	ErrResourceNotFound  = errors.New("resource not found")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

type UserError struct {
	Code    int
	Message string
	Err     error
}

func (e *UserError) Error() string {
	return fmt.Sprintf("code=%d msg=%s err=%v", e.Code, e.Message, e.Err)
}

func (e *UserError) Unwrap() error {
	return e.Err
}

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
		WithName("error-handling").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用开发环境中间件 + 自定义错误处理
	bot.Engine().Use(middleware.DevelopmentSet()...)
	bot.Engine().Use(customErrorHandlerMiddleware())

	// 注册各种错误场景的处理器
	registerHandlers(bot)

	logger.Info("[ErrorHandling] Bot started! Try these commands:")
	logger.Info("[ErrorHandling] /success - 成功响应")
	logger.Info("[ErrorHandling] /error - 一般错误")
	logger.Info("[ErrorHandling] /panic - Panic错误")
	logger.Info("[ErrorHandling] /invalid - 无效输入错误")
	logger.Info("[ErrorHandling] /notfound - 资源不存在错误")
	logger.Info("[ErrorHandling] /permission - 权限错误")

	bot.Start()
	bot.WaitForShutdown()
}

func registerHandlers(bot *remilia.Bot) {
	// 1. 成功场景
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/success").Handle(func(ctx *eventctx.Context) error {
		return ctx.Reply(platform.TextMessage("✅ Success! Everything works fine."))
	})

	// 2. 一般错误
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/error").Handle(func(ctx *eventctx.Context) error {
		err := errors.New("something went wrong")
		return handleError(err, "Business logic error")
	})

	// 3. Panic场景（会被Recover中间件捕获）
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/panic").Handle(func(ctx *eventctx.Context) error {
		panic("intentional panic for testing")
	})

	// 4. 无效输入错误
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/invalid").Handle(func(ctx *eventctx.Context) error {
		err := &UserError{
			Code:    400,
			Message: "Invalid input provided",
			Err:     ErrInvalidInput,
		}
		logger.WithError(err).Warn("[ErrorHandling] Invalid input")
		_ = ctx.Reply(platform.TextMessage("❌ 错误: 输入无效，请检查后重试"))
		return err
	})

	// 5. 资源不存在错误
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/notfound").Handle(func(ctx *eventctx.Context) error {
		err := &UserError{
			Code:    404,
			Message: "Resource not found",
			Err:     ErrResourceNotFound,
		}
		logger.WithError(err).Info("[ErrorHandling] Resource not found")
		_ = ctx.Reply(platform.TextMessage("❌ 错误: 找不到请求的资源"))
		return err
	})

	// 6. 权限错误
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/permission").Handle(func(ctx *eventctx.Context) error {
		userID := ctx.GetSenderInfo().ID
		if !checkPermission(userID) {
			err := &UserError{
				Code:    403,
				Message: "Permission denied",
				Err:     ErrPermissionDenied,
			}
			logger.WithFields(logger.Fields{
				"user": userID,
			}).Warn("[ErrorHandling] Permission denied")
			_ = ctx.Reply(platform.TextMessage("❌ 错误: 权限不足"))
			return err
		}
		return nil
	})

	// 7. 重试场景
	bot.Engine().OnCommand(dto.C2CMessageCreate, "/retry").Handle(func(ctx *eventctx.Context) error {
		err := retryOperation(func() error {
			return simulateUnstableOperation()
		}, 3)
		if err != nil {
			_ = ctx.Reply(platform.TextMessage("❌ 操作失败: " + err.Error()))
			return err
		}
		return ctx.Reply(platform.TextMessage("✅ 操作成功（经过重试）"))
	})

	logger.Info("[ErrorHandling] Handlers registered")
}

// handleError 统一错误处理函数
func handleError(err error, context string) error {
	if err == nil {
		return nil
	}

	// 记录错误
	logger.WithError(err).WithFields(logger.Fields{
		"context": context,
	}).Error("[ErrorHandling] Error occurred")

	// 这里可以添加错误上报到监控系统
	// reportError(err, context)

	// 包装错误
	return fmt.Errorf("%s: %w", context, err)
}

// checkPermission 检查用户权限（模拟）
func checkPermission(userID string) bool {
	// 实际应用中应该查询权限系统
	return false // 总是返回false用于演示
}

// retryOperation 重试操作
func retryOperation(fn func() error, maxRetries int) error {
	var err error
	for i := range maxRetries {
		err = fn()
		if err == nil {
			if i > 0 {
				logger.WithFields(logger.Fields{
					"attempt": i + 1,
				}).Info("[ErrorHandling] Retry succeeded")
			}
			return nil
		}

		logger.WithFields(logger.Fields{
			"attempt": i + 1,
			"error":   err.Error(),
		}).Warn("[ErrorHandling] Retry attempt failed")
	}

	return fmt.Errorf("operation failed after %d retries: %w", maxRetries, err)
}

// simulateUnstableOperation 模拟不稳定的操作
func simulateUnstableOperation() error {
	// 这里可以模拟随机失败
	// 实际应用中这可能是网络请求、数据库操作等
	return nil // 简化演示，总是成功
}

// customErrorHandlerMiddleware 自定义错误处理中间件
func customErrorHandlerMiddleware() eventctx.Middleware {
	return func(next eventctx.Handler) eventctx.Handler {
		return func(ctx *eventctx.Context) error {
			err := next(ctx)
			if err != nil {
				// 根据错误类型进行不同处理
				var userErr *UserError
				if errors.As(err, &userErr) {
					logger.WithFields(logger.Fields{
						"code":    userErr.Code,
						"message": userErr.Message,
					}).Error("[ErrorHandling] User error")
				} else if errors.Is(err, ErrInvalidInput) {
					logger.Error("[ErrorHandling] Invalid input error")
				} else if errors.Is(err, ErrResourceNotFound) {
					logger.Error("[ErrorHandling] Resource not found")
				} else {
					logger.WithError(err).Error("[ErrorHandling] Unexpected error")
				}
			}
			return err
		}
	}
}
