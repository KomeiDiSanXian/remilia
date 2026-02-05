package remilia

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// adapterTestWebhook 实现 Webhook 接口用于测试
type adapterTestWebhook struct {
	ch chan *dto.Payload
}

func (m *adapterTestWebhook) EventStream() <-chan *dto.Payload {
	return m.ch
}

// TestAdapter_ConcurrentStart 测试并发 Start 调用
func TestAdapter_ConcurrentStart(t *testing.T) {
	wh := &adapterTestWebhook{
		ch: make(chan *dto.Payload, 10),
	}
	adapter := NewWebhookAdapter(wh)

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	numGoroutines := 10
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler := func(p *dto.Payload) {}
			err := adapter.Start(context.Background(), handler)
			if err != nil {
				if err.Error() == "adapter is already starting or started" {
					errorCount.Add(1)
				}
			} else {
				successCount.Add(1)
			}
		}()
	}

	wg.Wait()

	if successCount.Load() != 1 {
		t.Errorf("Expected exactly 1 successful Start, got %d", successCount.Load())
	}

	t.Logf("Success: %d, Errors: %d", successCount.Load(), errorCount.Load())

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	adapter.Stop(ctx)

	t.Log("Concurrent Start protection - PASS")
}

// TestAdapter_NilEventStream 测试 nil EventStream
func TestAdapter_NilEventStream(t *testing.T) {
	wh := &adapterTestWebhook{
		ch: nil,
	}
	adapter := NewWebhookAdapter(wh)

	handler := func(p *dto.Payload) {}
	err := adapter.Start(context.Background(), handler)

	if err == nil {
		t.Error("Expected error for nil EventStream, got nil")
	}

	if err.Error() != "EventStream returned nil channel" {
		t.Errorf("Expected 'EventStream returned nil channel' error, got: %v", err)
	}

	t.Log("Nil EventStream handling - PASS")
}
