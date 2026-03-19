package engine_test

import (
	stdctx "context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestBatchRegisterMatchers 测试批量注册匹配器
func TestBatchRegisterMatchers(t *testing.T) {
	e := engine.NewEngine()
	defer e.Shutdown(stdctx.Background())

	// 创建多个匹配器
	matchers := make([]*engine.Matcher, 10)
	for i := range 10 {
		matchers[i] = &engine.Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules: []context.Rule{
				func(ctx *context.Context) bool { return true },
			},
		}
	}

	// 批量注册
	registered := e.BatchRegisterMatchers(matchers)

	// 验证
	assert.Equal(t, 10, len(registered))
	assert.Equal(t, 10, e.GetMatcherCount())

	// 验证每个 matcher 都正确注册
	for _, m := range registered {
		assert.NotNil(t, m)
		assert.False(t, m.IsDeleted())
	}
}

// TestBatchRegisterWithLimit 测试批量注册时的数量限制
func TestBatchRegisterWithLimit(t *testing.T) {
	e := engine.NewEngine()
	defer e.Shutdown(stdctx.Background())

	// 设置限制
	e.SetMaxMatchers(5)

	// 创建 10 个匹配器
	matchers := make([]*engine.Matcher, 10)
	for i := range 10 {
		matchers[i] = &engine.Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules: []context.Rule{
				func(ctx *context.Context) bool { return true },
			},
		}
	}

	// 批量注册
	registered := e.BatchRegisterMatchers(matchers)

	// 只应该注册 5 个
	assert.Equal(t, 5, len(registered))
	assert.Equal(t, 5, e.GetMatcherCount())
}

// TestBatchRegisterEmpty 测试批量注册空列表
func TestBatchRegisterEmpty(t *testing.T) {
	e := engine.NewEngine()
	defer e.Shutdown(stdctx.Background())

	matchers := []*engine.Matcher{}
	registered := e.BatchRegisterMatchers(matchers)

	assert.Equal(t, 0, len(registered))
	assert.Equal(t, 0, e.GetMatcherCount())
}

// BenchmarkSingleRegister 单个注册性能基准
func BenchmarkSingleRegister(b *testing.B) {
	e := engine.NewEngine()
	defer e.Shutdown(stdctx.Background())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.On(string(platform.EventKindPrivateMessage), func(ctx *context.Context) bool {
			return true
		})
	}
}

// BenchmarkBatchRegister 批量注册性能基准
func BenchmarkBatchRegister(b *testing.B) {
	e := engine.NewEngine()
	defer e.Shutdown(stdctx.Background())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matchers := make([]*engine.Matcher, 10)
		for j := range 10 {
			matchers[j] = &engine.Matcher{
				EventType: string(platform.EventKindPrivateMessage),
				Rules: []context.Rule{
					func(ctx *context.Context) bool { return true },
				},
			}
		}
		e.BatchRegisterMatchers(matchers)
		e.DeleteAllMatchers()
	}
}
