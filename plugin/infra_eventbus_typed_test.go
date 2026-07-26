package plugin

// eventbus_typed_test.go — TypedEventBus 泛型 API 测试（P2-6）
//
// 覆盖：
//   - Subscribe[T] / MustSubscribe[T]
//   - SubscribeAllTyped[T]
//   - PublishTyped[T] / MustPublishTyped[T]
//   - TypedChannel[T] 全 API
//   - 类型不匹配时静默跳过（不 panic）
//   - nil bus / nil handler 的错误路径

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- 测试用类型 ----------------------------------------------------------------

type userLoginEvent struct {
	UserID string
	IP     string
}

type metricEvent struct {
	Name  string
	Value float64
}

// ---- Subscribe[T] ------------------------------------------------------------

func TestSubscribe_TypedReceive(t *testing.T) {
	bus := NewEventBus()
	var got userLoginEvent
	var called atomic.Bool

	sub, err := Subscribe[userLoginEvent](bus, "user.login", func(e userLoginEvent) {
		got = e
		called.Store(true)
	})
	require.NoError(t, err)
	require.NotNil(t, sub)

	want := userLoginEvent{UserID: "u1", IP: "1.2.3.4"}
	err = bus.Publish("user.login", want)
	require.NoError(t, err)

	// 等待异步 handler
	waitFor(t, func() bool { return called.Load() }, 500*time.Millisecond)
	assert.Equal(t, want, got)
}

func TestSubscribe_TypeMismatchSilentSkip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	// 发布 string，但订阅 userLoginEvent ——应静默跳过，不 panic
	bus := NewEventBus()
	var called atomic.Bool

	_, err := Subscribe[userLoginEvent](bus, "topic", func(e userLoginEvent) {
		called.Store(true)
	})
	require.NoError(t, err)

	err = bus.Publish("topic", "this is a string, not userLoginEvent")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.False(t, called.Load(), "handler must not be called on type mismatch")
	})
}

func TestSubscribe_NilBus(t *testing.T) {
	_, err := Subscribe[string](nil, "t", func(s string) {})
	assert.Error(t, err)
}

func TestSubscribe_NilHandler(t *testing.T) {
	bus := NewEventBus()
	_, err := Subscribe[string](bus, "t", nil)
	assert.Error(t, err)
}

// ---- MustSubscribe[T] --------------------------------------------------------

func TestMustSubscribe_PanicsOnNilBus(t *testing.T) {
	assert.Panics(t, func() {
		MustSubscribe[string](nil, "t", func(s string) {})
	})
}

func TestMustSubscribe_Works(t *testing.T) {
	bus := NewEventBus()
	var received atomic.Bool
	sub := MustSubscribe[int](bus, "count", func(n int) {
		received.Store(true)
	})
	require.NotNil(t, sub)

	require.NoError(t, PublishTyped[int](bus, "count", 42))
	waitFor(t, func() bool { return received.Load() }, 500*time.Millisecond)
}

// ---- SubscribeAllTyped[T] ---------------------------------------------------

func TestSubscribeAllTyped_ReceivesMatchingTypes(t *testing.T) {
	bus := NewEventBus()
	var count atomic.Int32

	_, err := SubscribeAllTyped[metricEvent](bus, func(e metricEvent) {
		count.Add(1)
	})
	require.NoError(t, err)

	// 发布两个匹配类型的事件
	require.NoError(t, PublishTyped[metricEvent](bus, "metric.cpu", metricEvent{Name: "cpu", Value: 0.8}))
	require.NoError(t, PublishTyped[metricEvent](bus, "metric.mem", metricEvent{Name: "mem", Value: 512}))
	// 发布一个不匹配类型的事件（string）
	require.NoError(t, bus.Publish("metric.other", "should be skipped"))

	waitFor(t, func() bool { return count.Load() >= 2 }, 500*time.Millisecond)
	assert.EqualValues(t, 2, count.Load(), "only metricEvent messages should be received")
}

func TestSubscribeAllTyped_NilBus(t *testing.T) {
	_, err := SubscribeAllTyped[string](nil, func(s string) {})
	assert.Error(t, err)
}

func TestSubscribeAllTyped_NilHandler(t *testing.T) {
	bus := NewEventBus()
	_, err := SubscribeAllTyped[string](bus, nil)
	assert.Error(t, err)
}

// ---- PublishTyped[T] ---------------------------------------------------------

func TestPublishTyped_NilBus(t *testing.T) {
	err := PublishTyped[string](nil, "t", "data")
	assert.Error(t, err)
}

func TestPublishTyped_RoundTrip(t *testing.T) {
	bus := NewEventBus()
	var got string
	var called atomic.Bool

	_, err := Subscribe[string](bus, "greet", func(s string) {
		got = s
		called.Store(true)
	})
	require.NoError(t, err)

	err = PublishTyped[string](bus, "greet", "hello")
	require.NoError(t, err)

	waitFor(t, func() bool { return called.Load() }, 500*time.Millisecond)
	assert.Equal(t, "hello", got)
}

// ---- MustPublishTyped[T] ----------------------------------------------------

func TestMustPublishTyped_PanicsOnNilBus(t *testing.T) {
	assert.Panics(t, func() {
		MustPublishTyped[string](nil, "t", "data")
	})
}

func TestMustPublishTyped_Works(t *testing.T) {
	bus := NewEventBus()
	var received atomic.Bool
	MustSubscribe[string](bus, "greet", func(s string) {
		received.Store(true)
	})
	// 不应 panic
	assert.NotPanics(t, func() {
		MustPublishTyped[string](bus, "greet", "world")
	})
	waitFor(t, func() bool { return received.Load() }, 500*time.Millisecond)
}

// ---- TypedChannel[T] --------------------------------------------------------

func TestTypedChannel_PublishAndSubscribe(t *testing.T) {
	bus := NewEventBus()
	ch := NewTypedChannel[userLoginEvent](bus, "user.login")

	var got userLoginEvent
	var called atomic.Bool

	sub, err := ch.Subscribe(func(e userLoginEvent) {
		got = e
		called.Store(true)
	})
	require.NoError(t, err)
	require.NotNil(t, sub)

	want := userLoginEvent{UserID: "alice", IP: "10.0.0.1"}
	require.NoError(t, ch.Publish(want))

	waitFor(t, func() bool { return called.Load() }, 500*time.Millisecond)
	assert.Equal(t, want, got)
}

func TestTypedChannel_Topic(t *testing.T) {
	ch := NewTypedChannel[string](nil, "test.topic")
	assert.Equal(t, "test.topic", ch.Topic())
}

func TestTypedChannel_WithBus_ImmutableOriginal(t *testing.T) {
	// WithBus 不修改原 TypedChannel，保证契约对象可以安全复用
	bus := NewEventBus()
	global := NewTypedChannel[string](nil, "ev")
	ch := global.WithBus(bus)

	assert.Nil(t, global.bus, "original TypedChannel must remain unmodified")
	assert.Equal(t, bus, ch.bus)
}

func TestTypedChannel_MustPublish_PanicsOnNilBus(t *testing.T) {
	ch := NewTypedChannel[string](nil, "t")
	assert.Panics(t, func() {
		ch.MustPublish("data")
	})
}

func TestTypedChannel_MustSubscribe_PanicsOnNilBus(t *testing.T) {
	ch := NewTypedChannel[string](nil, "t")
	assert.Panics(t, func() {
		ch.MustSubscribe(func(s string) {})
	})
}

func TestTypedChannel_GlobalContractPattern(t *testing.T) {
	// 演示跨插件事件契约的推荐用法：
	// 在共享包中定义 TypedChannel 常量，再 WithBus 绑定真实 EventBus
	var GlobalLoginEvent = NewTypedChannel[userLoginEvent](nil, "user.login")

	bus := NewEventBus()
	publisher := GlobalLoginEvent.WithBus(bus)
	subscriber := GlobalLoginEvent.WithBus(bus)

	var received atomic.Bool
	var receivedEvent userLoginEvent
	_, err := subscriber.Subscribe(func(e userLoginEvent) {
		receivedEvent = e
		received.Store(true)
	})
	require.NoError(t, err)

	want := userLoginEvent{UserID: "bob", IP: "192.168.1.1"}
	require.NoError(t, publisher.Publish(want))

	waitFor(t, func() bool { return received.Load() }, 500*time.Millisecond)
	assert.Equal(t, want, receivedEvent)
}

func TestTypedChannel_ConcurrentPublish(t *testing.T) {
	// 并发发布强类型事件，验证无 race condition
	bus := NewEventBus()
	ch := NewTypedChannel[int](bus, "counter")

	var sum atomic.Int64
	ch.MustSubscribe(func(n int) {
		sum.Add(int64(n))
	})

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = ch.Publish(n)
		}(i)
	}
	wg.Wait()

	// 等待所有异步 handler 完成
	expected := int64(50 * 49 / 2) // 0+1+2+...+49
	waitFor(t, func() bool { return sum.Load() == expected }, time.Second)
	assert.Equal(t, expected, sum.Load())
}

func TestTypedChannel_Unsubscribe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
	bus := NewEventBus()
	ch := NewTypedChannel[string](bus, "greet")

	var count atomic.Int32
	sub, err := ch.Subscribe(func(s string) {
		count.Add(1)
	})
	require.NoError(t, err)

	// 第一次发布，应该收到
	require.NoError(t, ch.Publish("hello"))
	waitFor(t, func() bool { return count.Load() >= 1 }, 500*time.Millisecond)

	// 取消订阅
	require.NoError(t, sub.Unsubscribe())

	// 第二次发布，不应该收到
	require.NoError(t, ch.Publish("world"))
	time.Sleep(50 * time.Millisecond)
	assert.EqualValues(t, 1, count.Load(), "handler must not be called after Unsubscribe")
	})
}

// ---- 辅助函数 -----------------------------------------------------------------

// waitFor 轮询等待 condition 为 true，最长等待 timeout。
func waitFor(t *testing.T, condition func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}
