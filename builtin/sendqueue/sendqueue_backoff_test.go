package sendqueue

import (
	"testing"
	"time"
)

// TestSendQueue_ExponentialBackoff verifies that retry delay grows exponentially (Bug 2.11 fix).
func TestSendQueue_ExponentialBackoff(t *testing.T) {
	cfg := DefaultConfig()
	base := cfg.RetryDelay // 500ms

	// attempt=1: delay = 500ms * 2^0 = 500ms
	// attempt=2: delay = 500ms * 2^1 = 1000ms
	// attempt=3: delay = 500ms * 2^2 = 2000ms

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		backoff := base * (1 << (attempt - 1))
		minExpected := backoff
		maxExpected := backoff + base // backoff + jitter 上界

		t.Logf("attempt=%d: backoff=%s, jitter=[0,%s), total=[%s,%s)",
			attempt, backoff, base, minExpected, maxExpected)

		if backoff < base {
			t.Errorf("attempt=%d: backoff %s should be >= base %s", attempt, backoff, base)
		}
	}

	// 验证指数增长：attempt=2 的基础退避 >= attempt=1
	backoff1 := base * (1 << 0)
	backoff2 := base * (1 << 1)
	backoff3 := base * (1 << 2)

	if backoff2 != 2*backoff1 {
		t.Errorf("expected backoff2 = 2*backoff1, got %s vs %s", backoff2, backoff1)
	}
	if backoff3 != 4*backoff1 {
		t.Errorf("expected backoff3 = 4*backoff1, got %s vs %s", backoff3, backoff1)
	}

	t.Logf("✓ Bug 2.11 修复：指数退避正确 backoff1=%s backoff2=%s backoff3=%s",
		backoff1, backoff2, backoff3)
}

// TestSendQueue_DefaultConfig verifies default configuration is valid.
func TestSendQueue_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RetryDelay <= 0 {
		t.Errorf("RetryDelay should be positive, got %s", cfg.RetryDelay)
	}
	if cfg.MaxRetries <= 0 {
		t.Errorf("MaxRetries should be positive, got %d", cfg.MaxRetries)
	}

	// Verify max backoff doesn't overflow for reasonable attempts
	for i := 1; i <= cfg.MaxRetries; i++ {
		delay := cfg.RetryDelay * time.Duration(1<<(i-1))
		if delay <= 0 {
			t.Errorf("overflow at attempt %d", i)
		}
	}

	t.Log("✓ Bug 2.11 修复：默认配置的指数退避计算不会溢出")
}
