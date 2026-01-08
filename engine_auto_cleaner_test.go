package remilia

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestEngine_AutoTempMatcherCleaner(t *testing.T) {
	engine := NewEngine()

	// 验证清理器已自动启动
	interval := engine.GetTempMatcherCleanInterval()
	assert.Equal(t, 5*time.Minute, interval)
}

func TestEngine_SetTempMatcherCleanInterval(t *testing.T) {
	engine := NewEngine()

	// 修改清理间隔
	engine.SetTempMatcherCleanInterval(10 * time.Minute)

	// 验证已更新
	interval := engine.GetTempMatcherCleanInterval()
	assert.Equal(t, 10*time.Minute, interval)
}

func TestEngine_DisableTempMatcherCleaner(t *testing.T) {
	engine := NewEngine()

	// 禁用清理器
	engine.SetTempMatcherCleanInterval(0)

	// 验证已禁用
	interval := engine.GetTempMatcherCleanInterval()
	assert.Equal(t, time.Duration(0), interval)
}

func TestEngine_AutoCleanExpiredMatchers(t *testing.T) {
	engine := NewEngine()
	engine.SetTempMatcherCleanInterval(50 * time.Millisecond)

	// 添加一个会很快过期的临时 matcher
	engine.On(dto.C2CMessageCreate).SetTempWithTimeout(50 * time.Millisecond).Handle(func(ctx *Context) {})

	// 验证已添加
	assert.Equal(t, 1, engine.GetTempMatcherCount())

	// 等待过期和清理
	time.Sleep(150 * time.Millisecond)

	// 验证已被清理
	assert.Equal(t, 0, engine.GetTempMatcherCount())
}

func TestEngine_TempMatcherCleanerRestart(t *testing.T) {
	engine := NewEngine()

	// 记录初始清理器
	initialInterval := engine.GetTempMatcherCleanInterval()
	assert.Equal(t, 5*time.Minute, initialInterval)

	// 修改间隔（应该重启清理器）
	engine.SetTempMatcherCleanInterval(1 * time.Minute)
	assert.Equal(t, 1*time.Minute, engine.GetTempMatcherCleanInterval())

	// 再次修改
	engine.SetTempMatcherCleanInterval(2 * time.Minute)
	assert.Equal(t, 2*time.Minute, engine.GetTempMatcherCleanInterval())
}

func TestEngine_TempMatcherCleaner_NoMemoryLeak(t *testing.T) {
	engine := NewEngine()
	engine.SetTempMatcherCleanInterval(10 * time.Millisecond)

	// 添加大量临时 matcher
	for i := 0; i < 100; i++ {
		engine.On(dto.C2CMessageCreate).SetTempWithTimeout(50 * time.Millisecond).Handle(func(ctx *Context) {})
	}

	// 临时 matcher 不在 State 中，应检查 TempMatcherCount
	assert.Equal(t, 100, engine.GetTempMatcherCount())
	assert.Equal(t, 0, engine.GetMatcherCount())

	// 等待清理
	time.Sleep(100 * time.Millisecond)

	// 验证已全部清理
	assert.Equal(t, 0, engine.GetTempMatcherCount())
}

func TestEngine_TempMatcherCleaner_MixedMatchers(t *testing.T) {
	engine := NewEngine()
	engine.SetTempMatcherCleanInterval(20 * time.Millisecond)

	// 添加永久 matcher
	engine.OnC2C().Handle(func(ctx *Context) {})

	// 添加临时 matcher
	engine.On(dto.C2CMessageCreate).SetTempWithTimeout(30 * time.Millisecond).Handle(func(ctx *Context) {})
	engine.On(dto.C2CMessageCreate).SetTempWithTimeout(30 * time.Millisecond).Handle(func(ctx *Context) {})

	// 1 永久 (State), 2 临时 (TempManager)
	assert.Equal(t, 1, engine.GetMatcherCount())
	assert.Equal(t, 2, engine.GetTempMatcherCount())

	// 等待临时 matcher 过期
	time.Sleep(60 * time.Millisecond)

	// 验证只清理了临时 matcher
	assert.Equal(t, 1, engine.GetMatcherCount())
	assert.Equal(t, 0, engine.GetTempMatcherCount())
}

func TestEngine_GetTempMatcherCleanInterval(t *testing.T) {
	engine := NewEngine()

	// 默认值
	interval := engine.GetTempMatcherCleanInterval()
	assert.Equal(t, 5*time.Minute, interval)

	// 修改后
	engine.SetTempMatcherCleanInterval(3 * time.Minute)
	interval = engine.GetTempMatcherCleanInterval()
	assert.Equal(t, 3*time.Minute, interval)
}
