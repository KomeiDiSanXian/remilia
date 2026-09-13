package buildinfo

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// resetState 把包级状态置回「从未注入」。
//
// 本包的状态是包级 atomic.Value，各用例对它的前提不同（一个要求未注入，
// 两个要求已注入），因此每个用例开头都要显式复位，否则结果取决于执行顺序——
// `go test -shuffle=on` 会随机化顺序，TestGet_DefaultsToEmpty 可能落在
// TestSetAndGet 之后而读到它写入的值。
//
// 直接把零值赋回变量：atomic.Value 的零值 Load 返回 nil，正是未注入的状态。
// 不能改成 Store 一个零值 Info，那样 Load 会返回非 nil，与「未注入」不等价。
func resetState() {
	current = atomic.Value{}
}

// TestGet_DefaultsToEmpty 验证未注入时返回空字符串而非 panic。
func TestGet_DefaultsToEmpty(t *testing.T) {
	resetState()

	commit, date := Get()
	assert.Equal(t, "", commit)
	assert.Equal(t, "", date)
}

// TestSetAndGet 验证注入后可读回。
//
// 注意：本包使用包级状态，因此不能用 t.Parallel()。
func TestSetAndGet(t *testing.T) {
	resetState()

	Set("abc1234", "2026-09-13T00:00:00Z")
	commit, date := Get()
	assert.Equal(t, "abc1234", commit)
	assert.Equal(t, "2026-09-13T00:00:00Z", date)
}

// TestSet_Overwrites 验证重复注入以最后一次为准。
func TestSet_Overwrites(t *testing.T) {
	resetState()

	Set("first", "d1")
	Set("second", "d2")
	commit, date := Get()
	assert.Equal(t, "second", commit)
	assert.Equal(t, "d2", date)
}
