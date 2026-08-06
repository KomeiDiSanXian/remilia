package bilibili

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWatchManagerAddRemove(t *testing.T) {
	w := newWatchManager(filepath.Join(t.TempDir(), "bili"), 60*time.Second)

	w.add("chat1", 123, "主播A", &LiveInfo{RoomID: 100, IsLiving: false})
	w.add("chat1", 456, "主播B", nil)
	w.add("chat2", 123, "主播A", nil)

	if !w.contains("chat1", 123) {
		t.Fatal("chat1/123 应已订阅")
	}
	if got := len(w.list("chat1")); got != 2 {
		t.Fatalf("chat1 应有 2 条订阅，got %d", got)
	}
	if got := len(w.all()); got != 3 {
		t.Fatalf("all 应有 3 条，got %d", got)
	}

	// 移除只影响指定会话
	if !w.remove("chat1", 123) {
		t.Fatal("remove 应命中")
	}
	if w.contains("chat1", 123) {
		t.Fatal("chat1/123 应已移除")
	}
	if !w.contains("chat2", 123) {
		t.Fatal("chat2/123 应保留")
	}
	if w.remove("chat1", 123) {
		t.Fatal("重复移除应返回 false")
	}
}

func TestWatchManagerApplyLive(t *testing.T) {
	w := newWatchManager(filepath.Join(t.TempDir(), "bili"), 60*time.Second)
	w.add("chat1", 123, "主播A", &LiveInfo{RoomID: 100, IsLiving: false})

	notified := 0
	var got *LiveInfo
	w.applyLive(123, &LiveInfo{RoomID: 100, IsLiving: true, Title: "开播了"}, func(uid int64, live *LiveInfo) {
		notified++
		got = live
	})
	if notified != 1 {
		t.Fatalf("首次开播应推送 1 次，got %d", notified)
	}
	if got == nil || got.Title != "开播了" {
		t.Fatal("推送应携带直播间信息")
	}

	// 持续直播中不再推送
	w.applyLive(123, &LiveInfo{RoomID: 100, IsLiving: true}, func(uid int64, live *LiveInfo) {
		notified++
	})
	if notified != 1 {
		t.Fatalf("持续直播不应重复推送，got %d", notified)
	}

	// 下播后再开播应重新推送
	w.applyLive(123, &LiveInfo{RoomID: 100, IsLiving: false}, nil)
	w.applyLive(123, &LiveInfo{RoomID: 100, IsLiving: true, Title: "又开播了"}, func(uid int64, live *LiveInfo) {
		notified++
	})
	if notified != 2 {
		t.Fatalf("下播后再开播应推送，got %d", notified)
	}
}

func TestWatchManagerPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bili")
	w := newWatchManager(dir, 60*time.Second)
	w.add("chat1", 123, "主播A", nil)

	// 重新加载（模拟重启）
	w2 := newWatchManager(dir, 60*time.Second)
	if !w2.contains("chat1", 123) {
		t.Fatal("重启后订阅应保留")
	}
	entries := w2.list("chat1")
	if len(entries) != 1 || entries[0].Name != "主播A" {
		t.Fatalf("持久化内容不符: %+v", entries)
	}
}

func TestWatchManagerNotifier(t *testing.T) {
	w := newWatchManager(filepath.Join(t.TempDir(), "bili"), 60*time.Second)
	if w.hasNotifier() {
		t.Fatal("初始不应有 notifier")
	}
	w.setNotifier(func(chatID, msg string) error { return nil })
	if !w.hasNotifier() {
		t.Fatal("注册后应有 notifier")
	}
}
