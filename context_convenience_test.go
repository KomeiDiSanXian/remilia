package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestContext_GetString tests GetString convenience method
func TestContext_GetString(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Test with existing string
	ctx.SetState("key", "value")
	assert.Equal(t, "value", ctx.GetString("key"))

	// Test with non-existent key
	assert.Equal(t, "", ctx.GetString("nonexistent"))

	// Test with wrong type
	ctx.SetState("number", 123)
	assert.Equal(t, "", ctx.GetString("number"))
}

// TestContext_GetInt tests GetInt convenience method
func TestContext_GetInt(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Test with existing int
	ctx.SetState("count", 42)
	assert.Equal(t, 42, ctx.GetInt("count"))

	// Test with non-existent key
	assert.Equal(t, 0, ctx.GetInt("nonexistent"))

	// Test with wrong type
	ctx.SetState("text", "hello")
	assert.Equal(t, 0, ctx.GetInt("text"))
}

// TestContext_GetInt64 tests GetInt64 convenience method
func TestContext_GetInt64(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Test with existing int64
	var num int64 = 9223372036854775807
	ctx.SetState("big", num)
	assert.Equal(t, num, ctx.GetInt64("big"))

	// Test with non-existent key
	assert.Equal(t, int64(0), ctx.GetInt64("nonexistent"))
}

// TestContext_GetBool tests GetBool convenience method
func TestContext_GetBool(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Test with existing bool
	ctx.SetState("active", true)
	assert.True(t, ctx.GetBool("active"))

	ctx.SetState("inactive", false)
	assert.False(t, ctx.GetBool("inactive"))

	// Test with non-existent key
	assert.False(t, ctx.GetBool("nonexistent"))

	// Test with wrong type
	ctx.SetState("number", 1)
	assert.False(t, ctx.GetBool("number"))
}

// TestContext_GetFloat64 tests GetFloat64 convenience method
func TestContext_GetFloat64(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Test with existing float64
	ctx.SetState("price", 99.99)
	assert.Equal(t, 99.99, ctx.GetFloat64("price"))

	// Test with non-existent key
	assert.Equal(t, 0.0, ctx.GetFloat64("nonexistent"))

	// Test with wrong type
	ctx.SetState("text", "hello")
	assert.Equal(t, 0.0, ctx.GetFloat64("text"))
}

// TestContext_MustGetString tests MustGetString method
func TestContext_MustGetString(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Test with existing string
	ctx.SetState("key", "value")
	val, err := ctx.MustGetString("key")
	assert.NoError(t, err)
	assert.Equal(t, "value", val)

	// Test with non-existent key
	_, err = ctx.MustGetString("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Test with wrong type
	ctx.SetState("number", 123)
	_, err = ctx.MustGetString("number")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a string")
}

// TestContext_MustGetInt tests MustGetInt method
func TestContext_MustGetInt(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Test with existing int
	ctx.SetState("count", 42)
	val, err := ctx.MustGetInt("count")
	assert.NoError(t, err)
	assert.Equal(t, 42, val)

	// Test with non-existent key
	_, err = ctx.MustGetInt("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Test with wrong type
	ctx.SetState("text", "hello")
	_, err = ctx.MustGetInt("text")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not an int")
}

// TestContext_SetStateMap tests batch state setting
func TestContext_SetStateMap(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Set multiple states at once
	data := State{
		"key1": "value1",
		"key2": 123,
		"key3": true,
	}
	ctx.SetStateMap(data)

	// Verify all states were set
	assert.Equal(t, "value1", ctx.GetString("key1"))
	assert.Equal(t, 123, ctx.GetInt("key2"))
	assert.True(t, ctx.GetBool("key3"))
}

// TestContext_GetStateKeys tests getting multiple states
func TestContext_GetStateKeys(t *testing.T) {
	ctx := NewContext(&dto.Payload{}, nil)

	// Set up test data
	ctx.SetState("key1", "value1")
	ctx.SetState("key2", 123)
	ctx.SetState("key3", true)

	// Get multiple keys
	result := ctx.GetStateKeys("key1", "key2", "nonexistent")

	// Verify results
	assert.Equal(t, "value1", result["key1"])
	assert.Equal(t, 123, result["key2"])
	_, exists := result["nonexistent"]
	assert.False(t, exists)
}

// Benchmark tests
func BenchmarkContext_GetString(b *testing.B) {
	ctx := NewContext(&dto.Payload{}, nil)
	ctx.SetState("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.GetString("key")
	}
}

func BenchmarkContext_GetState_ManualCast(b *testing.B) {
	ctx := NewContext(&dto.Payload{}, nil)
	ctx.SetState("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if val, ok := ctx.GetState("key"); ok {
			_ = val.(string)
		}
	}
}

func BenchmarkContext_SetStateMap(b *testing.B) {
	ctx := NewContext(&dto.Payload{}, nil)
	data := State{
		"key1": "value1",
		"key2": 123,
		"key3": true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.SetStateMap(data)
	}
}

func BenchmarkContext_SetState_Multiple(b *testing.B) {
	ctx := NewContext(&dto.Payload{}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.SetState("key1", "value1")
		ctx.SetState("key2", 123)
		ctx.SetState("key3", true)
	}
}
