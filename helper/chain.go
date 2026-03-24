package helper

import "strings"

// ChainGeneric 将多个具有相同签名的函数合并为一个函数。
//
// 函数按顺序执行，若任一函数返回错误，链式调用立即停止并返回该错误。
//
// 这是泛型版本，适用于任意上下文类型，而非仅限于 *context.Context。
// 若要对 Remilia 专属的 context.Handler 进行链式调用，请使用 handler.go 中的非泛型 Chain 函数。
//
// 示例：
//
//	// 用于自定义上下文类型
//	type MyContext struct { UserID string }
//	handler := ChainGeneric[*MyContext](
//	    validateInput,
//	    parseCommand,
//	    executeLogic,
//	)
//
//	// 用于 HTTP 上下文
//	type HTTPContext struct { /* ... */ }
//	httpHandler := ChainGeneric[*HTTPContext](
//	    authMiddleware,
//	    rateLimit,
//	    businessLogic,
//	)
//
// 性能：
//   - 空链或单处理器链零分配
//   - 多处理器链极低额外开销
func ChainGeneric[Ctx any](handlers ...func(Ctx) error) func(Ctx) error {
	if len(handlers) == 0 {
		return func(Ctx) error { return nil }
	}
	if len(handlers) == 1 {
		return handlers[0]
	}

	return func(ctx Ctx) error {
		for _, h := range handlers {
			if err := h(ctx); err != nil {
				return err
			}
		}
		return nil
	}
}

// ChainWithNext 将中间件风格（接受"next"处理器）的函数组合在一起。
//
// 适用于构建中间件链，每个中间件可决定是否调用链中的下一个处理器。
//
// 并发安全性：返回的函数每次调用都会创建独立的执行状态，因此可并发调用。
// 但每条单独的调用链必须顺序执行（即单个中间件不得从多个 goroutine 并发调用 next()）。
//
// 示例：
//
//	middleware := ChainWithNext[*context.Context](
//	    func(ctx *context.Context, next func(*context.Context) error) error {
//	        // 前置处理
//	        if err := validateAuth(ctx); err != nil {
//	            return err
//	        }
//	        // 调用下一个处理器
//	        if err := next(ctx); err != nil {
//	            return err
//	        }
//	        // 后置处理
//	        logRequest(ctx)
//	        return nil
//	    },
//	    // ... 更多中间件
//	)
func ChainWithNext[Ctx any](middlewares ...func(Ctx, func(Ctx) error) error) func(Ctx, func(Ctx) error) error {
	if len(middlewares) == 0 {
		return func(ctx Ctx, next func(Ctx) error) error {
			return next(ctx)
		}
	}

	return func(ctx Ctx, final func(Ctx) error) error {
		// 使用递归而非可变 index，避免并发调用时的数据竞争
		var dispatch func(i int, c Ctx) error
		dispatch = func(i int, c Ctx) error {
			if i >= len(middlewares) {
				return final(c)
			}
			return middlewares[i](c, func(next Ctx) error {
				return dispatch(i+1, next)
			})
		}
		return dispatch(0, ctx)
	}
}

// Pipe 将函数组合成管道，前一个函数的输出作为后一个函数的输入。
//
// 适用于构建数据转换管道。
//
// 示例：
//
//	transform := Pipe[string](
//	    strings.TrimSpace,
//	    strings.ToLower,
//	    func(s string) string { return strings.ReplaceAll(s, " ", "-") },
//	)
//	slug := transform("  Hello World  ")  // "hello-world"
func Pipe[T any](funcs ...func(T) T) func(T) T {
	if len(funcs) == 0 {
		return func(t T) T { return t }
	}
	if len(funcs) == 1 {
		return funcs[0]
	}

	return func(input T) T {
		result := input
		for _, f := range funcs {
			result = f(result)
		}
		return result
	}
}

// Compose 与 Pipe 类似，但以逆序应用函数。
//
// 遵循数学函数合成：(f ∘ g)(x) = f(g(x))
//
// 示例：
//
//	// f(g(h(x)))
//	composed := Compose[string](f, g, h)
//	result := composed(x)  // 等同于 f(g(h(x)))
func Compose[T any](funcs ...func(T) T) func(T) T {
	if len(funcs) == 0 {
		return func(t T) T { return t }
	}
	if len(funcs) == 1 {
		return funcs[0]
	}

	return func(input T) T {
		result := input
		for i := len(funcs) - 1; i >= 0; i-- {
			result = funcs[i](result)
		}
		return result
	}
}

// Filter 根据谓词函数过滤切片。
//
// 示例：
//
//	numbers := []int{1, 2, 3, 4, 5}
//	evens := Filter(numbers, func(n int) bool { return n%2 == 0 })
//	// evens = [2, 4]
func Filter[T any](slice []T, predicate func(T) bool) []T {
	result := make([]T, 0, len(slice))
	for _, item := range slice {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

// Map 通过对每个元素应用函数来转换切片。
//
// 示例：
//
//	numbers := []int{1, 2, 3}
//	doubled := Map(numbers, func(n int) int { return n * 2 })
//	// doubled = [2, 4, 6]
func Map[T, R any](slice []T, fn func(T) R) []R {
	result := make([]R, len(slice))
	for i, item := range slice {
		result[i] = fn(item)
	}
	return result
}

// Reduce 使用累加器函数将切片归约为单个值。
//
// 示例：
//
//	numbers := []int{1, 2, 3, 4}
//	sum := Reduce(numbers, 0, func(acc, n int) int { return acc + n })
//	// sum = 10
func Reduce[T, R any](slice []T, initial R, fn func(R, T) R) R {
	result := initial
	for _, item := range slice {
		result = fn(result, item)
	}
	return result
}

// Find 返回满足谓词的第一个元素，若未找到则返回零值。
//
// 示例：
//
//	numbers := []int{1, 2, 3, 4, 5}
//	found, ok := Find(numbers, func(n int) bool { return n > 3 })
//	// found = 4, ok = true
func Find[T any](slice []T, predicate func(T) bool) (T, bool) {
	for _, item := range slice {
		if predicate(item) {
			return item, true
		}
	}
	var zero T
	return zero, false
}

// StringPipe 创建字符串转换管道。
//
// 这是为字符串预配置 Pipe 的便捷函数。
//
// 示例：
//
//	slugify := StringPipe(
//	    strings.TrimSpace,
//	    strings.ToLower,
//	    func(s string) string { return strings.ReplaceAll(s, " ", "-") },
//	)
//	slug := slugify("  Hello World  ")  // "hello-world"
func StringPipe(funcs ...func(string) string) func(string) string {
	return Pipe(funcs...)
}

// StringReplace 创建一个将所有 old 替换为 new 的函数。
func StringReplace(old, new string) func(string) string {
	return func(s string) string {
		return strings.ReplaceAll(s, old, new)
	}
}
