package plugin

// benchmark_escape_test.go — 插件每事件 API 分配预算基准
//
// 覆盖插件 handler 热路径的分配形态：
//   - Container.Get（冻结快照）/ GetService：必须保持 0 allocs
//   - EventBus.Publish：异步分发的固定成本（COW 订阅者切片直接迭代）

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type benchSvc struct{ n int }

// Container.Get 冻结快照路径（插件 handler 每事件访问服务的形态）
func BenchmarkContainerGetFrozen(b *testing.B) {
	c := NewContainer()
	c.Register("svc", &benchSvc{n: 1})
	c.Freeze()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Get("svc")
	}
}

// GetService 按名类型安全访问（推荐 API）
func BenchmarkGetServiceByName(b *testing.B) {
	c := NewContainer()
	c.Register("svc", &benchSvc{n: 1})
	c.Freeze()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetService[*benchSvc](c, "svc")
	}
}

// EventBus.Publish 无订阅者（空分发路径）
func BenchmarkPublishNoSubscribers(b *testing.B) {
	eb := NewEventBusWithOptions(EventBusOptions{WorkerPoolSize: 64}).(*eventBus)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eb.PublishContext(context.Background(), "topic", "data")
	}
}

// EventBus.Publish 1 个订阅者（最典型形态）
func BenchmarkPublishOneSubscriber(b *testing.B) {
	eb := NewEventBusWithOptions(EventBusOptions{WorkerPoolSize: 64}).(*eventBus)
	var done atomic.Int64
	_, _ = eb.Subscribe("topic", func(ctx context.Context, data any) error {
		done.Add(1)
		return nil
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eb.PublishContext(context.Background(), "topic", "data")
	}
	// 等待所有异步 handler 完成，避免泄漏 goroutine
	deadline := time.Now().Add(30 * time.Second)
	for done.Load() < int64(b.N) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}
