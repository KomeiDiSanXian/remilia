package plugin_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

func TestEventBus_WildcardSubscription(t *testing.T) {
	bus := plugin.NewEventBus()
	var mu sync.Mutex
	var received []any

	// 订阅通配符
	sub, err := bus.SubscribeAll(func(data any) {
		mu.Lock()
		received = append(received, data)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("SubscribeAll failed: %v", err)
	}

	// 发布到不同 topic
	_ = bus.Publish("topic.a", "msg1")
	_ = bus.Publish("topic.b", "msg2")
	_ = bus.Publish("topic.c", "msg3")

	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count != 3 {
		t.Fatalf("wildcard subscriber should receive 3 messages, got %d", count)
	}

	// 取消订阅后不再收到
	sub.Unsubscribe()
	_ = bus.Publish("topic.d", "msg4")
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	count = len(received)
	mu.Unlock()

	if count != 3 {
		t.Fatalf("after unsubscribe, should not receive more messages, got %d", count)
	}
}

func TestEventBus_WildcardAndSpecific(t *testing.T) {
	bus := plugin.NewEventBus()
	var mu sync.Mutex
	specificCount := 0
	wildcardCount := 0

	bus.Subscribe("news", func(data any) {
		mu.Lock()
		specificCount++
		mu.Unlock()
	})
	bus.SubscribeAll(func(data any) {
		mu.Lock()
		wildcardCount++
		mu.Unlock()
	})

	bus.Publish("news", "breaking")
	bus.Publish("weather", "sunny")
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	sc, wc := specificCount, wildcardCount
	mu.Unlock()

	if sc != 1 {
		t.Fatalf("specific subscriber should get 1 message, got %d", sc)
	}
	if wc != 2 {
		t.Fatalf("wildcard subscriber should get 2 messages, got %d", wc)
	}
}

func TestDependencyReloadNotification(t *testing.T) {
	eng := engine.NewEngine()
	mgr := plugin.NewManager(eng)

	var notified string
	var mu sync.Mutex

	// 注册基础插件
	mgr.Register(&plugin.Descriptor{
		Name:  "base",
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
	})

	// 注册依赖方，设置 OnDependencyReloaded 回调
	mgr.Register(&plugin.Descriptor{
		Name:  "dependent",
		Deps:  []string{"base"},
		Setup: func(ctx *plugin.SetupContext) (any, error) { return nil, nil },
		Advanced: &plugin.Advanced{
			OnDependencyReloaded: func(dep string) {
				mu.Lock()
				notified = dep
				mu.Unlock()
			},
		},
	})

	// 重载 base 插件
	if err := mgr.Reload(context.Background(), "base"); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// 等待异步通知
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	got := notified
	mu.Unlock()

	if got != "base" {
		t.Fatalf("expected notified='base', got %q", got)
	}
}
