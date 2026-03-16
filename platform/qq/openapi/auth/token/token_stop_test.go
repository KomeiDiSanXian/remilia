package token

import (
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// TestTokenManager_StopIdempotent 测试 Stop 方法的幂等性
func TestTokenManager_StopIdempotent(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123456,
		Token: "test_token",
	}

	mgr := NewManager(info)

	// 等待一小段时间让 manager 启动
	time.Sleep(100 * time.Millisecond)

	// 第一次停止
	mgr.Stop()

	// 第二次停止应该安全（幂等）
	mgr.Stop()

	// 第三次停止也应该安全
	mgr.Stop()

	t.Log("Stop is idempotent - PASS")
}

// TestTokenManager_GetTokenAfterStop 测试停止后获取 token
func TestTokenManager_GetTokenAfterStop(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123456,
		Token: "test_token",
	}

	mgr := NewManager(info)
	time.Sleep(100 * time.Millisecond)

	// 停止 manager
	mgr.Stop()

	// 尝试获取 token - 应该返回空字符串且不panic
	token := mgr.GetToken()
	if token != "" {
		t.Errorf("Expected empty token after stop, got: %s", token)
	}

	t.Log("GetToken after Stop returns empty - PASS")
}

// TestTokenManager_ConcurrentStopAndGetToken 测试并发停止和获取token
func TestTokenManager_ConcurrentStopAndGetToken(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123456,
		Token: "test_token",
	}

	mgr := NewManager(info)
	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	numGoroutines := 50

	// 启动多个goroutine并发调用Stop
	for i := 0; i < numGoroutines/2; i++ {
		wg.Go(func() {
			mgr.Stop()
		})
	}

	// 启动多个goroutine并发调用GetToken
	for i := 0; i < numGoroutines/2; i++ {
		wg.Go(func() {
			_ = mgr.GetToken()
		})
	}

	wg.Wait()

	t.Log("Concurrent Stop and GetToken - PASS (no panic)")
}

// TestTokenManager_WaitReadyAfterStop 测试停止后等待ready
func TestTokenManager_WaitReadyAfterStop(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123456,
		Token: "test_token",
	}

	mgr := NewManager(info)
	time.Sleep(100 * time.Millisecond)

	// 停止 manager
	mgr.Stop()

	// 尝试等待 ready - 应该立即返回错误
	err := mgr.WaitReadyWithTimeout(1 * time.Second)
	if err == nil {
		t.Error("Expected error when WaitReady called after Stop, got nil")
	} else if err.Error() != "token manager has been stopped" {
		t.Errorf("Expected 'token manager has been stopped' error, got: %v", err)
	}

	t.Log("WaitReady after Stop returns error - PASS")
}

// TestTokenManager_GetTokenBeforeReady 测试在token准备前获取
func TestTokenManager_GetTokenBeforeReady(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123456,
		Token: "test_token",
	}

	mgr := NewManager(info)

	// 立即获取token（可能还未ready）
	token := mgr.GetToken()
	if token != "" {
		t.Logf("Token ready immediately: %s", token)
	} else {
		t.Log("Token not ready yet (expected)")
	}

	// 清理
	mgr.Stop()
	t.Log("GetToken before ready handled gracefully - PASS")
}

// TestTokenManager_StopDuringRefresh 测试在刷新期间停止
func TestTokenManager_StopDuringRefresh(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123456,
		Token: "test_token",
	}

	mgr := NewManager(info)

	// 启动多个goroutine持续获取token
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_ = mgr.GetToken()
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// 等待一段时间
	time.Sleep(200 * time.Millisecond)

	// 停止manager
	mgr.Stop()
	close(done)

	// 再次尝试获取应该返回空
	token := mgr.GetToken()
	if token != "" {
		t.Errorf("Expected empty token after stop during refresh, got: %s", token)
	}

	t.Log("Stop during refresh - PASS")
}
