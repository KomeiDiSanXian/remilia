// Package future 提供泛型 Future，用于异步操作的结果传递。
//
// Future 在创建时处于未就绪状态，通过 Resolve 设置结果并通知所有等待者。
// 支持多次调用 Resolve（仅第一次生效），适合竞争场景（如超时 vs 正常返回）。
package future

import (
	"context"
	"errors"
	"sync"
)

// ErrNotReady 表示 Future 尚未完成。
var ErrNotReady = errors.New("future: not ready")

// Future 是一个泛型异步结果容器。
//
// 使用方式：
//
//	f := future.New[SendResult]()
//	go func() {
//		res, err := sender.Send(ctx, req)
//		f.Resolve(res, err)
//	}()
//	result, err := f.Wait(ctx)
type Future[T any] struct {
	once sync.Once
	done chan struct{}
	val  T
	err  error
}

// New 创建一个新的 Future。
func New[T any]() *Future[T] {
	return &Future[T]{done: make(chan struct{})}
}

// Resolve 设置 Future 的结果。返回 true 表示本次调用完成了 Future，
// false 表示 Future 已经被之前的 Resolve 调用完成。
// 多次调用安全，仅第一次生效。
func (f *Future[T]) Resolve(val T, err error) bool {
	var resolved bool
	f.once.Do(func() {
		f.val, f.err = val, err
		close(f.done)
		resolved = true
	})
	return resolved
}

// Wait 等待 Future 完成并返回结果。
// 如果 ctx 在 Future 完成前被取消，返回 ctx.Err()。
func (f *Future[T]) Wait(ctx context.Context) (T, error) {
	select {
	case <-f.done:
		return f.val, f.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// Result 非阻塞返回 Future 的结果。
// 如果 Future 尚未完成，返回零值和 ErrNotReady。
func (f *Future[T]) Result() (T, error) {
	select {
	case <-f.done:
		return f.val, f.err
	default:
		var zero T
		return zero, ErrNotReady
	}
}

// IsDone 返回 Future 是否已完成。
func (f *Future[T]) IsDone() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

// Done 返回一个 channel，Future 完成时关闭。
// 可用于 select 语句：
//
//	select {
//	case <-f.Done():
//		// Future 已完成
//	case <-ctx.Done():
//		// 超时
//	}
func (f *Future[T]) Done() <-chan struct{} {
	return f.done
}

// MustWait 等待 Future 完成并返回结果。
// 如果 Future 返回错误，直接 panic。
func (f *Future[T]) MustWait(ctx context.Context) T {
	val, err := f.Wait(ctx)
	if err != nil {
		panic(err)
	}
	return val
}
