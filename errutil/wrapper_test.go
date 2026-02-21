package errutil_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew 测试 New 创建哨兵错误
func TestNew(t *testing.T) {
	err := errutil.New("sentinel error")
	assert.Equal(t, "sentinel error", err.Error())

	// 相同消息的两次 New 返回不同实例（和 errors.New 一样）
	err2 := errutil.New("sentinel error")
	assert.False(t, errors.Is(err, err2), "two New calls should not be equal")
}

// TestNewf 测试 Newf 格式化错误
func TestNewf(t *testing.T) {
	err := errutil.Newf("invalid value: %d", 42)
	assert.Equal(t, "invalid value: 42", err.Error())
}

// TestWrap 测试 Wrap 保留错误链
func TestWrap(t *testing.T) {
	sentinel := errutil.New("base error")
	wrapped := errutil.Wrap(sentinel, "operation failed")
	assert.Equal(t, "operation failed: base error", wrapped.Error())
	assert.True(t, errors.Is(wrapped, sentinel), "Wrap should preserve error chain")
}

// TestWrap_NilReturnsNil 测试 Wrap(nil) 返回 nil
func TestWrap_NilReturnsNil(t *testing.T) {
	assert.Nil(t, errutil.Wrap(nil, "msg"))
}

// TestWrapf 测试 Wrapf 格式化包装
func TestWrapf(t *testing.T) {
	sentinel := errutil.New("plugin not found")
	wrapped := errutil.Wrapf(sentinel, "plugin %s load failed", "myplugin")
	assert.Equal(t, "plugin myplugin load failed: plugin not found", wrapped.Error())
	assert.True(t, errors.Is(wrapped, sentinel))
}

// TestWrapf_NilReturnsNil 测试 Wrapf(nil) 返回 nil
func TestWrapf_NilReturnsNil(t *testing.T) {
	assert.Nil(t, errutil.Wrapf(nil, "plugin %s failed", "foo"))
}

// TestWrapWithContext 测试 WrapWithContext
func TestWrapWithContext(t *testing.T) {
	sentinel := errutil.New("db error")
	wrapped := errutil.WrapWithContext(sentinel, "query failed", "table=users")

	assert.Contains(t, wrapped.Error(), "query failed")
	assert.Contains(t, wrapped.Error(), "table=users")
	assert.True(t, errors.Is(wrapped, sentinel))

	// 可以用 errors.As 提取 ErrorWrapper
	var ew *errutil.ErrorWrapper
	require.True(t, errors.As(wrapped, &ew))
	assert.Equal(t, "query failed", ew.Message)
	assert.Equal(t, "table=users", ew.Context)
}

// TestWrapWithContext_NilReturnsNil
func TestWrapWithContext_NilReturnsNil(t *testing.T) {
	assert.Nil(t, errutil.WrapWithContext(nil, "msg", "ctx"))
}

// TestIs 测试 Is 快捷方式
func TestIs(t *testing.T) {
	sentinel := errutil.New("sentinel")
	wrapped := fmt.Errorf("wrapping: %w", sentinel)
	assert.True(t, errutil.Is(wrapped, sentinel))
	assert.False(t, errutil.Is(wrapped, errutil.New("other")))
}

// TestAs 测试 As 泛型快捷方式
func TestAs(t *testing.T) {
	sentinel := errutil.New("db error")
	wrapped := errutil.WrapWithContext(sentinel, "query failed", "table=users")

	var ew *errutil.ErrorWrapper
	assert.True(t, errutil.As(wrapped, &ew))
	assert.Equal(t, "query failed", ew.Message)
}

// TestJoin 测试 Join
func TestJoin(t *testing.T) {
	err1 := errutil.New("err1")
	err2 := errutil.New("err2")
	joined := errutil.Join(err1, err2)
	assert.True(t, errutil.Is(joined, err1))
	assert.True(t, errutil.Is(joined, err2))
}

// TestJoin_WithNils
func TestJoin_WithNils(t *testing.T) {
	err1 := errutil.New("err1")
	joined := errutil.Join(nil, err1, nil)
	assert.NotNil(t, joined)
	assert.True(t, errutil.Is(joined, err1))
}

// TestUnwrap
func TestUnwrap(t *testing.T) {
	sentinel := errutil.New("base")
	wrapped := errutil.Wrap(sentinel, "outer")
	assert.Equal(t, sentinel, errutil.Unwrap(wrapped))
}

// TestWrapErrorChain_MultiLevel 测试多层包装仍可检查
func TestWrapErrorChain_MultiLevel(t *testing.T) {
	base := errutil.ErrPluginNotFound
	layer1 := errutil.Wrapf(base, "plugin %s not registered", "foo")
	layer2 := errutil.Wrap(layer1, "failed to start")

	assert.True(t, errutil.Is(layer2, base), "should find base error through multiple layers")
	assert.Equal(t, "failed to start: plugin foo not registered: plugin not found", layer2.Error())
}

// TestPredefinedErrors 测试预定义哨兵错误存在
func TestPredefinedErrors(t *testing.T) {
	assert.NotNil(t, errutil.ErrConfigInvalid)
	assert.NotNil(t, errutil.ErrPluginNotFound)
	assert.NotNil(t, errutil.ErrBotAlreadyRunning)
	assert.NotNil(t, errutil.ErrRateLimitExceeded)
	assert.NotNil(t, errutil.ErrCircuitBreakerOpen)
}

// TestErrorWrapper_Error 测试 ErrorWrapper 格式化
func TestErrorWrapper_Error(t *testing.T) {
	base := errutil.New("io error")

	// 没有 Context
	ew := &errutil.ErrorWrapper{Err: base, Message: "read failed"}
	assert.Equal(t, "read failed: io error", ew.Error())

	// 有 Context
	ewCtx := &errutil.ErrorWrapper{Err: base, Message: "read failed", Context: "file=/tmp/foo"}
	assert.Contains(t, ewCtx.Error(), "read failed")
	assert.Contains(t, ewCtx.Error(), "file=/tmp/foo")
	assert.Contains(t, ewCtx.Error(), "io error")
}
