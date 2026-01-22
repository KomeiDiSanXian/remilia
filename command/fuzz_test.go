package command

import (
	"strings"
	"testing"
)

// FuzzTokenize 对 tokenize 函数进行模糊测试
func FuzzTokenize(f *testing.F) {
	// 提供种子语料库
	seeds := []string{
		"hello world",
		`"quoted string"`,
		`'single quoted'`,
		`escaped\\ space`,
		`hello "world test" foo`,
		`cmd --flag value`,
		`/command arg1 arg2 --flag1 val1 --flag2`,
		`"nested 'quote' test"`,
		`'nested "quote" test'`,
		`backslash\\ test`,
		"multiple    spaces",
		"tabs\tand\nnewlines",
		"",
		"   ",
		"a",
		"--flag",
		"-f",
		`"unclosed quote`,
		`trailing backslash\`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// tokenize 不应该 panic
		tokens, err := tokenize(input)

		if err != nil {
			// 如果有错误，应该是以下几种情况之一
			errMsg := err.Error()
			if !strings.Contains(errMsg, "unclosed quote") &&
				!strings.Contains(errMsg, "unexpected end of string after escape character") {
				t.Errorf("unexpected error type: %v", err)
			}
			return
		}

		// 验证返回的 tokens 不为 nil
		if tokens == nil {
			t.Error("tokens should not be nil when no error")
		}

		// 验证每个 token 不应该为空字符串（除非输入有问题）
		for _, token := range tokens {
			if token == "" {
				// 空 token 可能表明解析有问题
				t.Logf("warning: empty token found in result: %v", tokens)
			}
		}
	})
}

// FuzzParseCommandLine 对 ParseCommandLine 函数进行模糊测试
func FuzzParseCommandLine(f *testing.F) {
	// 提供种子语料库
	seeds := []string{
		"/command",
		"/cmd arg1",
		"/cmd arg1 arg2",
		"/cmd --flag value",
		"/cmd arg --flag1 val1 --flag2 val2",
		"/cmd -f value",
		"/cmd --bool-flag",
		"/weather Beijing --unit celsius",
		"/search golang -l 20 -v",
		"command without slash",
		"",
		"   ",
		"/cmd 'quoted arg' --flag 'quoted value'",
		`/cmd "double quoted" --flag "value"`,
		"/cmd arg1 arg2 arg3 arg4 arg5",
		"/cmd --flag1 --flag2 --flag3",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// ParseCommandLine 不应该 panic
		args, err := ParseCommandLine(input)

		if err != nil {
			// 有错误是可以接受的，只要不 panic
			return
		}

		// 验证基本不变性
		if args == nil {
			t.Error("args should not be nil when no error")
			return
		}

		// 验证 Raw 字段正确设置
		if args.Raw != input {
			t.Errorf("Raw field mismatch: got %q, want %q", args.Raw, input)
		}

		// 验证 Flags 和 Positional 不为 nil
		if args.Flags == nil {
			t.Error("Flags map should not be nil")
		}
		if args.Positional == nil {
			t.Error("Positional slice should not be nil")
		}

		// 验证 Command 不为空（如果成功解析）
		if args.Command == "" {
			t.Error("Command should not be empty when parsing succeeds")
		}

		// 验证 flag 值不应该包含 "--" 前缀
		for key, val := range args.Flags {
			if strings.HasPrefix(key, "--") {
				t.Errorf("flag key should not have -- prefix: %q", key)
			}
			if strings.HasPrefix(key, "-") && len(key) > 1 {
				t.Errorf("flag key should not have - prefix for long flags: %q", key)
			}
			if val == "" {
				// 空值应该至少是 "true" (boolean flags)
				// 但这在某些边缘情况下可能是合法的
				t.Logf("warning: empty flag value for key %q", key)
			}
		}
	})
}

// FuzzParseValue 对内部 parseValue 函数进行模糊测试
func FuzzParseValue(f *testing.F) {
	// 提供各种类型的种子值
	seeds := []struct {
		value string
		typ   ArgType
	}{
		{"hello", ArgTypeString},
		{"42", ArgTypeInt},
		{"-123", ArgTypeInt},
		{"true", ArgTypeBool},
		{"false", ArgTypeBool},
		{"1", ArgTypeBool},
		{"0", ArgTypeBool},
		{"yes", ArgTypeBool},
		{"no", ArgTypeBool},
		{"3.14", ArgTypeFloat},
		{"-2.5", ArgTypeFloat},
		{"one two three", ArgTypeStringSlice},
	}

	for _, seed := range seeds {
		f.Add(seed.value, int(seed.typ))
	}

	f.Fuzz(func(t *testing.T, value string, typ int) {
		// 限制 typ 在有效范围内
		if typ < 0 || typ > int(ArgTypeStringSlice) {
			t.Skip()
		}

		argType := ArgType(typ)

		// parseValue 不应该 panic
		result, err := parseValue(value, argType)

		// 根据类型验证结果
		switch argType {
		case ArgTypeString:
			if err != nil {
				t.Error("ArgTypeString should never return error")
			}
			if result != value {
				t.Errorf("ArgTypeString: expected %q, got %q", value, result)
			}

		case ArgTypeInt:
			if err == nil {
				if _, ok := result.(int); !ok {
					t.Errorf("ArgTypeInt should return int type, got %T", result)
				}
			}

		case ArgTypeBool:
			if err == nil {
				if _, ok := result.(bool); !ok {
					t.Errorf("ArgTypeBool should return bool type, got %T", result)
				}
			}

		case ArgTypeFloat:
			if err == nil {
				if _, ok := result.(float64); !ok {
					t.Errorf("ArgTypeFloat should return float64 type, got %T", result)
				}
			}

		case ArgTypeStringSlice:
			if err != nil {
				t.Error("ArgTypeStringSlice should never return error")
			}
			if _, ok := result.([]string); !ok {
				t.Errorf("ArgTypeStringSlice should return []string type, got %T", result)
			}
		}
	})
}

// FuzzParser 对完整的 Parser 进行模糊测试
func FuzzParser(f *testing.F) {
	// 设置一个测试解析器
	parser := NewParser("/")

	// 注册一些命令
	parser.Register(&Definition{
		Name:    "test",
		Aliases: []string{"t"},
		Arguments: []*Argument{
			{Name: "arg1", Type: ArgTypeString},
		},
		Flags: []*Flag{
			{Name: "flag", ShortName: "f", Type: ArgTypeString},
		},
	})

	parser.Register(&Definition{
		Name: "complex",
		SubCommands: []*Definition{
			{
				Name: "sub",
				Arguments: []*Argument{
					{Name: "value", Type: ArgTypeInt},
				},
			},
		},
	})

	// 种子语料库
	seeds := []string{
		"/test arg1",
		"/test arg1 --flag value",
		"/t arg1 -f value",
		"/complex sub 123",
		"/unknown command",
		"/test",
		"",
		"   ",
		"/test 'quoted arg'",
		`/test "double quoted"`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Parse 不应该 panic
		parsed, err := parser.Parse(input)

		if err != nil {
			// 错误是可以接受的
			return
		}

		// 验证基本不变性
		if parsed == nil {
			t.Error("parsed should not be nil when no error")
			return
		}

		if parsed.Raw != input {
			t.Errorf("Raw field mismatch")
		}

		if parsed.Definition == nil {
			t.Error("Definition should not be nil")
		}

		if parsed.Arguments == nil {
			t.Error("Arguments map should not be nil")
		}

		if parsed.Flags == nil {
			t.Error("Flags map should not be nil")
		}

		if len(parsed.CommandPath) == 0 {
			t.Error("CommandPath should not be empty")
		}
	})
}

// FuzzParseFromDefinition 对 ParseFromDefinition 进行模糊测试
func FuzzParseFromDefinition(f *testing.F) {
	// 创建一个测试定义
	def := &Definition{
		Name:    "cmd",
		Aliases: []string{"c"},
		Arguments: []*Argument{
			{Name: "arg1", Type: ArgTypeString},
			{Name: "arg2", Type: ArgTypeInt},
		},
		Flags: []*Flag{
			{Name: "flag1", ShortName: "f", Type: ArgTypeString},
			{Name: "flag2", Type: ArgTypeBool},
		},
	}

	// 种子语料库
	seeds := []string{
		"/cmd arg1 123",
		"/cmd arg1 123 --flag1 value --flag2",
		"/c arg1 456 -f test",
		"/cmd",
		"/wrong command",
		"",
		"/cmd arg1",
		"/cmd arg1 not-a-number",
		"/cmd arg1 123 --unknown flag",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// ParseFromDefinition 不应该 panic
		parsed, err := ParseFromDefinition(input, def, "/")

		if err != nil {
			// 错误是可以接受的
			return
		}

		// 验证基本不变性
		if parsed == nil {
			t.Error("parsed should not be nil when no error")
			return
		}

		if parsed.Raw != input {
			t.Errorf("Raw field mismatch")
		}

		if parsed.Definition == nil {
			t.Error("Definition should not be nil")
		}

		// 验证返回的定义与输入定义一致
		if parsed.Definition != def {
			t.Error("Definition should match input definition")
		}
	})
}

// FuzzArgsGetMethods 对 Args 的各种 Get 方法进行模糊测试
func FuzzArgsGetMethods(f *testing.F) {
	seeds := []string{
		"/cmd arg1 arg2 --flag1 value1 --flag2 123",
		"/cmd 42 true --num 456 --bool yes",
		"/cmd",
		"",
	}

	for _, seed := range seeds {
		f.Add(seed, 0)
		f.Add(seed, 1)
		f.Add(seed, -1)
	}

	f.Fuzz(func(t *testing.T, input string, index int) {
		args, err := ParseCommandLine(input)
		if err != nil {
			return
		}

		// 这些方法都不应该 panic，无论输入什么
		_ = args.Get(index)
		_ = args.GetIntOrDefault(index, 0)

		// 测试 flag 方法
		_ = args.GetFlag("any-key")
		_ = args.GetFlagOrDefault("any-key", "default")
		_ = args.HasFlag("any-key")
		_ = args.GetFlagBool("any-key")
		_ = args.GetFlagIntOrDefault("any-key", 0)

		// 这些可能返回错误，但不应该 panic
		_, _ = args.GetInt(index)
		_, _ = args.GetBool(index)
		_, _ = args.GetFlagInt("any-key")

		// 验证 Len 方法
		length := args.Len()
		if length < 0 {
			t.Error("Len() should not return negative value")
		}

		// 验证 String 方法
		str := args.String()
		if str != input {
			t.Errorf("String() should return original input")
		}
	})
}
