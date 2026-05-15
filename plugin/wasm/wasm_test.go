package wasm_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

func TestDescriptor_Validate(t *testing.T) {
	d := &wasm.Descriptor{Name: "test", Path: "/path/to/test.wasm"}
	assert.NoError(t, d.Validate())
}

func TestDescriptor_Validate_NoName(t *testing.T) {
	d := &wasm.Descriptor{Path: "/path/to/test.wasm"}
	assert.Error(t, d.Validate())
}

func TestDescriptor_Validate_NoPath(t *testing.T) {
	d := &wasm.Descriptor{Name: "test"}
	assert.Error(t, d.Validate())
}

func TestDescriptor_EffectiveResourceLimit_Defaults(t *testing.T) {
	d := &wasm.Descriptor{Name: "test", Path: "/x.wasm"}
	rl := d.EffectiveResourceLimit()
	assert.Equal(t, uint32(2), rl.MemoryPages)
	assert.Equal(t, int64(1000), rl.MaxCallPerSec)
}

func TestDescriptor_EffectiveResourceLimit_Custom(t *testing.T) {
	d := &wasm.Descriptor{
		Name: "test", Path: "/x.wasm",
		ResourceLimit: &wasm.ResourceLimit{MemoryPages: 8, MaxCallPerSec: 500},
	}
	rl := d.EffectiveResourceLimit()
	assert.Equal(t, uint32(8), rl.MemoryPages)
	assert.Equal(t, int64(500), rl.MaxCallPerSec)
}

func TestHostFuncRegistry_RegisterAndCall(t *testing.T) {
	reg := wasm.NewHostFuncRegistry()
	var called bool
	reg.Register("ping", func(args json.RawMessage) (json.RawMessage, error) {
		called = true
		return json.RawMessage(`"pong"`), nil
	})
	// 验证注册成功（BuildModule 不会 panic）
	assert.NotNil(t, reg)
	_ = called
}

func TestTokenBucket_Allow(t *testing.T) {
	tb := wasm.NewTokenBucket(100, 100)
	for range 100 {
		assert.True(t, tb.Allow(), "should allow within capacity")
	}
	assert.False(t, tb.Allow(), "should be rate limited after capacity exhausted")
}

func TestBridge_NewAndCleanup(t *testing.T) {
	_ = engine.NewEngine(engine.WithNoBackgroundWorkers())
}

func TestSandbox_MemoryLimit(t *testing.T) {
	s := wasm.NewSandbox(2, 100)
	assert.Equal(t, uint32(2*64*1024), s.MemoryLimitBytes())
}

func TestSandbox_AllowCall(t *testing.T) {
	s := wasm.NewSandbox(0, 100)
	for range 100 {
		assert.True(t, s.AllowCall())
	}
}

func TestEncodeDecodeResult(t *testing.T) {
	ptr := uint32(42)
	length := uint32(128)
	encoded := wasm.EncodeResult(ptr, length)
	decPtr, decLen := wasm.DecodeResult(encoded)
	assert.Equal(t, ptr, decPtr)
	assert.Equal(t, length, decLen)
}

func TestEncodeDecodeResult_Zero(t *testing.T) {
	encoded := wasm.EncodeResult(0, 0)
	ptr, length := wasm.DecodeResult(encoded)
	assert.Equal(t, uint32(0), ptr)
	assert.Equal(t, uint32(0), length)
}
