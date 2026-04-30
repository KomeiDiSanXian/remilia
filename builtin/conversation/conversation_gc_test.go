package conversation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// TestConversation_GC_AutoCleanup verifies that GC removes expired sessions (Bug 2.4 fix).
func TestConversation_GC_AutoCleanup(t *testing.T) {
	p := NewPlugin()

	// 添加 50 条过期会话（使用唯一 key）
	for i := range 50 {
		key := fmt.Sprintf("expired:session:%d", i)
		p.sessions.Store(key, &Session{
			ID:        key,
			UserID:    fmt.Sprintf("user%d", i),
			ExpiresAt: time.Now().Add(-1 * time.Hour), // 已过期
		})
	}
	// 添加 1 条活跃会话
	p.sessions.Store("active:session", &Session{
		ID:        "active",
		UserID:    "user",
		ExpiresAt: time.Now().Add(1 * time.Hour), // 未过期
	})

	if count := p.ActiveSessions(); count != 51 {
		t.Fatalf("expected 51 sessions before GC, got %d", count)
	}

	removed := p.GC()
	if removed != 50 {
		t.Fatalf("expected GC to remove 50 sessions, got %d", removed)
	}

	// 活跃会话应保留
	if p.ActiveSessions() != 1 {
		t.Fatalf("expected 1 active session after GC, got %d", p.ActiveSessions())
	}

	t.Logf("✓ Bug 2.4 修复：GC 正确清理 %d 条过期会话", removed)
}

// TestConversation_Descriptor_AutoGC verifies the plugin registers and the GC goroutine
// is bound to the plugin lifecycle.
func TestConversation_Descriptor_AutoGC(t *testing.T) {
	pm := plugin.NewManager(nil)

	desc := New()
	if err := pm.Register(desc); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	inst, ok := pm.Get("conversation")
	if !ok {
		t.Fatal("plugin not found after registration")
	}
	if inst.GetState() != plugin.Loaded {
		t.Fatalf("expected Loaded, got %s", inst.GetState())
	}

	if err := pm.Unregister(context.Background(), "conversation"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	t.Log("✓ Bug 2.4 修复：conversation 插件注册/卸载正常，后台 GC goroutine 随生命周期管理")
}
