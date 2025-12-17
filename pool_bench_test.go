package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// BenchmarkContextWithoutPool 不使用对象池的基准测试
func BenchmarkContextWithoutPool(b *testing.B) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		ctx.SetState("key", "value")
		_ = ctx.GetMessageContent()
	}
}

// BenchmarkInstrumentedPool 测试带统计功能的对象池性能
func BenchmarkInstrumentedPool(b *testing.B) {
	pool := NewInstrumentedPool(func() interface{} {
		return &Context{
			state: make(State),
		}
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		obj := pool.Get()
		ctx := obj.(*Context)
		ctx.SetState("key", "value")
		pool.Put(obj)
	}
}
