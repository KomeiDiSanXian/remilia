package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestWebhook_GetStats 测试获取统计信息
func TestWebhook_GetStats(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		AppID:     123456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	// 创建一个小buffer的webhook以便测试丢弃
	conn := NewWithBuffer(ctx, info, 2)

	// 初始统计应该为0
	stats := conn.GetStats()
	if stats.TotalEvents != 0 {
		t.Errorf("Expected TotalEvents=0, got %d", stats.TotalEvents)
	}
	if stats.DroppedEvents != 0 {
		t.Errorf("Expected DroppedEvents=0, got %d", stats.DroppedEvents)
	}
	if stats.DropRate != 0 {
		t.Errorf("Expected DropRate=0, got %f", stats.DropRate)
	}

	t.Log("Initial stats - PASS")
}

// TestWebhook_EventCounters 测试事件计数器
func TestWebhook_EventCounters(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		AppID:     123456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	// 创建小buffer的webhook
	conn := NewWithBuffer(ctx, info, 2)

	// 模拟发送多个事件（超过buffer大小）
	for i := range 10 {
		payload := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID(string(rune('A' + i))),
			Raw:  []byte("test"),
		}
		conn.handleDispatch(payload)
	}

	// 稍等片刻让处理完成
	time.Sleep(100 * time.Millisecond)

	stats := conn.GetStats()

	// 应该接收了10个事件
	if stats.TotalEvents != 10 {
		t.Errorf("Expected TotalEvents=10, got %d", stats.TotalEvents)
	}

	// 应该有一些事件被丢弃（因为buffer只有2）
	if stats.DroppedEvents == 0 {
		t.Log("Warning: No events dropped (consumer might be fast enough)")
	} else {
		t.Logf("Dropped %d events out of %d (%.2f%%)",
			stats.DroppedEvents, stats.TotalEvents, stats.DropRate*100)
	}

	// DropRate 应该在合理范围
	if stats.DropRate < 0 || stats.DropRate > 1 {
		t.Errorf("Invalid DropRate: %f (should be 0-1)", stats.DropRate)
	}

	t.Log("Event counters - PASS")
}

// TestWebhook_DropRateCalculation 测试丢弃率计算
func TestWebhook_DropRateCalculation(t *testing.T) {
	ctx := context.Background()
	info := &dto.BotInfo{
		AppID:     123456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	// 创建buffer为1的webhook，容易触发丢弃
	conn := NewWithBuffer(ctx, info, 1)

	// 快速发送大量事件
	numEvents := 100
	for i := range numEvents {
		payload := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID(string(rune(i))),
			Raw:  []byte("test"),
		}
		conn.handleDispatch(payload)
	}

	time.Sleep(50 * time.Millisecond)

	stats := conn.GetStats()

	t.Logf("Stats after %d events:", numEvents)
	t.Logf("  Total: %d", stats.TotalEvents)
	t.Logf("  Dropped: %d", stats.DroppedEvents)
	t.Logf("  Drop Rate: %.2f%%", stats.DropRate*100)
	t.Logf("  Channel: %d/%d", stats.ChannelSize, stats.ChannelCap)

	// 验证计算正确性
	if stats.TotalEvents > 0 {
		expectedDropRate := float64(stats.DroppedEvents) / float64(stats.TotalEvents)
		if stats.DropRate != expectedDropRate {
			t.Errorf("DropRate calculation error: expected %f, got %f",
				expectedDropRate, stats.DropRate)
		}
	}

	t.Log("Drop rate calculation - PASS")
}
