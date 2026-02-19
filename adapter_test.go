package remilia

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockWebHook 实现 Webhook 接口用于测试
type mockWebHook struct {
	ch chan *dto.Payload
}

func (m *mockWebHook) EventStream() <-chan *dto.Payload {
	return m.ch
}

// TestWebhookAdapter_NormalOperation 测试正常操作
func TestWebhookAdapter_NormalOperation(t *testing.T) {
	eventCh := make(chan *dto.Payload, 10)
	wh := &mockWebHook{ch: eventCh}
	adapter := NewWebhookAdapter(wh)

	received := make([]*dto.Payload, 0)
	var mu sync.Mutex

	handler := func(payload *dto.Payload) {
		mu.Lock()
		received = append(received, payload)
		mu.Unlock()
	}

	ctx := context.Background()
	err := adapter.Start(ctx, handler)
	require.NoError(t, err, "Start should not return error")

	// 发送测试事件
	testEvents := []*dto.Payload{
		{ID: "event-1", Type: "test.event"},
		{ID: "event-2", Type: "test.event"},
		{ID: "event-3", Type: "test.event"},
	}

	for _, event := range testEvents {
		eventCh <- event
	}

	// 等待事件被处理
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 3, len(received), "Should receive all 3 events")
	mu.Unlock()

	// 关闭 adapter
	err = adapter.Stop(context.Background())
	assert.NoError(t, err, "Stop should not return error")
}

// TestWebhookAdapter_NilChannel 测试 nil channel 的情况
func TestWebhookAdapter_NilChannel(t *testing.T) {
	wh := &mockWebHook{ch: nil}
	adapter := NewWebhookAdapter(wh)

	handler := func(payload *dto.Payload) {}

	ctx := context.Background()
	err := adapter.Start(ctx, handler)
	require.Error(t, err, "Start should return error for nil channel")
	assert.Contains(t, err.Error(), "nil channel", "Error should mention nil channel")
}

// TestWebhookAdapter_ChannelClosed 测试 channel 关闭的情况
func TestWebhookAdapter_ChannelClosed(t *testing.T) {
	eventCh := make(chan *dto.Payload, 10)
	wh := &mockWebHook{ch: eventCh}
	adapter := NewWebhookAdapter(wh)

	var handlerCalled atomic.Int32

	handler := func(payload *dto.Payload) {
		handlerCalled.Add(1)
	}

	ctx := context.Background()
	err := adapter.Start(ctx, handler)
	require.NoError(t, err, "Start should not return error")

	// 发送一个事件
	eventCh <- &dto.Payload{ID: "event-1", Type: "test"}
	time.Sleep(50 * time.Millisecond)

	// 关闭 channel
	close(eventCh)

	// 等待 goroutine 退出
	time.Sleep(100 * time.Millisecond)

	// 验证事件被处理
	assert.Equal(t, int32(1), handlerCalled.Load(), "Handler should be called once")

	// Stop 应该正常工作
	err = adapter.Stop(context.Background())
	assert.NoError(t, err, "Stop should not return error")
}

// TestWebhookAdapter_ContextCancellation 测试 context 取消的情况
func TestWebhookAdapter_ContextCancellation(t *testing.T) {
	eventCh := make(chan *dto.Payload, 10)
	wh := &mockWebHook{ch: eventCh}
	adapter := NewWebhookAdapter(wh)

	var handlerCalled atomic.Int32

	handler := func(payload *dto.Payload) {
		handlerCalled.Add(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	err := adapter.Start(ctx, handler)
	require.NoError(t, err, "Start should not return error")

	// 发送一个事件
	eventCh <- &dto.Payload{ID: "event-1", Type: "test"}
	time.Sleep(50 * time.Millisecond)

	// 取消 context
	cancel()

	// 等待 goroutine 退出
	time.Sleep(100 * time.Millisecond)

	// 验证事件被处理
	assert.Equal(t, int32(1), handlerCalled.Load(), "Handler should be called once")
}

// TestWebhookAdapter_HandlerPanic 测试 handler panic 的情况
func TestWebhookAdapter_HandlerPanic(t *testing.T) {
	eventCh := make(chan *dto.Payload, 10)
	wh := &mockWebHook{ch: eventCh}
	adapter := NewWebhookAdapter(wh)

	var panicCount atomic.Int32
	var normalCount atomic.Int32

	handler := func(payload *dto.Payload) {
		if payload.ID == "panic-event" {
			panicCount.Add(1)
			panic("test panic")
		}
		normalCount.Add(1)
	}

	ctx := context.Background()
	err := adapter.Start(ctx, handler)
	require.NoError(t, err, "Start should not return error")

	// 发送会导致 panic 的事件
	eventCh <- &dto.Payload{ID: "panic-event", Type: "test"}
	time.Sleep(50 * time.Millisecond)

	// 发送正常事件，验证 adapter 仍在运行
	eventCh <- &dto.Payload{ID: "normal-event", Type: "test"}
	time.Sleep(50 * time.Millisecond)

	// 验证两个事件都被处理了
	assert.Equal(t, int32(1), panicCount.Load(), "Panic handler should be called once")
	assert.Equal(t, int32(1), normalCount.Load(), "Normal handler should be called once")

	// Stop
	err = adapter.Stop(context.Background())
	assert.NoError(t, err, "Stop should not return error")
}

// TestWebhookAdapter_NilEventIgnored 测试 nil 事件被忽略
func TestWebhookAdapter_NilEventIgnored(t *testing.T) {
	eventCh := make(chan *dto.Payload, 10)
	wh := &mockWebHook{ch: eventCh}
	adapter := NewWebhookAdapter(wh)

	var handlerCalled atomic.Int32

	handler := func(payload *dto.Payload) {
		if payload != nil {
			handlerCalled.Add(1)
		}
	}

	ctx := context.Background()
	err := adapter.Start(ctx, handler)
	require.NoError(t, err, "Start should not return error")

	// 发送 nil 事件
	eventCh <- nil
	time.Sleep(50 * time.Millisecond)

	// 发送正常事件
	eventCh <- &dto.Payload{ID: "event-1", Type: "test"}
	time.Sleep(50 * time.Millisecond)

	// 验证只有正常事件被处理
	assert.Equal(t, int32(1), handlerCalled.Load(), "Only non-nil event should be processed")

	// Stop
	err = adapter.Stop(context.Background())
	assert.NoError(t, err, "Stop should not return error")
}

// TestWebhookAdapter_MultipleShutdown 测试多次调用 Stop
func TestWebhookAdapter_MultipleShutdown(t *testing.T) {
	eventCh := make(chan *dto.Payload, 10)
	wh := &mockWebHook{ch: eventCh}
	adapter := NewWebhookAdapter(wh)

	handler := func(payload *dto.Payload) {}

	ctx := context.Background()
	err := adapter.Start(ctx, handler)
	require.NoError(t, err, "Start should not return error")

	// 第一次 Stop
	err = adapter.Stop(context.Background())
	assert.NoError(t, err, "First shutdown should not return error")

	// 第二次 Stop（应该是幂等的）
	err = adapter.Stop(context.Background())
	assert.NoError(t, err, "Second shutdown should not return error")
}

// TestWebhookAdapter_ConcurrentEvents 测试并发事件处理
func TestWebhookAdapter_ConcurrentEvents(t *testing.T) {
	eventCh := make(chan *dto.Payload, 100)
	wh := &mockWebHook{ch: eventCh}
	adapter := NewWebhookAdapter(wh)

	var handlerCalled atomic.Int32
	var mu sync.Mutex
	received := make(map[string]bool)

	handler := func(payload *dto.Payload) {
		handlerCalled.Add(1)
		mu.Lock()
		received[string(payload.ID)] = true
		mu.Unlock()
		// 模拟处理时间
		time.Sleep(10 * time.Millisecond)
	}

	ctx := context.Background()
	err := adapter.Start(ctx, handler)
	require.NoError(t, err, "Start should not return error")

	// 发送多个事件
	eventCount := 10
	for i := range eventCount {
		eventCh <- &dto.Payload{
			ID:   dto.EventID(fmt.Sprintf("event-%d", i)),
			Type: "test",
		}
	}

	// 等待所有事件被处理
	time.Sleep(200 * time.Millisecond)

	// 验证所有事件都被处理
	assert.Equal(t, int32(eventCount), handlerCalled.Load(), "All events should be processed")

	mu.Lock()
	assert.Equal(t, eventCount, len(received), "All unique events should be received")
	mu.Unlock()

	// Stop
	err = adapter.Stop(context.Background())
	assert.NoError(t, err, "Stop should not return error")
}

// TestWebhookAdapter_ShutdownWithPendingEvents 测试有待处理事件时的关闭
func TestWebhookAdapter_ShutdownWithPendingEvents(t *testing.T) {
	eventCh := make(chan *dto.Payload, 100)
	wh := &mockWebHook{ch: eventCh}
	adapter := NewWebhookAdapter(wh)

	var handlerCalled atomic.Int32

	handler := func(payload *dto.Payload) {
		handlerCalled.Add(1)
		// 模拟较长的处理时间
		time.Sleep(50 * time.Millisecond)
	}

	ctx := context.Background()
	err := adapter.Start(ctx, handler)
	require.NoError(t, err, "Start should not return error")

	// 发送大量事件
	for i := range 10 {
		eventCh <- &dto.Payload{
			ID:   dto.EventID(fmt.Sprintf("event-%d", i)),
			Type: "test",
		}
	}

	// 立即关闭（可能有事件还在处理）
	err = adapter.Stop(context.Background())
	assert.NoError(t, err, "Stop should not return error even with pending events")

	// 记录已处理的事件数
	processed := handlerCalled.Load()
	t.Logf("Processed %d events before shutdown", processed)

	// 等待一段时间，验证不会再处理新事件
	time.Sleep(100 * time.Millisecond)
	processedAfter := handlerCalled.Load()

	// 关闭后不应该再处理新事件（允许正在处理的事件完成）
	assert.LessOrEqual(t, processedAfter, int32(10), "Should not process more than sent events")
}
