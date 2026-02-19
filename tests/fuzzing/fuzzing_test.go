package fuzzing

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	rcontext "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// FuzzEventPayload 模糊测试事件负载
func FuzzEventPayload(f *testing.F) {
	// 种子语料库
	seeds := [][]byte{
		[]byte(`{"content": "test"}`),
		[]byte(`{"content": "/command"}`),
		[]byte(`{"content": "/cmd arg1 arg2"}`),
		[]byte(`{"author": {"user_openid": "user123"}}`),
		[]byte(`{"content": "test", "author": {"user_openid": "user123"}}`),
		[]byte(`{}`),
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`"string"`),
		[]byte(`123`),
		[]byte(`true`),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// 不应该 panic
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: data,
		}

		ctx := rcontext.NewContext(event, nil)

		// 尝试各种操作
		_ = ctx.GetEventType()
		_ = ctx.GetAuthor()
		_ = ctx.GetEvent()
	})
}

// FuzzCommandParsing 模糊测试命令解析
func FuzzCommandParsing(f *testing.F) {
	parser := command.NewParser("/")

	// 注册一些命令
	def := &command.Definition{
		Name: "test",
		Arguments: []*command.Argument{
			{Name: "arg1", Type: command.ArgTypeString},
			{Name: "arg2", Type: command.ArgTypeInt},
		},
		Flags: []*command.Flag{
			{Name: "flag1", Type: command.ArgTypeString},
			{Name: "bool-flag", Type: command.ArgTypeBool},
		},
	}
	parser.Register(def)

	// 种子
	seeds := []string{
		"/test value1 123",
		"/test value1 123 --flag1 test",
		"/test value1 123 --bool-flag",
		"/test",
		"/unknown",
		"",
		"/test arg1",
		"/test arg1 arg2 arg3 arg4",
		"/test --flag1",
		"/test --unknown-flag value",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// 不应该 panic
		_, _ = parser.Parse(input)
	})
}

// FuzzEngineProcessEvent 模糊测试 engine 事件处理
func FuzzEngineProcessEvent(f *testing.F) {
	// 种子
	seeds := []string{
		"/test",
		"/command arg1 arg2",
		"plain text",
		"",
		strings.Repeat("a", 10000),            // 长字符串
		"/cmd " + strings.Repeat("arg ", 100), // 大量参数
		string(make([]byte, 1000)),            // 随机字节
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, content string) {
		eng := engine.NewEngine()
		defer eng.Close()

		// 注册一个通配命令
		eng.OnCommand(dto.C2CMessageCreate, "/test").Handle(func(ctx *rcontext.Context) error {
			return nil
		})

		// 构造事件
		detail := map[string]any{
			"content": content,
		}
		detailBytes, err := json.Marshal(detail)
		if err != nil {
			return
		}

		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailBytes,
		}

		ctx := rcontext.NewContext(event, nil)

		// 不应该 panic
		eng.ProcessEvent(ctx)
	})
}

// FuzzContextOperations 模糊测试 Context 操作
func FuzzContextOperations(f *testing.F) {
	seeds := []struct {
		key   string
		value string
	}{
		{"test", "value"},
		{"", ""},
		{"key", ""},
		{"", "value"},
		{strings.Repeat("k", 100), strings.Repeat("v", 1000)},
	}

	for _, seed := range seeds {
		f.Add(seed.key, seed.value)
	}

	f.Fuzz(func(t *testing.T, key, value string) {
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`{}`),
		}
		ctx := rcontext.NewContext(event, nil)

		// Set/Get 不应该 panic
		ctx.Set(key, value)
		_, _ = ctx.Get(key)
	})
}

// FuzzTrieOperations 模糊测试 Trie 树操作
func FuzzTrieOperations(f *testing.F) {
	seeds := []string{
		"/command",
		"/cmd",
		"",
		"/",
		"//",
		"/a/b/c",
		strings.Repeat("/", 100),
		strings.Repeat("a", 1000),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		trie := command.NewTrie()

		// Insert 不应该 panic
		trie.Insert(input, nil)

		// Search 不应该 panic
		_ = trie.Search(input)
	})
}

// FuzzJSONDecoding 模糊测试 JSON 解码
func FuzzJSONDecoding(f *testing.F) {
	// 种子
	seeds := [][]byte{
		[]byte(`{"key": "value"}`),
		[]byte(`{"nested": {"key": "value"}}`),
		[]byte(`{"array": [1, 2, 3]}`),
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`{}`),
		[]byte(`"string"`),
		[]byte(`123`),
		[]byte(`true`),
		[]byte(`false`),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var result map[string]any
		// JSON 解码不应该 panic
		_ = json.Unmarshal(data, &result)
	})
}

// FuzzMatcherRules 模糊测试匹配规则
func FuzzMatcherRules(f *testing.F) {
	seeds := []string{
		"test message",
		"/command",
		"",
		strings.Repeat("a", 10000),
		"unicode: 你好世界 🌍",
		"special chars: !@#$%^&*()",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, content string) {
		detail := map[string]any{
			"content": content,
		}
		detailBytes, _ := json.Marshal(detail)

		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailBytes,
		}
		ctx := rcontext.NewContext(event, nil)

		// 测试各种规则
		rules := []rcontext.Rule{
			rcontext.OnFullMatch("test"),
			rcontext.OnPrefix("/"),
			rcontext.OnSuffix("!"),
			rcontext.OnRegex(`^\w+$`),
		}

		for _, rule := range rules {
			// 不应该 panic
			_ = rule(ctx)
		}
	})
}

// FuzzArgumentParsing 模糊测试参数解析
func FuzzArgumentParsing(f *testing.F) {
	def := &command.Definition{
		Name: "test",
		Arguments: []*command.Argument{
			{Name: "str", Type: command.ArgTypeString, Required: true},
			{Name: "num", Type: command.ArgTypeInt, Required: false},
			{Name: "float", Type: command.ArgTypeFloat, Required: false},
			{Name: "bool", Type: command.ArgTypeBool, Required: false},
		},
	}

	seeds := []string{
		"value1 123 3.14 true",
		"value1",
		"value1 abc",
		"value1 123 xyz",
		"",
		"arg1 arg2 arg3 arg4 arg5",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		fullInput := "/test " + input
		// 不应该 panic
		_, _ = command.ParseFromDefinition(fullInput, def, "/")
	})
}

// FuzzMiddlewareChain 模糊测试中间件链
func FuzzMiddlewareChain(f *testing.F) {
	f.Add(int32(0))
	f.Add(int32(1))
	f.Add(int32(5))
	f.Add(int32(10))
	f.Add(int32(100))

	f.Fuzz(func(t *testing.T, middlewareCount int32) {
		if middlewareCount < 0 || middlewareCount > 1000 {
			return
		}

		eng := engine.NewEngine()
		defer eng.Close()

		// 添加中间件
		for range middlewareCount {
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
		ctx := rcontext.NewContext(event, nil)

		// 不应该 panic
		eng.ProcessEvent(ctx)
	})
}

// FuzzSpecialCharacters 模糊测试特殊字符处理
func FuzzSpecialCharacters(f *testing.F) {
	// 种子包含各种特殊字符
	seeds := []string{
		"\x00", // NULL
		"\n\r\t",
		"\"'`",
		"<>",
		"[]{}()",
		"!@#$%^&*",
		"\\\\\\",
		"你好世界",
		"🌍🌎🌏",
		"\u0000\u001f",
		string([]byte{0xff, 0xfe, 0xfd}),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// 测试命令解析
		_, _ = command.ParseCommandLine(input)

		// 测试 Context 操作
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			Detail: func() []byte {
				buf := &bytes.Buffer{}
				buf.WriteString(`{"content": "`)
				buf.WriteString(input)
				buf.WriteString(`"}`)
				return buf.Bytes()
			}(),
		}
		ctx := rcontext.NewContext(event, nil)
		ctx.Set("test", input)
		_, _ = ctx.Get("test")
	})
}

// FuzzConcurrentOperations 模糊测试并发操作
func FuzzConcurrentOperations(f *testing.F) {
	f.Add(int32(1))
	f.Add(int32(10))
	f.Add(int32(100))

	f.Fuzz(func(t *testing.T, opCount int32) {
		if opCount < 1 || opCount > 1000 {
			return
		}

		eng := engine.NewEngine()
		defer eng.Close()

		eng.OnCommand(dto.C2CMessageCreate, "/test").Handle(func(ctx *rcontext.Context) error {
			return nil
		})

		// 并发操作
		done := make(chan struct{}, opCount)
		for range opCount {
			go func() {
				event := &dto.Payload{
					Type:   dto.C2CMessageCreate,
					Detail: []byte(`{"content": "/test"}`),
				}
				ctx := rcontext.NewContext(event, nil)
				eng.ProcessEvent(ctx)
				done <- struct{}{}
			}()
		}

		// 等待所有完成
		for range opCount {
			<-done
		}
	})
}

// FuzzMemoryBounds 模糊测试内存边界
func FuzzMemoryBounds(f *testing.F) {
	f.Add(int32(0))
	f.Add(int32(1))
	f.Add(int32(1024))
	f.Add(int32(1024 * 1024))

	f.Fuzz(func(t *testing.T, size int32) {
		if size < 0 || size > 10*1024*1024 { // 限制最大 10MB
			return
		}

		// 创建大内容
		content := strings.Repeat("a", int(size))

		detail := map[string]any{
			"content": content,
		}
		detailBytes, err := json.Marshal(detail)
		if err != nil {
			return
		}

		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: detailBytes,
		}

		ctx := rcontext.NewContext(event, nil)
		// 不应该 panic 或 OOM
		_ = ctx.GetEvent()
	})
}
