package middleware

import (
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestLogging(t *testing.T) {
	mw := Logging()
	called := false

	handler := mw(func(ctx *remilia.Context) error {
		called = true
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.NoError(t, err)
	assert.True(t, called)
}

func TestLoggingWithError(t *testing.T) {
	mw := Logging()
	testErr := errors.New("test error")

	handler := mw(func(ctx *remilia.Context) error {
		return testErr
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.Equal(t, testErr, err)
}

func TestRecover(t *testing.T) {
	mw := Recover()

	handler := mw(func(ctx *remilia.Context) error {
		panic("test panic")
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic")
	assert.Contains(t, err.Error(), "test panic")
}

func TestRecoverNoError(t *testing.T) {
	mw := Recover()

	handler := mw(func(ctx *remilia.Context) error {
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.NoError(t, err)
}

func TestAuth_Allowed(t *testing.T) {
	mw := Auth(func(ctx *remilia.Context) bool {
		return true
	})

	called := false
	handler := mw(func(ctx *remilia.Context) error {
		called = true
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.NoError(t, err)
	assert.True(t, called)
}

func TestAuth_Denied(t *testing.T) {
	mw := Auth(func(ctx *remilia.Context) bool {
		return false
	})

	called := false
	handler := mw(func(ctx *remilia.Context) error {
		called = true
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
	assert.False(t, called)
}

func TestTimeout(t *testing.T) {
	// 测试超时情况
	mw := Timeout(50 * time.Millisecond)

	handler := mw(func(ctx *remilia.Context) error {
		time.Sleep(200 * time.Millisecond) // 超过超时时间
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	start := time.Now()
	err := handler(ctx)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	assert.Less(t, elapsed, 150*time.Millisecond) // 应该在超时时间附近
}

func TestTimeoutSuccess(t *testing.T) {
	// 测试未超时情况
	mw := Timeout(200 * time.Millisecond)

	called := false
	handler := mw(func(ctx *remilia.Context) error {
		time.Sleep(50 * time.Millisecond)
		called = true
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.NoError(t, err)
	assert.True(t, called)
}

func TestRequestID(t *testing.T) {
	mw := RequestID()

	var capturedID string
	handler := mw(func(ctx *remilia.Context) error {
		// 检查 request_id 是否被设置
		requestID, ok := ctx.GetState("request_id")
		assert.True(t, ok)
		assert.NotEmpty(t, requestID)
		capturedID = requestID.(string)
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.NoError(t, err)
	assert.NotEmpty(t, capturedID)
}

func TestRateLimitTokenBucket_SharedBucket(t *testing.T) {
	// 共享桶：keyFn 为 nil
	mw := RateLimitTokenBucket(2, 2, nil)

	handler := mw(func(ctx *remilia.Context) error {
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	// 前两次应该成功
	assert.NoError(t, handler(ctx))
	assert.NoError(t, handler(ctx))

	// 第三次应该被限流
	err := handler(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
}

func TestRateLimitTokenBucket_PerKeyBucket(t *testing.T) {
	// 按 key 限流 - 使用 Context State 模拟不同用户
	mw := RateLimitTokenBucket(1, 1, func(ctx *remilia.Context) string {
		if userID, ok := ctx.GetState("user_id"); ok {
			return userID.(string)
		}
		return "default"
	})

	handler := mw(func(ctx *remilia.Context) error {
		return nil
	})

	// 两个不同的用户
	ctx1 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	ctx1.SetState("user_id", "user1")

	ctx2 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	ctx2.SetState("user_id", "user2")

	// user1 第一次成功
	assert.NoError(t, handler(ctx1))
	// user1 第二次被限流
	err1 := handler(ctx1)
	assert.Error(t, err1)

	// user2 第一次成功（独立的桶）
	assert.NoError(t, handler(ctx2))
}

func TestMetrics(t *testing.T) {
	mw := Metrics()

	called := false
	handler := mw(func(ctx *remilia.Context) error {
		called = true
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.NoError(t, err)
	assert.True(t, called)
}

func TestPrometheusMetrics(t *testing.T) {
	mw := PrometheusMetrics("test")

	called := false
	handler := mw(func(ctx *remilia.Context) error {
		called = true
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	err := handler(ctx)

	assert.NoError(t, err)
	assert.True(t, called)
}
