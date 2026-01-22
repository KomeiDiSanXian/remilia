package command

import (
	"fmt"
	"testing"
)

// BenchmarkTokenize 测试 tokenize 函数的性能
func BenchmarkTokenize(b *testing.B) {
	tests := []struct {
		name  string
		input string
	}{
		{"simple", "hello world test"},
		{"quoted", `hello "world test" foo bar`},
		{"complex", `cmd "arg with spaces" 'another arg' normal\ arg --flag value`},
		{"long", "word1 word2 word3 word4 word5 word6 word7 word8 word9 word10"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = tokenize(tt.input)
			}
		})
	}
}

// BenchmarkParseCommandLine 测试 ParseCommandLine 函数的性能
func BenchmarkParseCommandLine(b *testing.B) {
	tests := []struct {
		name  string
		input string
	}{
		{"simple", "/weather Beijing"},
		{"with_flags", "/weather Beijing --unit celsius --days 3"},
		{"many_args", "/copy file1 file2 file3 file4 file5 --recursive --verbose"},
		{"complex", `/search "query string" --limit 20 --sort date --filter 'tag:golang' -v`},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = ParseCommandLine(tt.input)
			}
		})
	}
}

// BenchmarkParser_Parse 测试 Parser.Parse 的性能
func BenchmarkParser_Parse(b *testing.B) {
	parser := NewParser("/")

	// 注册一些命令
	parser.Register(&Definition{
		Name: "simple",
		Arguments: []*Argument{
			{Name: "arg1", Type: ArgTypeString},
		},
	})

	parser.Register(&Definition{
		Name: "complex",
		Arguments: []*Argument{
			{Name: "arg1", Type: ArgTypeString, Required: true},
			{Name: "arg2", Type: ArgTypeInt, Required: true},
		},
		Flags: []*Flag{
			{Name: "flag1", Type: ArgTypeString},
			{Name: "flag2", Type: ArgTypeBool},
			{Name: "flag3", Type: ArgTypeInt},
		},
	})

	parser.Register(&Definition{
		Name: "nested",
		SubCommands: []*Definition{
			{
				Name: "sub1",
				SubCommands: []*Definition{
					{
						Name: "sub2",
						Arguments: []*Argument{
							{Name: "value", Type: ArgTypeString},
						},
					},
				},
			},
		},
	})

	tests := []struct {
		name  string
		input string
	}{
		{"simple", "/simple arg1"},
		{"complex", "/complex arg1 42 --flag1 value --flag2 --flag3 123"},
		{"nested", "/nested sub1 sub2 value"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = parser.Parse(tt.input)
			}
		})
	}
}

// BenchmarkParseFromDefinition 测试 ParseFromDefinition 的性能
func BenchmarkParseFromDefinition(b *testing.B) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "arg1", Type: ArgTypeString, Required: true},
			{Name: "arg2", Type: ArgTypeInt, Required: true},
		},
		Flags: []*Flag{
			{Name: "flag1", ShortName: "f", Type: ArgTypeString},
			{Name: "flag2", Type: ArgTypeBool},
		},
	}

	tests := []struct {
		name  string
		input string
	}{
		{"minimal", "/cmd arg1 123"},
		{"with_flags", "/cmd arg1 123 --flag1 value --flag2"},
		{"short_flags", "/cmd arg1 123 -f value"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = ParseFromDefinition(tt.input, def, "/")
			}
		})
	}
}

// BenchmarkParseValue 测试 parseValue 的性能
func BenchmarkParseValue(b *testing.B) {
	tests := []struct {
		name  string
		value string
		typ   ArgType
	}{
		{"string", "hello world", ArgTypeString},
		{"int", "12345", ArgTypeInt},
		{"bool_true", "true", ArgTypeBool},
		{"bool_false", "false", ArgTypeBool},
		{"float", "3.14159", ArgTypeFloat},
		{"string_slice", "one two three four", ArgTypeStringSlice},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = parseValue(tt.value, tt.typ)
			}
		})
	}
}

// BenchmarkArgs_GetMethods 测试 Args 的各种 Get 方法
func BenchmarkArgs_GetMethods(b *testing.B) {
	args, _ := ParseCommandLine("/cmd arg1 arg2 123 --flag1 value1 --flag2 456 --bool true")

	b.Run("Get", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = args.Get(0)
		}
	})

	b.Run("GetFlag", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = args.GetFlag("flag1")
		}
	})

	b.Run("GetInt", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = args.GetInt(2)
		}
	})

	b.Run("GetFlagInt", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = args.GetFlagInt("flag2")
		}
	})

	b.Run("GetFlagBool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = args.GetFlagBool("bool")
		}
	})

	b.Run("HasFlag", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = args.HasFlag("flag1")
		}
	})
}

// BenchmarkParsed_GetMethods 测试 Parsed 的各种 Get 方法
func BenchmarkParsed_GetMethods(b *testing.B) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "str", Type: ArgTypeString},
			{Name: "num", Type: ArgTypeInt},
		},
		Flags: []*Flag{
			{Name: "bool", Type: ArgTypeBool},
			{Name: "float", Type: ArgTypeFloat},
		},
	}

	parsed, _ := ParseFromDefinition("/cmd hello 42 --bool true --float 3.14", def, "/")

	b.Run("GetString", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = parsed.GetString("str")
		}
	})

	b.Run("GetInt", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = parsed.GetInt("num")
		}
	})

	b.Run("GetBool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = parsed.GetBool("bool")
		}
	})

	b.Run("GetFloat", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = parsed.GetFloat("float")
		}
	})
}

// BenchmarkComplexCommandTree 测试复杂命令树的性能
func BenchmarkComplexCommandTree(b *testing.B) {
	parser := NewParser("/")

	// 创建一个复杂的命令树
	def := &Definition{
		Name: "app",
		SubCommands: []*Definition{
			{
				Name: "module1",
				SubCommands: []*Definition{
					{
						Name: "action1",
						Arguments: []*Argument{
							{Name: "arg1", Type: ArgTypeString},
							{Name: "arg2", Type: ArgTypeInt},
						},
						Flags: []*Flag{
							{Name: "verbose", Type: ArgTypeBool},
							{Name: "output", Type: ArgTypeString},
						},
					},
					{Name: "action2"},
				},
			},
			{
				Name: "module2",
				SubCommands: []*Definition{
					{Name: "action3"},
					{Name: "action4"},
				},
			},
		},
	}

	parser.Register(def)

	tests := []struct {
		name  string
		input string
	}{
		{"level1", "/app module1"},
		{"level2", "/app module1 action1 value 123"},
		{"level2_with_flags", "/app module1 action1 value 123 --verbose --output file.txt"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = parser.Parse(tt.input)
			}
		})
	}
}

// BenchmarkValidation 测试验证器的性能影响
func BenchmarkValidation(b *testing.B) {
	// 无验证器
	defNoValidator := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "arg1", Type: ArgTypeString, Required: true},
		},
	}

	// 带参数验证器
	defWithArgValidator := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{
				Name:     "arg1",
				Type:     ArgTypeString,
				Required: true,
				Validator: func(s string) error {
					if len(s) < 3 {
						return fmt.Errorf("too short")
					}
					return nil
				},
			},
		},
	}

	// 带定义验证器
	defWithDefValidator := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "arg1", Type: ArgTypeString, Required: true},
		},
		Validator: func(p *Parsed) error {
			if len(p.GetString("arg1")) < 3 {
				return fmt.Errorf("too short")
			}
			return nil
		},
	}

	input := "/cmd hello"

	b.Run("no_validator", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = ParseFromDefinition(input, defNoValidator, "/")
		}
	})

	b.Run("arg_validator", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = ParseFromDefinition(input, defWithArgValidator, "/")
		}
	})

	b.Run("def_validator", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = ParseFromDefinition(input, defWithDefValidator, "/")
		}
	})
}

// BenchmarkStringSlice 测试 StringSlice 类型的性能
func BenchmarkStringSlice(b *testing.B) {
	tests := []struct {
		name  string
		def   *Definition
		input string
	}{
		{
			name: "positional_small",
			def: &Definition{
				Name: "cmd",
				Arguments: []*Argument{
					{Name: "files", Type: ArgTypeStringSlice},
				},
			},
			input: "/cmd file1 file2 file3",
		},
		{
			name: "positional_large",
			def: &Definition{
				Name: "cmd",
				Arguments: []*Argument{
					{Name: "files", Type: ArgTypeStringSlice},
				},
			},
			input: "/cmd f1 f2 f3 f4 f5 f6 f7 f8 f9 f10",
		},
		{
			name: "flag",
			def: &Definition{
				Name: "cmd",
				Flags: []*Flag{
					{Name: "tags", Type: ArgTypeStringSlice},
				},
			},
			input: "/cmd --tags 'tag1 tag2 tag3 tag4 tag5'",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = ParseFromDefinition(tt.input, tt.def, "/")
			}
		})
	}
}
