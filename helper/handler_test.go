package helper

import (
	"errors"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Chain 测试
// ============================================================================

func TestChain_Empty(t *testing.T) {
	handler := Chain()
	ctx := &context.Context{}
	err := handler(ctx)
	assert.NoError(t, err)
}

func TestChain_Single(t *testing.T) {
	called := false
	h := func(ctx *context.Context) error {
		called = true
		return nil
	}

	handler := Chain(h)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.True(t, called)
}

func TestChain_MultipleSuccess(t *testing.T) {
	var order []int

	h1 := func(ctx *context.Context) error {
		order = append(order, 1)
		return nil
	}
	h2 := func(ctx *context.Context) error {
		order = append(order, 2)
		return nil
	}
	h3 := func(ctx *context.Context) error {
		order = append(order, 3)
		return nil
	}

	handler := Chain(h1, h2, h3)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, order)
}

func TestChain_StopOnError(t *testing.T) {
	var order []int
	expectedErr := errors.New("handler 2 failed")

	h1 := func(ctx *context.Context) error {
		order = append(order, 1)
		return nil
	}
	h2 := func(ctx *context.Context) error {
		order = append(order, 2)
		return expectedErr
	}
	h3 := func(ctx *context.Context) error {
		order = append(order, 3)
		return nil
	}

	handler := Chain(h1, h2, h3)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, []int{1, 2}, order) // h3 不应该被调用
}

func TestChain_FirstHandlerError(t *testing.T) {
	var order []int
	expectedErr := errors.New("first handler failed")

	h1 := func(ctx *context.Context) error {
		order = append(order, 1)
		return expectedErr
	}
	h2 := func(ctx *context.Context) error {
		order = append(order, 2)
		return nil
	}

	handler := Chain(h1, h2)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, []int{1}, order)
}

// ============================================================================
// ToMiddleware 测试
// ============================================================================

func TestToMiddleware_Success(t *testing.T) {
	var order []string

	handlerFunc := func(ctx *context.Context) error {
		order = append(order, "handler")
		return nil
	}

	nextFunc := func(ctx *context.Context) error {
		order = append(order, "next")
		return nil
	}

	middleware := ToMiddleware(handlerFunc)
	wrapped := middleware(nextFunc)
	ctx := &context.Context{}
	err := wrapped(ctx)

	assert.NoError(t, err)
	assert.Equal(t, []string{"handler", "next"}, order)
}

func TestToMiddleware_Error(t *testing.T) {
	var order []string
	expectedErr := errors.New("handler failed")

	handlerFunc := func(ctx *context.Context) error {
		order = append(order, "handler")
		return expectedErr
	}

	nextFunc := func(ctx *context.Context) error {
		order = append(order, "next")
		return nil
	}

	middleware := ToMiddleware(handlerFunc)
	wrapped := middleware(nextFunc)
	ctx := &context.Context{}
	err := wrapped(ctx)

	assert.Equal(t, expectedErr, err)
	assert.Equal(t, []string{"handler"}, order) // next 不应该被调用
}

func TestToMiddleware_ChainMultiple(t *testing.T) {
	var order []string

	h1 := func(ctx *context.Context) error {
		order = append(order, "h1")
		return nil
	}
	h2 := func(ctx *context.Context) error {
		order = append(order, "h2")
		return nil
	}
	h3 := func(ctx *context.Context) error {
		order = append(order, "h3")
		return nil
	}

	// 链接多个中间件
	m1 := ToMiddleware(h1)
	m2 := ToMiddleware(h2)
	finalHandler := m1(m2(h3))

	ctx := &context.Context{}
	err := finalHandler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, []string{"h1", "h2", "h3"}, order)
}

// ============================================================================
// Parallel 测试
// ============================================================================

func TestParallel_Empty(t *testing.T) {
	handler := Parallel()
	ctx := &context.Context{}
	err := handler(ctx)
	assert.NoError(t, err)
}

func TestParallel_Single(t *testing.T) {
	called := false
	var mu sync.Mutex

	h := func(ctx *context.Context) error {
		mu.Lock()
		called = true
		mu.Unlock()
		return nil
	}

	handler := Parallel(h)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.True(t, called)
}

func TestParallel_MultipleSuccess(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	h1 := func(ctx *context.Context) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		return nil
	}
	h2 := func(ctx *context.Context) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		return nil
	}
	h3 := func(ctx *context.Context) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		return nil
	}

	handler := Parallel(h1, h2, h3)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestParallel_CollectErrors(t *testing.T) {
	err1 := errors.New("error 1")
	err2 := errors.New("error 2")

	h1 := func(ctx *context.Context) error {
		return err1
	}
	h2 := func(ctx *context.Context) error {
		return nil
	}
	h3 := func(ctx *context.Context) error {
		return err2
	}

	handler := Parallel(h1, h2, h3)
	ctx := &context.Context{}
	err := handler(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error 1")
	assert.Contains(t, err.Error(), "error 2")
}

func TestParallel_AllErrors(t *testing.T) {
	err1 := errors.New("error 1")
	err2 := errors.New("error 2")
	err3 := errors.New("error 3")

	h1 := func(ctx *context.Context) error {
		return err1
	}
	h2 := func(ctx *context.Context) error {
		return err2
	}
	h3 := func(ctx *context.Context) error {
		return err3
	}

	handler := Parallel(h1, h2, h3)
	ctx := &context.Context{}
	err := handler(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error 1")
	assert.Contains(t, err.Error(), "error 2")
	assert.Contains(t, err.Error(), "error 3")
}

// ============================================================================
// Conditional 测试
// ============================================================================

func TestConditional_ThenBranch(t *testing.T) {
	var result string

	condition := func(ctx *context.Context) error {
		return nil
	}
	thenHandler := func(ctx *context.Context) error {
		result = "then"
		return nil
	}
	elseHandler := func(ctx *context.Context) error {
		result = "else"
		return nil
	}

	handler := Conditional(condition, thenHandler, elseHandler)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, "then", result)
}

func TestConditional_ElseBranch(t *testing.T) {
	var result string

	condition := func(ctx *context.Context) error {
		return errors.New("condition failed")
	}
	thenHandler := func(ctx *context.Context) error {
		result = "then"
		return nil
	}
	elseHandler := func(ctx *context.Context) error {
		result = "else"
		return nil
	}

	handler := Conditional(condition, thenHandler, elseHandler)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, "else", result)
}

func TestConditional_NoElseHandler(t *testing.T) {
	expectedErr := errors.New("condition failed")

	condition := func(ctx *context.Context) error {
		return expectedErr
	}
	thenHandler := func(ctx *context.Context) error {
		return nil
	}

	handler := Conditional(condition, thenHandler, nil)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.Equal(t, expectedErr, err)
}

func TestConditional_ThenHandlerError(t *testing.T) {
	expectedErr := errors.New("then handler error")

	condition := func(ctx *context.Context) error {
		return nil
	}
	thenHandler := func(ctx *context.Context) error {
		return expectedErr
	}
	elseHandler := func(ctx *context.Context) error {
		return nil
	}

	handler := Conditional(condition, thenHandler, elseHandler)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.Equal(t, expectedErr, err)
}

func TestConditional_ElseHandlerError(t *testing.T) {
	expectedErr := errors.New("else handler error")

	condition := func(ctx *context.Context) error {
		return errors.New("condition failed")
	}
	thenHandler := func(ctx *context.Context) error {
		return nil
	}
	elseHandler := func(ctx *context.Context) error {
		return expectedErr
	}

	handler := Conditional(condition, thenHandler, elseHandler)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.Equal(t, expectedErr, err)
}

// ============================================================================
// Recover 测试
// ============================================================================

func TestRecover_NoPanic(t *testing.T) {
	h := func(ctx *context.Context) error {
		return nil
	}

	handler := Recover(h)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
}

func TestRecover_PanicError(t *testing.T) {
	expectedErr := errors.New("panic error")

	h := func(ctx *context.Context) error {
		panic(expectedErr)
	}

	handler := Recover(h)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.Equal(t, expectedErr, err)
}

func TestRecover_PanicString(t *testing.T) {
	h := func(ctx *context.Context) error {
		panic("panic message")
	}

	handler := Recover(h)
	ctx := &context.Context{}
	err := handler(ctx)

	require.Error(t, err)
	assert.Equal(t, "panic message", err.Error())
}

func TestRecover_PanicOtherType(t *testing.T) {
	h := func(ctx *context.Context) error {
		panic(123)
	}

	handler := Recover(h)
	ctx := &context.Context{}
	err := handler(ctx)

	require.Error(t, err)
	assert.Equal(t, "panic recovered", err.Error())
}

func TestRecover_NormalError(t *testing.T) {
	expectedErr := errors.New("normal error")

	h := func(ctx *context.Context) error {
		return expectedErr
	}

	handler := Recover(h)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.Equal(t, expectedErr, err)
}

// ============================================================================
// 集成测试
// ============================================================================

func TestIntegration_ChainWithToMiddleware(t *testing.T) {
	var order []string

	validate := func(ctx *context.Context) error {
		order = append(order, "validate")
		return nil
	}

	parse := func(ctx *context.Context) error {
		order = append(order, "parse")
		return nil
	}

	process := func(ctx *context.Context) error {
		order = append(order, "process")
		return nil
	}

	// 测试 Chain
	handler := Chain(validate, parse, process)
	ctx := &context.Context{}
	err := handler(ctx)
	assert.NoError(t, err)
	assert.Equal(t, []string{"validate", "parse", "process"}, order)

	// 重置并测试 ToMiddleware
	order = []string{}
	middleware1 := ToMiddleware(validate)
	middleware2 := ToMiddleware(parse)
	finalHandler := middleware1(middleware2(process))

	err = finalHandler(ctx)
	assert.NoError(t, err)
	assert.Equal(t, []string{"validate", "parse", "process"}, order)
}

func TestIntegration_ChainWithRecover(t *testing.T) {
	var order []string

	h1 := func(ctx *context.Context) error {
		order = append(order, "h1")
		return nil
	}

	h2 := func(ctx *context.Context) error {
		order = append(order, "h2")
		panic("intentional panic")
	}

	h3 := func(ctx *context.Context) error {
		order = append(order, "h3")
		return nil
	}

	// 使用 Recover 包装可能 panic 的 handler
	handler := Chain(h1, Recover(h2), h3)
	ctx := &context.Context{}
	err := handler(ctx)

	require.Error(t, err)
	assert.Equal(t, "intentional panic", err.Error())
	assert.Equal(t, []string{"h1", "h2"}, order) // h3 不应该执行
}

func TestIntegration_ConditionalWithChain(t *testing.T) {
	var result string

	isAdmin := func(ctx *context.Context) error {
		// 模拟条件检查
		return nil
	}

	adminFlow := Chain(
		func(ctx *context.Context) error {
			result += "admin:"
			return nil
		},
		func(ctx *context.Context) error {
			result += "step1:"
			return nil
		},
		func(ctx *context.Context) error {
			result += "step2"
			return nil
		},
	)

	userFlow := func(ctx *context.Context) error {
		result = "user"
		return nil
	}

	handler := Conditional(isAdmin, adminFlow, userFlow)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, "admin:step1:step2", result)
}

func TestIntegration_ParallelWithChain(t *testing.T) {
	var mu sync.Mutex
	var results []string

	task1 := Chain(
		func(ctx *context.Context) error {
			mu.Lock()
			results = append(results, "task1-step1")
			mu.Unlock()
			return nil
		},
		func(ctx *context.Context) error {
			mu.Lock()
			results = append(results, "task1-step2")
			mu.Unlock()
			return nil
		},
	)

	task2 := Chain(
		func(ctx *context.Context) error {
			mu.Lock()
			results = append(results, "task2-step1")
			mu.Unlock()
			return nil
		},
		func(ctx *context.Context) error {
			mu.Lock()
			results = append(results, "task2-step2")
			mu.Unlock()
			return nil
		},
	)

	handler := Parallel(task1, task2)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.Len(t, results, 4)
	// 包含所有步骤（顺序可能不同因为并行）
	assert.Contains(t, results, "task1-step1")
	assert.Contains(t, results, "task1-step2")
	assert.Contains(t, results, "task2-step1")
	assert.Contains(t, results, "task2-step2")
}

// ============================================================================
// 边界情况测试
// ============================================================================

func TestChain_NilHandler(t *testing.T) {
	// 测试包含 nil handler 的情况
	defer func() {
		if r := recover(); r != nil {
			t.Log("Recovered from panic:", r)
		}
	}()

	var nilHandler context.Handler
	handler := Chain(
		func(ctx *context.Context) error { return nil },
		nilHandler,
	)

	ctx := &context.Context{}
	_ = handler(ctx) // 可能会 panic，但被 defer 捕获
}

func TestToMiddleware_NilNext(t *testing.T) {
	h := func(ctx *context.Context) error {
		return nil
	}

	middleware := ToMiddleware(h)

	// 测试 nil next handler
	defer func() {
		if r := recover(); r != nil {
			t.Log("Recovered from panic:", r)
		}
	}()

	var nilNext context.Handler
	wrapped := middleware(nilNext)
	ctx := &context.Context{}
	_ = wrapped(ctx) // 可能会 panic
}

func TestParallel_ConcurrentContextAccess(t *testing.T) {
	// 测试并发访问 context 的安全性
	var counter int
	var mu sync.Mutex

	makeHandler := func(id int) context.Handler {
		return func(ctx *context.Context) error {
			mu.Lock()
			counter++
			mu.Unlock()
			return nil
		}
	}

	handlers := make([]context.Handler, 100)
	for i := 0; i < 100; i++ {
		handlers[i] = makeHandler(i)
	}

	handler := Parallel(handlers...)
	ctx := &context.Context{}
	err := handler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, 100, counter)
}
