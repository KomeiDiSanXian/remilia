package benchmark

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	rcontext "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// BenchmarkEngineProcessEvent 基准测试：事件处理
func BenchmarkEngineProcessEvent(b *testing.B) {
	eng := engine.NewEngine()
	defer eng.Shutdown(context.Background())

	eng.OnCommand(dto.C2CMessageCreate, "/bench").Handle(func(ctx *rcontext.Context) error {
		return nil
	})

	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "/bench"}`),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx := rcontext.NewContext(event, nil)
		eng.ProcessEvent(ctx)
	}
}

// BenchmarkEngineProcessEventParallel 并行基准测试：事件处理
func BenchmarkEngineProcessEventParallel(b *testing.B) {
	eng := engine.NewEngine()
	defer eng.Shutdown(context.Background())

	eng.OnCommand(dto.C2CMessageCreate, "/bench").Handle(func(ctx *rcontext.Context) error {
		return nil
	})

	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "/bench"}`),
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := rcontext.NewContext(event, nil)
			eng.ProcessEvent(ctx)
		}
	})
}

// BenchmarkMatcherRegistration 基准测试：匹配器注册
func BenchmarkMatcherRegistration(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		eng := engine.NewEngine()
		eng.OnCommand(dto.C2CMessageCreate, "/test")
		eng.Shutdown(context.Background())
	}
}

// BenchmarkBatchMatcherRegistration 基准测试：批量匹配器注册
func BenchmarkBatchMatcherRegistration(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		eng := engine.NewEngine()

		matchers := make([]*engine.Matcher, 100)
		for j := range 100 {
			matchers[j] = &engine.Matcher{
				EventType: dto.C2CMessageCreate,
				Rules: []rcontext.Rule{
					func(ctx *rcontext.Context) bool { return true },
				},
			}
		}
		eng.BatchRegisterMatchers(matchers)
		eng.Shutdown(context.Background())
	}
}

// BenchmarkCommandParsing 基准测试：命令解析
func BenchmarkCommandParsing(b *testing.B) {
	parser := command.NewParser("/")

	def := &command.Definition{
		Name: "weather",
		Arguments: []*command.Argument{
			{Name: "city", Type: command.ArgTypeString},
		},
		Flags: []*command.Flag{
			{Name: "unit", Type: command.ArgTypeString},
		},
	}
	parser.Register(def)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = parser.Parse("/weather Beijing --unit C")
	}
}

// BenchmarkCommandParsingComplex 基准测试：复杂命令解析
func BenchmarkCommandParsingComplex(b *testing.B) {
	parser := command.NewParser("/")

	def := &command.Definition{
		Name: "search",
		SubCommands: []*command.Definition{
			{
				Name: "user",
				Arguments: []*command.Argument{
					{Name: "query", Type: command.ArgTypeString},
					{Name: "limit", Type: command.ArgTypeInt},
				},
				Flags: []*command.Flag{
					{Name: "sort", Type: command.ArgTypeString},
					{Name: "reverse", Type: command.ArgTypeBool},
				},
			},
		},
	}
	parser.Register(def)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = parser.Parse("/search user test 10 --sort name --reverse")
	}
}

// BenchmarkTrieOperations 基准测试：Trie 树操作
func BenchmarkTrieOperations(b *testing.B) {
	b.Run("Insert", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			trie := command.NewTrie()
			for j := range 100 {
				trie.Insert("/command"+string(rune(j)), nil)
			}
		}
	})

	b.Run("Search", func(b *testing.B) {
		trie := command.NewTrie()
		for j := range 100 {
			trie.Insert("/command"+string(rune(j)), nil)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = trie.Search("/command50")
		}
	})

	b.Run("PrefixSearch", func(b *testing.B) {
		trie := command.NewTrie()
		for j := range 100 {
			trie.Insert("/command"+string(rune(j)), nil)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// 使用前缀搜索
			_ = trie.Search("/comm")
		}
	})
}

// BenchmarkMiddlewareChain 基准测试：中间件链
func BenchmarkMiddlewareChain(b *testing.B) {
	tests := []struct {
		name  string
		count int
	}{
		{"1_middleware", 1},
		{"5_middlewares", 5},
		{"10_middlewares", 10},
		{"20_middlewares", 20},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			eng := engine.NewEngine()
			defer eng.Shutdown(context.Background())

			// 添加多个中间件
			for i := 0; i < tt.count; i++ {
				eng.Use(func(next rcontext.Handler) rcontext.Handler {
					return func(ctx *rcontext.Context) error {
						return next(ctx)
					}
				})
			}

			eng.OnCommand(dto.C2CMessageCreate, "/test").Handle(func(ctx *rcontext.Context) error {
				return nil
			})

			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content": "/test"}`),
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				ctx := rcontext.NewContext(event, nil)
				eng.ProcessEvent(ctx)
			}
		})
	}
}

// BenchmarkLoggerOperations 基准测试：日志操作
func BenchmarkLoggerOperations(b *testing.B) {
	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fields := make(logger.Fields, 8)
			fields["key1"] = "value1"
			fields["key2"] = 123
			fields["key3"] = true
		}
	})

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fields := logger.GetFields()
			fields["key1"] = "value1"
			fields["key2"] = 123
			fields["key3"] = true
			logger.PutFields(fields)
		}
	})
}

// BenchmarkContextOperations 基准测试：Context 操作
func BenchmarkContextOperations(b *testing.B) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		Detail: []byte(`{
			"content": "test message",
			"author": {"user_openid": "user123"}
		}`),
	}

	b.Run("Get", func(b *testing.B) {
		ctx := rcontext.NewContext(event, nil)
		ctx.Set("test_key", "test_value")

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = ctx.Get("test_key")
		}
	})

	b.Run("Set", func(b *testing.B) {
		ctx := rcontext.NewContext(event, nil)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx.Set("test_key", "test_value")
		}
	})

	b.Run("GetAuthor", func(b *testing.B) {
		ctx := rcontext.NewContext(event, nil)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = ctx.GetSenderInfo()
		}
	})
}

// BenchmarkTempMatcherOperations 基准测试：临时匹配器操作
func BenchmarkTempMatcherOperations(b *testing.B) {
	b.Run("AddAndRemove", func(b *testing.B) {
		eng := engine.NewEngine()
		defer eng.Shutdown(context.Background())

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			matcher := eng.OnTemp(dto.C2CMessageCreate, func(ctx *rcontext.Context) bool {
				return true
			})
			matcher.Delete()
		}
	})

	b.Run("ProcessWithTemp", func(b *testing.B) {
		eng := engine.NewEngine()
		defer eng.Shutdown(context.Background())

		// 添加临时匹配器
		eng.OnTemp(dto.C2CMessageCreate, func(ctx *rcontext.Context) bool {
			return true
		}).Handle(func(ctx *rcontext.Context) error {
			return nil
		})

		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`{"content": "test"}`),
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx := rcontext.NewContext(event, nil)
			eng.ProcessEvent(ctx)
		}
	})
}

// BenchmarkCOWOperations 基准测试：COW 操作
func BenchmarkCOWOperations(b *testing.B) {
	b.Run("SingleRegister", func(b *testing.B) {
		eng := engine.NewEngine()
		defer eng.Shutdown(context.Background())

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			eng.On(dto.C2CMessageCreate, func(ctx *rcontext.Context) bool {
				return true
			})
		}
	})

	b.Run("BatchRegister", func(b *testing.B) {
		eng := engine.NewEngine()
		defer eng.Shutdown(context.Background())

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			matchers := make([]*engine.Matcher, 10)
			for j := range 10 {
				matchers[j] = &engine.Matcher{
					EventType: dto.C2CMessageCreate,
					Rules: []rcontext.Rule{
						func(ctx *rcontext.Context) bool { return true },
					},
				}
			}
			eng.BatchRegisterMatchers(matchers)
		}
	})
}

// BenchmarkMemoryAllocation 基准测试：内存分配
func BenchmarkMemoryAllocation(b *testing.B) {
	eng := engine.NewEngine()
	defer eng.Shutdown(context.Background())

	eng.OnCommand(dto.C2CMessageCreate, "/alloc").Handle(func(ctx *rcontext.Context) error {
		// 模拟一些内存分配
		data := make([]byte, 1024)
		_ = data
		return nil
	})

	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: []byte(`{"content": "/alloc"}`),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx := rcontext.NewContext(event, nil)
		eng.ProcessEvent(ctx)
	}
}

// BenchmarkComparisonTable 生成性能对比表
func BenchmarkComparisonTable(b *testing.B) {
	// 这个基准测试用于生成不同场景的性能对比数据
	scenarios := []struct {
		name            string
		matcherCount    int
		middlewareCount int
	}{
		{"Small_10M_1MW", 10, 1},
		{"Medium_100M_5MW", 100, 5},
		{"Large_1000M_10MW", 1000, 10},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			eng := engine.NewEngine()
			defer eng.Shutdown(context.Background())

			// 添加中间件
			for i := 0; i < scenario.middlewareCount; i++ {
				eng.Use(func(next rcontext.Handler) rcontext.Handler {
					return func(ctx *rcontext.Context) error {
						return next(ctx)
					}
				})
			}

			// 添加匹配器
			for i := 0; i < scenario.matcherCount; i++ {
				eng.OnCommand(dto.C2CMessageCreate, "/cmd").Handle(func(ctx *rcontext.Context) error {
					return nil
				})
			}

			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content": "/cmd"}`),
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				ctx := rcontext.NewContext(event, nil)
				eng.ProcessEvent(ctx)
			}
		})
	}
}
