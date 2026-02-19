package helper

import "strings"

// ChainGeneric combines multiple functions with the same signature into a single function.
//
// The functions are executed in order, and if any function returns an error,
// the chain stops and returns that error immediately.
//
// This is a generic version that works with any context type, not just *context.Context.
// For Remilia-specific context.Handler chaining, use the non-generic Chain function in handler.go.
//
// Example:
//
//	// For custom context types
//	type MyContext struct { UserID string }
//	handler := ChainGeneric[*MyContext](
//	    validateInput,
//	    parseCommand,
//	    executeLogic,
//	)
//
//	// For HTTP Context
//	type HTTPContext struct { /* ... */ }
//	httpHandler := ChainGeneric[*HTTPContext](
//	    authMiddleware,
//	    rateLimit,
//	    businessLogic,
//	)
//
// Performance:
//   - Zero allocation for empty or single-handler chains
//   - Minimal overhead for multi-handler chains
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

// ChainWithNext combines middleware-style functions that accept a "next" handler.
//
// This is useful for building middleware chains where each middleware can decide
// whether to call the next handler in the chain.
//
// Example:
//
//	middleware := ChainWithNext[*context.Context](
//	    func(ctx *context.Context, next func(*context.Context) error) error {
//	        // Pre-processing
//	        if err := validateAuth(ctx); err != nil {
//	            return err
//	        }
//	        // Call next
//	        if err := next(ctx); err != nil {
//	            return err
//	        }
//	        // Post-processing
//	        logRequest(ctx)
//	        return nil
//	    },
//	    // ... more middlewares
//	)
func ChainWithNext[Ctx any](middlewares ...func(Ctx, func(Ctx) error) error) func(Ctx, func(Ctx) error) error {
	if len(middlewares) == 0 {
		return func(ctx Ctx, next func(Ctx) error) error {
			return next(ctx)
		}
	}

	return func(ctx Ctx, final func(Ctx) error) error {
		var index int
		var runner func(Ctx) error
		runner = func(c Ctx) error {
			if index >= len(middlewares) {
				return final(c)
			}
			mw := middlewares[index]
			index++
			return mw(c, runner)
		}
		return runner(ctx)
	}
}

// Pipe composes functions where the output of one becomes the input of the next.
//
// This is useful for building data transformation pipelines.
//
// Example:
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

// Compose is similar to Pipe but applies functions in reverse order.
//
// This follows the mathematical function composition: (f ∘ g)(x) = f(g(x))
//
// Example:
//
//	// f(g(h(x)))
//	composed := Compose[string](f, g, h)
//	result := composed(x)  // same as f(g(h(x)))
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

// Filter filters a slice based on a predicate function.
//
// Example:
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

// Map transforms a slice by applying a function to each element.
//
// Example:
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

// Reduce reduces a slice to a single value using an accumulator function.
//
// Example:
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

// Find returns the first element that satisfies the predicate, or zero value if none found.
//
// Example:
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

// StringPipe creates a pipeline for string transformations.
//
// This is a convenience function that pre-configures Pipe for strings.
//
// Example:
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

// StringReplace creates a function that replaces all occurrences of old with new.
func StringReplace(old, new string) func(string) string {
	return func(s string) string {
		return strings.ReplaceAll(s, old, new)
	}
}
