package syncx

import "sync"

// Lazy 提供泛型懒初始化：首次调用 [Lazy.Get] 时执行 init 函数，
// 结果被缓存，后续调用直接返回缓存值，无额外锁开销。
//
// 使用 [NewLazy] 构造；零值不可用（init 为 nil 会 panic）。
//
// 示例：
//
//	loader := syncx.NewLazy(func() *Config {
//	    cfg, _ := loadConfigFromDisk()
//	    return cfg
//	})
//	// 首次调用触发加载，后续调用直接返回缓存：
//	cfg := loader.Get()
type Lazy[T any] struct {
	once sync.Once
	val  T
	init func() T
}

// NewLazy 创建一个懒初始化容器。
// init 在首次调用 [Lazy.Get] 时执行，且仅执行一次（并发安全）。
func NewLazy[T any](init func() T) *Lazy[T] {
	return &Lazy[T]{init: init}
}

// Get 返回初始化后的值。
// 首次调用时执行 init（其它并发 Get 调用阻塞直至完成），后续调用直接返回缓存值。
func (l *Lazy[T]) Get() T {
	l.once.Do(func() {
		l.val = l.init()
	})
	return l.val
}
