package remilia

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestContext_Context 测试 Context() 方法基本功能
func TestContext_Context(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 获取标准库 context
	stdCtx := ctx.Context()
	assert.NotNil(t, stdCtx)

	// 应该是 Background context
	assert.NoError(t, stdCtx.Err())
	assert.Nil(t, stdCtx.Done())
}

// TestContext_ContextWithTimeout 测试使用 WithTimeout
func TestContext_ContextWithTimeout(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建带超时的 context
	stdCtx, cancel := context.WithTimeout(ctx.Context(), 100*time.Millisecond)
	defer cancel()

	// 应该没有立即取消
	assert.NoError(t, stdCtx.Err())

	// 等待超时
	time.Sleep(150 * time.Millisecond)

	// 应该已经超时
	assert.Error(t, stdCtx.Err())
	assert.Equal(t, context.DeadlineExceeded, stdCtx.Err())
}

// TestContext_SetStdContext 测试 SetStdContext 方法
func TestContext_SetStdContext(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 原始 context 应该是 Background
	originalCtx := ctx.Context()
	assert.NotNil(t, originalCtx)

	// 创建自定义 context
	customCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置自定义 context
	ctx.SetStdContext(customCtx)

	// 应该返回自定义 context
	assert.Equal(t, customCtx, ctx.Context())

	// 恢复原始 context
	ctx.SetStdContext(originalCtx)
	assert.Equal(t, originalCtx, ctx.Context())
}

// TestContext_SetStdContextWithValue 测试通过 SetStdContext 传递值
func TestContext_SetStdContextWithValue(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建带值的 context
	type key string
	customKey := key("test-key")
	customValue := "test-value"
	customCtx := context.WithValue(context.Background(), customKey, customValue)

	// 设置自定义 context
	ctx.SetStdContext(customCtx)

	// 应该能够获取到值
	value := ctx.Context().Value(customKey)
	assert.Equal(t, customValue, value)
}

// TestContext_SetStdContextMiddleware 测试在中间件中使用 SetStdContext
func TestContext_SetStdContextMiddleware(t *testing.T) {
	type key string
	traceKey := key("trace-id")

	// 创建追踪中间件
	tracingMiddleware := func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			// 注入 trace context
			traceCtx := context.WithValue(ctx.Context(), traceKey, "trace-123")
			originalCtx := ctx.Context()
			ctx.SetStdContext(traceCtx)
			defer ctx.SetStdContext(originalCtx)

			return next(ctx)
		}
	}

	// 创建 handler，验证能获取到 trace-id
	var receivedTraceID string
	handler := func(ctx *Context) error {
		value := ctx.Context().Value(traceKey)
		if str, ok := value.(string); ok {
			receivedTraceID = str
		}
		return nil
	}

	// 应用中间件
	wrappedHandler := tracingMiddleware(handler)

	// 执行
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	err := wrappedHandler(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "trace-123", receivedTraceID)

	// 验证 context 已恢复
	value := ctx.Context().Value(traceKey)
	assert.Nil(t, value, "context should be restored after middleware")
}

// TestContext_SetStdContextWithTimeout 测试在中间件中注入超时
func TestContext_SetStdContextWithTimeout(t *testing.T) {
	// 创建超时中间件
	timeoutMiddleware := func(timeout time.Duration) func(HandlerE) HandlerE {
		return func(next HandlerE) HandlerE {
			return func(ctx *Context) error {
				timeoutCtx, cancel := context.WithTimeout(ctx.Context(), timeout)
				defer cancel()

				originalCtx := ctx.Context()
				ctx.SetStdContext(timeoutCtx)
				defer ctx.SetStdContext(originalCtx)

				return next(ctx)
			}
		}
	}

	// 测试成功情况（handler 在超时前完成）
	t.Run("success", func(t *testing.T) {
		handler := func(ctx *Context) error {
			time.Sleep(10 * time.Millisecond)
			// 检查 context 是否还有效
			return ctx.Context().Err()
		}

		wrappedHandler := timeoutMiddleware(100 * time.Millisecond)(handler)

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		err := wrappedHandler(ctx)
		assert.NoError(t, err)
	})

	// 测试超时情况
	t.Run("timeout", func(t *testing.T) {
		handler := func(ctx *Context) error {
			time.Sleep(200 * time.Millisecond)
			// 检查 context 是否已超时
			return ctx.Context().Err()
		}

		wrappedHandler := timeoutMiddleware(50 * time.Millisecond)(handler)

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		err := wrappedHandler(ctx)
		assert.Error(t, err)
		assert.Equal(t, context.DeadlineExceeded, err)
	})
}

// TestContext_ContextWithCancel 测试使用 WithCancel
func TestContext_ContextWithCancel(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建可取消的 context
	stdCtx, cancel := context.WithCancel(ctx.Context())

	// 应该没有取消
	assert.NoError(t, stdCtx.Err())

	// 主动取消
	cancel()

	// 等待取消生效
	<-stdCtx.Done()

	// 应该已经取消
	assert.Error(t, stdCtx.Err())
	assert.Equal(t, context.Canceled, stdCtx.Err())
}

// TestNewContextWithContext 测试 NewContextWithContext 构造函数
func TestNewContextWithContext(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}

	// 创建自定义 context
	customCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 使用 NewContextWithContext
	ctx := NewContextWithContext(customCtx, event, nil)

	// 应该返回自定义的 context
	stdCtx := ctx.Context()
	assert.Equal(t, customCtx, stdCtx)

	// 应该有截止时间
	deadline, ok := stdCtx.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))
}

// TestContext_MultipleCreation 测试多次创建 Context 的独立性
func TestContext_MultipleCreation(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}

	// 第一次创建
	ctx1 := NewContext(event, nil)
	stdCtx1 := ctx1.Context()
	assert.NotNil(t, stdCtx1)

	// 创建子 context
	_, cancel := context.WithTimeout(stdCtx1, 5*time.Second)
	defer cancel()

	// 第二次创建（独立的新 Context）
	ctx2 := NewContext(event, nil)
	stdCtx2 := ctx2.Context()
	assert.NotNil(t, stdCtx2)

	// 应该是新的独立 context（Background）
	assert.NoError(t, stdCtx2.Err())
}

// TestContext_AsyncWithStdCtx 测试异步场景中只使用 stdCtx（不需要 Retain）
func TestContext_AsyncWithStdCtx(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	stdCtx := ctx.Context()
	assert.NotNil(t, stdCtx)

	var wg sync.WaitGroup
	wg.Add(1)

	// 在 goroutine 中只使用 stdCtx
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)

		// stdCtx 仍然有效（由 GC 管理）
		assert.NotNil(t, stdCtx)
		assert.NoError(t, stdCtx.Err())
	}()

	wg.Wait()
	// ✅ 应该没有 panic 或错误
}

// TestContext_HTTPClientWithTimeout 测试实际场景：HTTP 请求超时
func TestContext_HTTPClientWithTimeout(t *testing.T) {
	// 创建一个慢速的测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建带超时的 HTTP 请求（超时时间短于服务器响应时间）
	httpCtx, cancel := context.WithTimeout(ctx.Context(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, "GET", server.URL, nil)
	assert.NoError(t, err)

	// 发送请求，应该超时
	_, err = http.DefaultClient.Do(req)
	assert.Error(t, err)

	// 检查是否是超时错误
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// TestContext_DatabaseQueryContext 测试实际场景：数据库查询（模拟）
func TestContext_DatabaseQueryContext(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建带超时的 context
	dbCtx, cancel := context.WithTimeout(ctx.Context(), 100*time.Millisecond)
	defer cancel()

	// 模拟数据库查询（使用 context）
	queryDone := make(chan error, 1)
	go func() {
		// 模拟慢查询
		time.Sleep(200 * time.Millisecond)
		queryDone <- nil
	}()

	// 等待查询完成或超时
	select {
	case err := <-queryDone:
		t.Fatalf("Query should timeout, but got: %v", err)
	case <-dbCtx.Done():
		// 应该超时
		assert.Error(t, dbCtx.Err())
		assert.Equal(t, context.DeadlineExceeded, dbCtx.Err())
	}
}

// TestContext_ContextPropagation 测试 context 传播
func TestContext_ContextPropagation(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建带值的 context
	type contextKey string
	key := contextKey("trace_id")
	stdCtx := context.WithValue(ctx.Context(), key, "test-trace-123")

	// 替换 context
	ctx.ctx = stdCtx

	// 验证值传播
	traceID := ctx.Context().Value(key)
	assert.Equal(t, "test-trace-123", traceID)

	// 创建子 context
	childCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
	defer cancel()

	// 子 context 应该继承父 context 的值
	traceIDFromChild := childCtx.Value(key)
	assert.Equal(t, "test-trace-123", traceIDFromChild)
}

// TestContext_MultipleTimeouts 测试多个超时的组合
func TestContext_MultipleTimeouts(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建第一个超时 context（5 秒）
	ctx1, cancel1 := context.WithTimeout(ctx.Context(), 5*time.Second)
	defer cancel1()

	// 创建第二个超时 context（100 毫秒，更短）
	ctx2, cancel2 := context.WithTimeout(ctx1, 100*time.Millisecond)
	defer cancel2()

	// 等待较短的超时
	time.Sleep(150 * time.Millisecond)

	// ctx2 应该超时
	assert.Error(t, ctx2.Err())
	assert.Equal(t, context.DeadlineExceeded, ctx2.Err())

	// ctx1 不应该超时
	assert.NoError(t, ctx1.Err())
}

// TestContext_ConcurrentContextAccess 测试并发访问 Context()
func TestContext_ConcurrentContextAccess(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	var wg sync.WaitGroup
	concurrency := 100

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// 并发访问 Context()
			stdCtx := ctx.Context()
			assert.NotNil(t, stdCtx)

			// 创建子 context
			childCtx, cancel := context.WithTimeout(stdCtx, 1*time.Second)
			defer cancel()

			assert.NotNil(t, childCtx)
		}()
	}

	wg.Wait()
}

// BenchmarkContext_WithStdContext 基准测试：添加标准库 context 后的性能
func BenchmarkContext_WithStdContext(b *testing.B) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		_ = ctx.Context()
	}
}

// BenchmarkContext_WithTimeout 基准测试：创建带超时的 context
func BenchmarkContext_WithTimeout(b *testing.B) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		stdCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
		cancel() // 立即取消，避免定时器泄漏
		_ = stdCtx
	}
}

// BenchmarkContext_ContextAccess 基准测试：只访问 Context()
func BenchmarkContext_ContextAccess(b *testing.B) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.Context()
	}
}

// mockDB 模拟数据库接口
type mockDB struct{}

func (db *mockDB) QueryContext(ctx context.Context, query string) error {
	// 模拟查询，检查 context 是否被取消
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Millisecond):
		return nil
	}
}

// TestContext_RealWorldDatabaseScenario 真实场景测试：数据库查询超时
func TestContext_RealWorldDatabaseScenario(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	db := &mockDB{}

	// 场景 1：正常查询（足够的超时时间）
	t.Run("Normal Query", func(t *testing.T) {
		dbCtx, cancel := context.WithTimeout(ctx.Context(), 100*time.Millisecond)
		defer cancel()

		err := db.QueryContext(dbCtx, "SELECT * FROM users")
		assert.NoError(t, err)
	})

	// 场景 2：查询超时
	t.Run("Query Timeout", func(t *testing.T) {
		dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Millisecond)
		defer cancel()

		err := db.QueryContext(dbCtx, "SELECT * FROM users")
		assert.Error(t, err)
		assert.Equal(t, context.DeadlineExceeded, err)
	})
}

// TestContext_ContextNil 测试 context 为 nil 的情况
func TestContext_ContextNil(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 手动设置 ctx 为 nil（模拟异常情况）
	ctx.ctx = nil

	// Context() 应该返回 Background
	stdCtx := ctx.Context()
	assert.NotNil(t, stdCtx)
	assert.NoError(t, stdCtx.Err())
}

// TestContext_ReleaseWithActiveContext 测试释放时有活跃的子 context
func TestContext_ReleaseWithActiveContext(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建子 context（但不取消）
	childCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
	defer cancel()

	// 子 context 应该仍然有效（由 GC 管理）
	assert.NoError(t, childCtx.Err())

	// 手动取消
	cancel()

	// 应该被取消
	<-childCtx.Done()
	assert.Error(t, childCtx.Err())
}

// Example_ContextWithTimeout 示例：使用超时
func Example_contextWithTimeout() {
	// 创建 Context
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建带超时的 context
	dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
	defer cancel()

	// 使用在数据库查询中
	_ = dbCtx
	// db.QueryContext(dbCtx, "SELECT ...")
}

// Example_ContextWithCancel 示例：使用取消
func Example_contextWithCancel() {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 创建可取消的 context
	stdCtx, cancel := context.WithCancel(ctx.Context())
	defer cancel()

	// 在需要时取消
	go func() {
		time.Sleep(1 * time.Second)
		cancel()
	}()

	// 等待取消
	<-stdCtx.Done()
}

// TestContext_IntegrationWithSQL 集成测试：与 database/sql 兼容性（需要真实数据库时可启用）
func TestContext_IntegrationWithSQL(t *testing.T) {
	t.Skip("Requires real database connection")

	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	// 模拟使用
	var db *sql.DB // 需要真实的数据库连接

	dbCtx, cancel := context.WithTimeout(ctx.Context(), 5*time.Second)
	defer cancel()

	_, _ = db.QueryContext(dbCtx, "SELECT 1")
}
