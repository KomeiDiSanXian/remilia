package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestNewWithOptions_DedupEnabled(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		QQNum:     123,
		AppID:     456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	opts := DedupOptions{
		Enable:           true,
		Shards:           512,
		LifeWindow:       2 * time.Minute,
		CleanWindow:      30 * time.Second,
		MaxEntrySize:     2048,
		HardMaxCacheSize: 512,
	}

	conn := NewWithOptions(ctx, info, 100, opts)
	assert.NotNil(t, conn)
	assert.NotNil(t, conn.bigCache, "BigCache should be initialized when dedup is enabled")
	assert.Equal(t, 100, cap(conn.eventChan))
}

func TestNewWithOptions_DedupDisabled(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		QQNum:     123,
		AppID:     456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	opts := DedupOptions{
		Enable: false,
	}

	conn := NewWithOptions(ctx, info, 50, opts)
	assert.NotNil(t, conn)
	assert.Nil(t, conn.bigCache, "BigCache should be nil when dedup is disabled")
	assert.Equal(t, 50, cap(conn.eventChan))
}

func TestNewWithOptions_DefaultBuffer(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		QQNum:     123,
		AppID:     456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	opts := DedupOptions{Enable: false}

	// Buffer <=0 应该默认为 1
	conn := NewWithOptions(ctx, info, 0, opts)
	assert.Equal(t, 1, cap(conn.eventChan))

	conn2 := NewWithOptions(ctx, info, -5, opts)
	assert.Equal(t, 1, cap(conn2.eventChan))
}

func TestNew_UsesDedupByDefault(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		QQNum:     123,
		AppID:     456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	conn := NewWebhook(ctx, info)
	assert.NotNil(t, conn)
	// NewWebhook() 应该启用去重
	assert.NotNil(t, conn.bigCache)
}

func TestNewWithBuffer_UsesDedupByDefault(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		QQNum:     123,
		AppID:     456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	conn := NewWithBuffer(ctx, info, 200)
	assert.NotNil(t, conn)
	assert.Equal(t, 200, cap(conn.eventChan))
	// NewWithBuffer() 也应该启用去重
	assert.NotNil(t, conn.bigCache)
}

func TestHandleDispatch_WithoutDedup(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		QQNum:     123,
		AppID:     456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	// 禁用去重
	opts := DedupOptions{Enable: false}
	conn := NewWithOptions(ctx, info, 10, opts)

	payload := &dto.Payload{
		ID:   "test_event_1",
		Type: dto.C2CMessageCreate,
	}

	// 调用 handleDispatch（这是内部方法，通过 Handle 间接测试）
	conn.handleDispatch(payload)

	// 验证事件被发送到通道
	select {
	case received := <-conn.eventChan:
		assert.Equal(t, "test_event_1", string(received.ID))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event was not dispatched to channel")
	}
}

func TestHandleDispatch_WithDedup(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		QQNum:     123,
		AppID:     456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	// 启用去重
	opts := DedupOptions{
		Enable:           true,
		Shards:           64,
		LifeWindow:       1 * time.Minute,
		HardMaxCacheSize: 128,
	}
	conn := NewWithOptions(ctx, info, 10, opts)

	payload := &dto.Payload{
		ID:   "duplicate_event",
		Type: dto.C2CMessageCreate,
		Raw:  []byte(`{"id":"duplicate_event"}`),
	}

	// 第一次分发应该成功
	conn.handleDispatch(payload)
	select {
	case <-conn.eventChan:
		// 成功接收
	case <-time.After(100 * time.Millisecond):
		t.Fatal("First dispatch failed")
	}

	// 第二次分发相同事件应该被去重
	conn.handleDispatch(payload)
	select {
	case <-conn.eventChan:
		t.Fatal("Duplicate event should not be dispatched")
	case <-time.After(100 * time.Millisecond):
		// 正确：没有重复事件被分发
	}
}
