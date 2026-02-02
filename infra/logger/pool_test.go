package logger_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/stretchr/testify/assert"
)

// TestFieldsPool 测试 Fields 对象池
func TestFieldsPool(t *testing.T) {
	// 获取 fields
	fields1 := logger.GetFields()
	assert.NotNil(t, fields1)
	assert.Equal(t, 0, len(fields1))

	// 使用 fields
	fields1["key1"] = "value1"
	fields1["key2"] = 123
	assert.Equal(t, 2, len(fields1))

	// 归还到池
	logger.PutFields(fields1)

	// 再次获取，应该是清空的
	fields2 := logger.GetFields()
	assert.Equal(t, 0, len(fields2))

	// 归还
	logger.PutFields(fields2)
}

// BenchmarkFieldsWithoutPool 不使用对象池的性能基准
func BenchmarkFieldsWithoutPool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fields := make(logger.Fields, 8)
		fields["key1"] = "value1"
		fields["key2"] = 123
		fields["key3"] = true
	}
}

// BenchmarkFieldsWithPool 使用对象池的性能基准
func BenchmarkFieldsWithPool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fields := logger.GetFields()
		fields["key1"] = "value1"
		fields["key2"] = 123
		fields["key3"] = true
		logger.PutFields(fields)
	}
}

// BenchmarkFieldsWithPoolParallel 并发使用对象池的性能基准
func BenchmarkFieldsWithPoolParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			fields := logger.GetFields()
			fields["key1"] = "value1"
			fields["key2"] = 123
			logger.PutFields(fields)
		}
	})
}
