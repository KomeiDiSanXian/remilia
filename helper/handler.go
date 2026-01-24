package helper

import (
	"errors"
	"sync"

	"github.com/KomeiDiSanXian/remilia/core/context"
)

// Chain 组合多个 Handler 为一个 Handler
//
// 按顺序执行所有 Handler，如果任何一个返回错误，立即停止并返回该错误。
//
// 使用场景：
//   - 将复杂的业务逻辑分解为多个步骤
//   - 复用常见的 Handler 逻辑
//   - 保持代码清晰和可维护
//
// 示例：
//
//	validateInput := func(ctx *context.Context) error {
//	    // 验证输入
//	    return nil
//	}
//
//	processData := func(ctx *context.Context) error {
//	    // 处理数据
//	    return nil
//	}
//
//	formatOutput := func(ctx *context.Context) error {
//	    // 格式化输出
//	    return nil
//	}
//
//	handler := helper.Chain(
//	    validateInput,
//	    processData,
//	    formatOutput,
//	)
//
//	eng.OnCommand("/api").Handle(handler)
//
// 错误处理：
//   - 如果任何一个 Handler 返回错误，链立即停止
//   - 返回第一个遇到的错误
func Chain(handlers ...context.Handler) context.Handler {
	if len(handlers) == 0 {
		return func(ctx *context.Context) error {
			return nil
		}
	}
	if len(handlers) == 1 {
		return handlers[0]
	}

	return func(ctx *context.Context) error {
		for _, h := range handlers {
			if err := h(ctx); err != nil {
				return err
			}
		}
		return nil
	}
}

// ToMiddleware 将 Handler 转换为 Middleware
//
// 转换后的中间件会先执行该 Handler，如果成功则继续执行下一个 Handler。
// 如果 Handler 返回错误，不会调用 next Handler。
//
// 使用场景：
//   - 将 Handler 逻辑作为中间件使用
//   - 在 Handler 之前执行验证或预处理
//   - 复用现有的 Handler 逻辑
//
// 示例：
//
//	validateAuth := func(ctx *context.Context) error {
//	    if !isAuthenticated(ctx) {
//	        return errors.New("unauthorized")
//	    }
//	    return nil
//	}
//
//	eng.OnCommand("/api").
//	    Use(helper.ToMiddleware(validateAuth)).
//	    Use(helper.ToMiddleware(parseInput)).
//	    Handle(businessLogic)
//
// 注意：
//   - 如果 Handler 返回错误，中间件链会停止，不会调用后续中间件或 Handler
//   - 与直接使用中间件的区别在于，这允许复用现有的 Handler 函数
func ToMiddleware(h context.Handler) context.Middleware {
	return func(next context.Handler) context.Handler {
		return func(ctx *context.Context) error {
			if err := h(ctx); err != nil {
				return err
			}
			return next(ctx)
		}
	}
}

// Parallel 并行执行多个 Handler
//
// 所有 Handler 在独立的 goroutine 中并行执行。
// 等待所有 Handler 完成后，如果有任何错误，返回所有错误的组合。
//
// 使用场景：
//   - 需要同时执行多个独立的操作
//   - 提高 I/O 密集型操作的性能
//   - 并行获取多个数据源
//
// 示例：
//
//	fetchUserData := func(ctx *context.Context) error {
//	    // 获取用户数据
//	    return nil
//	}
//
//	fetchProductData := func(ctx *context.Context) error {
//	    // 获取产品数据
//	    return nil
//	}
//
//	fetchOrderData := func(ctx *context.Context) error {
//	    // 获取订单数据
//	    return nil
//	}
//
//	handler := helper.Parallel(
//	    fetchUserData,
//	    fetchProductData,
//	    fetchOrderData,
//	)
//
//	eng.OnCommand("/dashboard").Handle(handler)
//
// 注意事项：
//   - Handler 必须是并发安全的
//   - 对 Context 的修改可能导致竞态条件
//   - 所有 Handler 都会执行，即使某些失败
//   - 如果多个 Handler 返回错误，所有错误会被组合返回
//
// 错误处理：
//   - 使用 errors.Join() 组合所有错误
//   - 只有在所有 Handler 完成后才返回
func Parallel(handlers ...context.Handler) context.Handler {
	if len(handlers) == 0 {
		return func(ctx *context.Context) error {
			return nil
		}
	}
	if len(handlers) == 1 {
		return handlers[0]
	}

	return func(ctx *context.Context) error {
		type result struct {
			index int
			err   error
		}

		results := make(chan result, len(handlers))
		var wg sync.WaitGroup
		wg.Add(len(handlers))

		// 并行执行所有 Handler
		for i, h := range handlers {
			go func(idx int, handler context.Handler) {
				defer wg.Done()
				err := handler(ctx)
				results <- result{index: idx, err: err}
			}(i, h)
		}

		// 等待所有完成
		wg.Wait()
		close(results)

		// 收集错误
		var errs []error
		for r := range results {
			if r.err != nil {
				errs = append(errs, r.err)
			}
		}

		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}
}

// Conditional 根据条件选择执行不同的 Handler
//
// 先执行 condition Handler，如果成功则执行 thenHandler，否则执行 elseHandler。
//
// 使用场景：
//   - 根据上下文条件执行不同的逻辑
//   - 实现条件分支
//
// 示例：
//
//	isAdmin := func(ctx *context.Context) error {
//	    if !ctx.HasPermission("admin") {
//	        return errors.New("not admin")
//	    }
//	    return nil
//	}
//
//	adminHandler := func(ctx *context.Context) error {
//	    return ctx.Reply("Admin panel")
//	}
//
//	userHandler := func(ctx *context.Context) error {
//	    return ctx.Reply("User panel")
//	}
//
//	handler := helper.Conditional(isAdmin, adminHandler, userHandler)
//	eng.OnCommand("/panel").Handle(handler)
//
// 注意：
//   - elseHandler 可以为 nil，此时条件失败会直接返回 condition 的错误
func Conditional(condition context.Handler, thenHandler, elseHandler context.Handler) context.Handler {
	return func(ctx *context.Context) error {
		err := condition(ctx)
		if err == nil {
			return thenHandler(ctx)
		}
		if elseHandler != nil {
			return elseHandler(ctx)
		}
		return err
	}
}

// Recover 包装 Handler，捕获 panic 并转换为 error
//
// 使用场景：
//   - 保护关键的 Handler 不因 panic 而崩溃
//   - 提供优雅的错误处理
//
// 示例：
//
//	riskyHandler := func(ctx *context.Context) error {
//	    // 可能 panic 的代码
//	    return nil
//	}
//
//	safeHandler := helper.Recover(riskyHandler)
//	eng.OnCommand("/risky").Handle(safeHandler)
func Recover(h context.Handler) context.Handler {
	return func(ctx *context.Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				switch v := r.(type) {
				case error:
					err = v
				case string:
					err = errors.New(v)
				default:
					err = errors.New("panic recovered")
				}
			}
		}()
		return h(ctx)
	}
}
