package helper

import (
	"reflect"
	"slices"
	"sort"
	"strings"
)

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

// Fn 是承载函数组合的具名函数类型（Go 1.27 泛型方法）。
//
// 通过 [FnOf] 构造后，可用 Pipe/Compose 方法组合数据转换管道。
type Fn[T any] func(T) T

// FnOf 将普通函数包装为 Fn。
func FnOf[T any](f func(T) T) Fn[T] {
	return f
}

// Pipe 将当前函数与其它函数组合成管道，前一个函数的输出作为后一个函数的输入。
//
// 适用于构建数据转换管道。
//
// 示例：
//
//	transform := helper.FnOf(func(s string) string { return strings.TrimSpace(s) }).Pipe(
//	    strings.ToLower,
//	    func(s string) string { return strings.ReplaceAll(s, " ", "-") },
//	)
//	slug := transform("  Hello World  ") // "hello-world"
func (f Fn[T]) Pipe(others ...func(T) T) Fn[T] {
	all := make([]func(T) T, 0, 1+len(others))
	if f != nil {
		all = append(all, f)
	}
	all = append(all, others...)
	return func(input T) T {
		result := input
		for _, fn := range all {
			result = fn(result)
		}
		return result
	}
}

// Compose 与 Pipe 类似，但以逆序应用函数。
//
// 遵循数学函数合成：f.Compose(g, h)(x) = f(g(h(x)))
//
// 示例：
//
//	composed := helper.FnOf(f).Compose(g, h)
//	result := composed(x) // 等同于 f(g(h(x)))
func (f Fn[T]) Compose(others ...func(T) T) Fn[T] {
	return func(input T) T {
		result := input
		for _, other := range slices.Backward(others) {
			result = other(result)
		}
		if f != nil {
			result = f(result)
		}
		return result
	}
}

// Seq 是承载链式切片操作的具名切片类型（Go 1.27 泛型方法）。
//
// 示例：
//
//	numbers := []int{1, 2, 3, 4, 5}
//	evens := helper.From(numbers).Filter(func(n int) bool { return n%2 == 0 })
//	doubled := helper.From(numbers).Map(func(n int) int { return n * 2 })
//	sum := helper.From(numbers).Reduce(0, func(acc, n int) int { return acc + n })
//	found, ok := helper.From(numbers).Find(func(n int) bool { return n > 3 })
//
// 链式组合（Map 方法自带类型参数 R）：
//
//	helper.From(numbers).
//	    Filter(func(n int) bool { return n > 0 }).
//	    Map(func(n int) string { return fmt.Sprint(n) })
type Seq[T any] []T

// From 将普通切片转换为 Seq，用于链式调用。
func From[T any](slice []T) Seq[T] {
	return slice
}

// Unwrap 返回底层切片。
func (s Seq[T]) Unwrap() []T {
	return s
}

// Filter 返回满足谓词的元素组成的新 Seq。
func (s Seq[T]) Filter(predicate func(T) bool) Seq[T] {
	result := make(Seq[T], 0, len(s))
	for _, item := range s {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

// Each 对每个元素执行 fn（副作用遍历）。
func (s Seq[T]) Each(fn func(T)) {
	for _, item := range s {
		fn(item)
	}
}

// Sort 按 less 排序（稳定排序，原地修改），返回自身以便链式调用。
func (s Seq[T]) Sort(less func(T, T) bool) Seq[T] {
	sort.SliceStable(s, func(i, j int) bool { return less(s[i], s[j]) })
	return s
}

// Contains 报告 Seq 中是否存在与 target 相等的元素。
//
// 由于方法无法为接收者类型参数追加 comparable 约束，相等性通过
// reflect.DeepEqual 判断（对 int/string 等常见类型与 == 一致）。
func (s Seq[T]) Contains(target T) bool {
	for _, item := range s {
		if reflect.DeepEqual(item, target) {
			return true
		}
	}
	return false
}

// Map 对每个元素应用 fn，返回新元素类型的 Seq（方法自带类型参数 R）。
func (s Seq[T]) Map[R any](fn func(T) R) Seq[R] {
	result := make(Seq[R], len(s))
	for i, item := range s {
		result[i] = fn(item)
	}
	return result
}

// Reduce 使用累加器函数将 Seq 归约为单个值。
func (s Seq[T]) Reduce[R any](initial R, fn func(R, T) R) R {
	result := initial
	for _, item := range s {
		result = fn(result, item)
	}
	return result
}

// Find 返回满足谓词的第一个元素，若未找到则返回零值。
func (s Seq[T]) Find(predicate func(T) bool) (T, bool) {
	for _, item := range s {
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
	return FnOf(func(s string) string { return s }).Pipe(funcs...)
}

// StringReplace 创建一个将所有 old 替换为 new 的函数。
func StringReplace(old, new string) func(string) string {
	return func(s string) string {
		return strings.ReplaceAll(s, old, new)
	}
}
