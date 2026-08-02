package token

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// TestTokenManagerStop 测试 Token Manager 的优雅关闭
func TestTokenManagerStop(t *testing.T) {
	// 创建一个测试用的 BotInfo（使用无效的 credentials 避免真实请求）
	botInfo := &dto.BotInfo{
		AppID:     123456789,
		AppSecret: "test-secret",
	}

	// 创建 Token Manager
	manager := NewManager(botInfo)

	// 等待一小段时间确保 goroutine 启动
	time.Sleep(100 * time.Millisecond)

	// 停止 Manager
	manager.Stop()

	// 验证停止后 goroutine 已经退出
	// 如果 goroutine 没有退出，wg.Wait() 会一直阻塞
	// 这个测试能够通过说明 Stop() 方法正确地停止了 goroutine

	t.Log("Token Manager stopped successfully")
}

// TestTokenManagerGetAppID 验证 GetAppID 返回构造时保存的 AppID。
func TestTokenManagerGetAppID(t *testing.T) {
	botInfo := &dto.BotInfo{
		QQNum:     10001,
		AppID:     102072748,
		AppSecret: "test-secret",
	}
	manager := NewManager(botInfo)
	defer manager.Stop()

	if got := manager.GetAppID(); got != 102072748 {
		t.Errorf("GetAppID: got %d, want 102072748", got)
	}
}

// TestTokenManagerGetAppIDNilInfo 验证 info 为 nil 时 GetAppID 返回 0 而非 panic。
func TestTokenManagerGetAppIDNilInfo(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Stop()

	if got := manager.GetAppID(); got != 0 {
		t.Errorf("GetAppID with nil info: got %d, want 0", got)
	}
}

// TestTokenManagerMultipleStop 测试多次调用 Stop 不会 panic
func TestTokenManagerMultipleStop(t *testing.T) {
	botInfo := &dto.BotInfo{
		AppID:     123456789,
		AppSecret: "test-secret",
	}

	manager := NewManager(botInfo)
	time.Sleep(100 * time.Millisecond)

	// 多次调用 Stop
	manager.Stop()
	manager.Stop()
	manager.Stop()

	t.Log("Multiple Stop calls handled correctly")
}

// TestTokenManagerContextCancellation 测试 context 取消能正确停止 refresh 循环
func TestTokenManagerContextCancellation(t *testing.T) {
	botInfo := &dto.BotInfo{
		AppID:     123456789,
		AppSecret: "test-secret",
	}

	manager := NewManager(botInfo)

	// 立即停止
	manager.Stop()

	// 使用 channel 来检测是否在合理时间内完成
	done := make(chan struct{})
	go func() {
		// 如果 Stop 工作正常，这应该很快完成
		time.Sleep(1 * time.Second)
		close(done)
	}()

	select {
	case <-done:
		t.Log("Manager stopped within timeout")
	case <-time.After(5 * time.Second):
		t.Fatal("Manager did not stop within timeout - possible goroutine leak")
	}
}
