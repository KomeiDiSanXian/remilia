package buildinfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGet_DefaultsToEmpty 验证未注入时返回空字符串而非 panic。
func TestGet_DefaultsToEmpty(t *testing.T) {
	commit, date := Get()
	assert.Equal(t, "", commit)
	assert.Equal(t, "", date)
}

// TestSetAndGet 验证注入后可读回。
//
// 注意：本包使用包级状态，因此不能用 t.Parallel()。
func TestSetAndGet(t *testing.T) {
	Set("abc1234", "2026-09-13T00:00:00Z")
	commit, date := Get()
	assert.Equal(t, "abc1234", commit)
	assert.Equal(t, "2026-09-13T00:00:00Z", date)
}

// TestSet_Overwrites 验证重复注入以最后一次为准。
func TestSet_Overwrites(t *testing.T) {
	Set("first", "d1")
	Set("second", "d2")
	commit, date := Get()
	assert.Equal(t, "second", commit)
	assert.Equal(t, "d2", date)
}
