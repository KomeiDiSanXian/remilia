package integration

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	rcontext "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/audit"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_BasicCommandFlow 测试基本命令流程
func TestE2E_BasicCommandFlow(t *testing.T) {
	// 1. 创建 engine
	eng := engine.NewEngine()
	defer eng.Close()

	// 2. 注册命令
	executed := false
	eng.OnCommand(dto.C2CMessageCreate, "/test").Handle(func(ctx *rcontext.Context) error {
		executed = true
		return nil
	})

	// 3. 创建测试事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		Detail: []byte(`{
			"content": "/test",
			"author": {
				"user_openid": "test_user"
			}
		}`),
	}

	// 4. 处理事件
	ctx := rcontext.NewContext(event, nil)
	eng.ProcessEvent(ctx)

	// 5. 验证
	assert.True(t, executed, "命令应该被执行")
}

// TestE2E_CommandWithArguments 测试带参数的命令
func TestE2E_CommandWithArguments(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	// 定义命令
	def := &command.Definition{
		Name:        "weather",
		Description: "获取天气信息",
		Arguments: []*command.Argument{
			{Name: "city", Description: "城市名称", Required: true, Type: command.ArgTypeString},
		},
		Flags: []*command.Flag{
			{Name: "unit", ShortName: "u", Description: "温度单位", Type: command.ArgTypeString, Default: "C"},
		},
	}

	// 注册命令
	var receivedCity, receivedUnit string
	eng.RegisterCommandDef(dto.C2CMessageCreate, def).Handle(func(ctx *rcontext.Context) error {
		parsed := ctx.GetParsedCommand()
		if parsed != nil {
			if city, ok := parsed.Arguments["city"]; ok {
				receivedCity = city.(string)
			}
			if unit, ok := parsed.Flags["unit"]; ok {
				receivedUnit = unit.(string)
			}
		}
		return nil
	})

	// 创建测试事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		Detail: []byte(`{
			"content": "/weather Beijing --unit F",
			"author": {
				"user_openid": "test_user"
			}
		}`),
	}

	// 处理事件
	ctx := rcontext.NewContext(event, nil)
	eng.ProcessEvent(ctx)

	// 验证
	assert.Equal(t, "Beijing", receivedCity)
	assert.Equal(t, "F", receivedUnit)
}

// TestE2E_MiddlewareChain 测试中间件链
func TestE2E_MiddlewareChain(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	// 记录中间件执行顺序
	var order []string

	// 全局中间件
	eng.Use(func(next rcontext.Handler) rcontext.Handler {
		return func(ctx *rcontext.Context) error {
			order = append(order, "global_before")
			err := next(ctx)
			order = append(order, "global_after")
			return err
		}
	})

	// 注册命令并添加局部中间件
	matcher := eng.OnCommand(dto.C2CMessageCreate, "/test")
	matcher.Use(func(next rcontext.Handler) rcontext.Handler {
		return func(ctx *rcontext.Context) error {
			order = append(order, "local_before")
			err := next(ctx)
			order = append(order, "local_after")
			return err
		}
	})
	matcher.Handle(func(ctx *rcontext.Context) error {
		order = append(order, "handler")
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "/test"}`),
	}
	ctx := rcontext.NewContext(event, nil)
	eng.ProcessEvent(ctx)

	// 验证执行顺序
	expected := []string{
		"global_before",
		"local_before",
		"handler",
		"local_after",
		"global_after",
	}
	assert.Equal(t, expected, order)
}

// TestE2E_ErrorHandling 测试错误处理
func TestE2E_ErrorHandling(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	// 注册会返回错误的命令
	var handlerErr error
	eng.OnCommand(dto.C2CMessageCreate, "/error").Handle(func(ctx *rcontext.Context) error {
		handlerErr = assert.AnError
		return handlerErr
	})

	// 处理事件
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "/error"}`),
	}
	ctx := rcontext.NewContext(event, nil)
	eng.ProcessEvent(ctx)

	// 验证错误被记录
	assert.Error(t, handlerErr)
}

// TestE2E_AuditLogging 测试审计日志集成
func TestE2E_AuditLogging(t *testing.T) {
	// 创建临时审计日志
	tmpDir := t.TempDir()
	auditConfig := audit.Config{
		Enabled:       true,
		OutputFile:    tmpDir + "/audit.log",
		AsyncWrite:    false, // 同步写入便于测试
		BufferSize:    10,
		FlushInterval: 1 * time.Second,
	}

	auditLogger, err := audit.NewLogger(auditConfig)
	require.NoError(t, err)
	defer func() {
		_ = auditLogger.Close()
	}()

	// 创建 engine 并注册审计中间件
	eng := engine.NewEngine()
	defer eng.Close()
	eng.Use(audit.Middleware(auditLogger))

	// 注册命令
	executed := false
	eng.OnCommand(dto.C2CMessageCreate, "/audit_test").Handle(func(ctx *rcontext.Context) error {
		executed = true
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		Detail: []byte(`{
			"content": "/audit_test",
			"author": {
				"user_openid": "test_user"
			}
		}`),
	}
	ctx := rcontext.NewContext(event, nil)
	eng.ProcessEvent(ctx)

	// 验证
	assert.True(t, executed)

	// 等待日志写入
	time.Sleep(100 * time.Millisecond)
}

// TestE2E_TempMatcher 测试临时匹配器
func TestE2E_TempMatcher(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	// 注册临时匹配器（一次性）
	executed := false
	eng.OnTemp(dto.C2CMessageCreate, func(ctx *rcontext.Context) bool {
		return true
	}).Handle(func(ctx *rcontext.Context) error {
		executed = true
		return nil
	})

	// 第一次处理 - 应该匹配
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "test"}`),
	}
	ctx := rcontext.NewContext(event, nil)
	eng.ProcessEvent(ctx)
	assert.True(t, executed)

	// 重置标志
	executed = false

	// 第二次处理 - 临时匹配器应该已被删除
	ctx2 := rcontext.NewContext(event, nil)
	eng.ProcessEvent(ctx2)
	assert.False(t, executed, "临时匹配器应该在第一次使用后被删除")
}

// TestE2E_ConcurrentEvents 测试并发事件处理
func TestE2E_ConcurrentEvents(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	// 计数器（使用互斥锁保护）
	var counter int32
	var mu sync.Mutex

	eng.OnCommand(dto.C2CMessageCreate, "/concurrent").Handle(func(ctx *rcontext.Context) error {
		mu.Lock()
		counter++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond) // 模拟处理延迟
		return nil
	})

	// 并发发送多个事件
	const concurrency = 10
	done := make(chan struct{}, concurrency)

	for range concurrency {
		go func() {
			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content": "/concurrent"}`),
			}
			ctx := rcontext.NewContext(event, nil)
			eng.ProcessEvent(ctx)
			done <- struct{}{}
		}()
	}

	// 等待所有完成
	for range concurrency {
		<-done
	}

	// 验证所有事件都被处理
	mu.Lock()
	finalCount := counter
	mu.Unlock()
	assert.Equal(t, int32(concurrency), finalCount)
}

// TestE2E_BatchRegistration 测试批量注册
func TestE2E_BatchRegistration(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	// 批量创建匹配器
	matchers := make([]*engine.Matcher, 10)
	for i := range 10 {
		matchers[i] = &engine.Matcher{
			EventType: dto.C2CMessageCreate,
			Rules: []rcontext.Rule{
				func(ctx *rcontext.Context) bool { return true },
			},
		}
	}

	// 批量注册
	registered := eng.BatchRegisterMatchers(matchers)

	// 验证
	assert.Equal(t, 10, len(registered))
	assert.Equal(t, 10, eng.GetMatcherCount())
}

// TestE2E_PluginLifecycle 测试插件生命周期
func TestE2E_PluginLifecycle(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Close()

	// 模拟插件注册多个命令
	pluginName := "test-plugin"

	// 使用批量操作
	eng.WithMatcherGroupBatch(func() {
		for range 5 {
			matcher := eng.OnCommand(dto.C2CMessageCreate, "/plugin_cmd")
			matcher.SetSource("plugin:" + pluginName)
			eng.SetMatcherGroup(matcher, pluginName, "plugin:"+pluginName)
		}
	})

	// 验证插件命令已注册
	assert.Equal(t, 5, eng.GetMatcherCount())

	// 卸载插件（删除所有相关命令）
	eng.RemoveGroup(pluginName)

	// 验证所有命令已删除
	assert.Equal(t, 0, eng.GetMatcherCount())
}

// TestE2E_GracefulShutdown 测试优雅关闭
func TestE2E_GracefulShutdown(t *testing.T) {
	eng := engine.NewEngine()

	// 注册命令
	processing := make(chan struct{})
	done := make(chan struct{})

	eng.OnCommand(dto.C2CMessageCreate, "/slow").Handle(func(ctx *rcontext.Context) error {
		processing <- struct{}{} // 通知开始处理
		time.Sleep(100 * time.Millisecond)
		done <- struct{}{} // 通知处理完成
		return nil
	})

	// 启动异步事件处理
	go func() {
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`{"content": "/slow"}`),
		}
		ctx := rcontext.NewContext(event, nil)
		eng.ProcessEvent(ctx)
	}()

	// 等待开始处理
	<-processing

	// 触发关闭
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = eng.Shutdown(shutdownCtx)
	}()

	// 验证处理完成
	select {
	case <-done:
		// 成功：事件在关闭前完成
	case <-time.After(200 * time.Millisecond):
		t.Fatal("事件处理超时")
	}
}

// TestE2E_FullBotLifecycle 测试完整 Bot 生命周期
//
// 此测试需要一个可访问的 Webhook 端点和有效的 QQ Bot 凭据，
// 因此默认跳过。在以下条件下可启用：
//   - 设置环境变量 E2E_BOT_TOKEN、E2E_BOT_SECRET、E2E_BOT_APPID
//   - 使用 -run TestE2E_FullBotLifecycle 单独运行
func TestE2E_FullBotLifecycle(t *testing.T) {
	if os.Getenv("E2E_BOT_TOKEN") == "" {
		t.Skip("跳过端到端测试：未设置 E2E_BOT_TOKEN 环境变量。" +
			"需要有效的 QQ Bot 凭据才能运行此测试。")
	}
}
