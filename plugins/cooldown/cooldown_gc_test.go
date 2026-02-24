package cooldown

import (
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// TestCooldown_GC_AutoCleanup verifies that CleanExpired removes expired records (Bug 2.2 fix).
func TestCooldown_GC_AutoCleanup(t *testing.T) {
	p := NewPlugin()

	// 手动添加 100 条过期记录（使用唯一 key 避免覆盖）
	p.mu.Lock()
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("user%d:cmd", i)
		p.records[key] = &entry{lastUsed: time.Now().Add(-48 * time.Hour)} // 已过期 48h
	}
	p.mu.Unlock()

	if count := p.ActiveCount(); count != 100 {
		t.Fatalf("expected 100 records before GC, got %d", count)
	}

	// 手动触发 CleanExpired，模拟 GC goroutine 的行为
	removed := p.CleanExpired(maxEntryAge)
	if removed != 100 {
		t.Fatalf("expected CleanExpired to remove 100 records, got %d", removed)
	}

	if count := p.ActiveCount(); count != 0 {
		t.Fatalf("expected 0 records after GC, got %d", count)
	}

	t.Logf("✓ Bug 2.2 修复：CleanExpired 正确清理 %d 条过期记录", removed)
}

// TestCooldown_GC_KeepsActive verifies that GC does not remove active records.
func TestCooldown_GC_KeepsActive(t *testing.T) {
	p := NewPlugin()

	// 添加活跃记录（刚使用）
	p.mu.Lock()
	p.records["active:cmd"] = &entry{lastUsed: time.Now()}
	// 添加过期记录
	p.records["expired:cmd"] = &entry{lastUsed: time.Now().Add(-48 * time.Hour)}
	p.mu.Unlock()

	removed := p.CleanExpired(maxEntryAge)
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if _, exists := p.records["active:cmd"]; !exists {
		t.Fatal("active record should not be removed by GC")
	}
	if _, exists := p.records["expired:cmd"]; exists {
		t.Fatal("expired record should have been removed by GC")
	}
}

// TestCooldown_Descriptor_AutoGC verifies the Descriptor can be registered and unloaded.
func TestCooldown_Descriptor_AutoGC(t *testing.T) {
	pm := plugin.NewManager(nil)

	desc := New()
	if err := pm.RegisterV2(desc); err != nil {
		t.Fatalf("RegisterV2 failed: %v", err)
	}

	inst, ok := pm.Get("cooldown")
	if !ok {
		t.Fatal("plugin not found after registration")
	}
	if inst.GetState() != plugin.Loaded {
		t.Fatalf("expected Loaded, got %s", inst.GetState())
	}

	// 卸载时 goroutine 应随之停止（GoroutineManager 负责）
	if err := pm.Unregister("cooldown"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	t.Log("✓ Bug 2.2 修复：cooldown 插件注册/卸载正常，后台 GC goroutine 随生命周期管理")
}
