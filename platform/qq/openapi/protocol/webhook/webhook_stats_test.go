package webhook

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// TestWebhook_GetStats 测试获取统计信息
func TestWebhook_GetStats(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     123456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	conn := NewWithBuffer(info, 2)

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
	info := &dto.BotInfo{
		AppID:     123456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	conn := NewWithBuffer(info, 2)

	for i := range 10 {
		payload := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID(string(rune('A' + i))),
			Raw:  []byte("test"),
		}
		conn.handleDispatch(payload)
	}

	time.Sleep(100 * time.Millisecond)

	stats := conn.GetStats()

	if stats.TotalEvents != 10 {
		t.Errorf("Expected TotalEvents=10, got %d", stats.TotalEvents)
	}

	if stats.DroppedEvents == 0 {
		t.Log("Warning: No events dropped (consumer might be fast enough)")
	} else {
		t.Logf("Dropped %d events out of %d (%.2f%%)",
			stats.DroppedEvents, stats.TotalEvents, stats.DropRate*100)
	}

	if stats.DropRate < 0 || stats.DropRate > 1 {
		t.Errorf("Invalid DropRate: %f (should be 0-1)", stats.DropRate)
	}

	t.Log("Event counters - PASS")
}

// TestWebhook_DropRateCalculation 测试丢弃率计算
func TestWebhook_DropRateCalculation(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     123456,
		Token:     "test_token",
		AppSecret: "test_secret",
	}

	conn := NewWithBuffer(info, 1)

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

	if stats.TotalEvents > 0 {
		expectedDropRate := float64(stats.DroppedEvents) / float64(stats.TotalEvents)
		if stats.DropRate != expectedDropRate {
			t.Errorf("DropRate calculation error: expected %f, got %f",
				expectedDropRate, stats.DropRate)
		}
	}

	t.Log("Drop rate calculation - PASS")
}
