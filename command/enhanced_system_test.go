package command

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParser_Register 测试命令注册
func TestParser_Register(t *testing.T) {
	parser := NewParser("/")

	def1 := &Definition{Name: "test1"}
	def2 := &Definition{Name: "test2"}

	parser.Register(def1)
	parser.Register(def2)

	assert.Len(t, parser.rootCommands, 2)
}

// TestParser_Parse_SimpleCommand 测试简单命令解析
func TestParser_Parse_SimpleCommand(t *testing.T) {
	parser := NewParser("/")

	def := &Definition{
		Name:        "weather",
		Description: "Get weather info",
		Arguments: []*Argument{
			{Name: "city", Type: ArgTypeString, Required: true},
		},
		Flags: []*Flag{
			{Name: "unit", Type: ArgTypeString, Default: "celsius"},
		},
	}

	parser.Register(def)

	parsed, err := parser.Parse("/weather Beijing --unit fahrenheit")
	require.NoError(t, err)
	assert.Equal(t, "Beijing", parsed.GetString("city"))
	assert.Equal(t, "fahrenheit", parsed.GetString("unit"))
	assert.Equal(t, []string{"weather"}, parsed.CommandPath)
}

// TestParser_Parse_WithSubCommands 测试子命令解析
func TestParser_Parse_WithSubCommands(t *testing.T) {
	parser := NewParser("/")

	def := &Definition{
		Name: "git",
		SubCommands: []*Definition{
			{
				Name: "commit",
				Flags: []*Flag{
					{Name: "message", ShortName: "m", Type: ArgTypeString, Required: true},
				},
			},
			{
				Name: "push",
				Flags: []*Flag{
					{Name: "force", ShortName: "f", Type: ArgTypeBool},
				},
			},
		},
	}

	parser.Register(def)

	// Test commit subcommand
	parsed, err := parser.Parse("/git commit -m 'initial commit'")
	require.NoError(t, err)
	assert.Equal(t, []string{"git", "commit"}, parsed.CommandPath)
	assert.Equal(t, "initial commit", parsed.GetString("message"))

	// Test push subcommand
	parsed, err = parser.Parse("/git push --force")
	require.NoError(t, err)
	assert.Equal(t, []string{"git", "push"}, parsed.CommandPath)
	assert.True(t, parsed.GetBool("force"))
}

// TestParser_Parse_UnknownCommand 测试未知命令
func TestParser_Parse_UnknownCommand(t *testing.T) {
	parser := NewParser("/")

	def := &Definition{Name: "known"}
	parser.Register(def)

	_, err := parser.Parse("/unknown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

// TestParser_Parse_EmptyInput 测试空输入
func TestParser_Parse_EmptyInput(t *testing.T) {
	parser := NewParser("/")

	_, err := parser.Parse("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty input")
}

// TestParseFromDefinition 测试从定义解析
func TestParseFromDefinition(t *testing.T) {
	def := &Definition{
		Name:    "search",
		Aliases: []string{"find", "query"},
		Arguments: []*Argument{
			{Name: "keyword", Type: ArgTypeString, Required: true},
		},
		Flags: []*Flag{
			{Name: "limit", ShortName: "l", Type: ArgTypeInt, Default: 10},
		},
	}

	// Test with primary name
	parsed, err := ParseFromDefinition("/search golang --limit 20", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "golang", parsed.GetString("keyword"))
	assert.Equal(t, 20, parsed.GetInt("limit"))

	// Test with alias
	parsed, err = ParseFromDefinition("/find golang", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "golang", parsed.GetString("keyword"))
	assert.Equal(t, 10, parsed.GetInt("limit")) // default value
}

// TestParseFromDefinition_WithSubCommands 测试带子命令的定义解析
func TestParseFromDefinition_WithSubCommands(t *testing.T) {
	def := &Definition{
		Name: "docker",
		SubCommands: []*Definition{
			{
				Name: "run",
				Arguments: []*Argument{
					{Name: "image", Type: ArgTypeString, Required: true},
				},
				Flags: []*Flag{
					{Name: "detach", ShortName: "d", Type: ArgTypeBool},
					{Name: "port", ShortName: "p", Type: ArgTypeString},
				},
			},
			{
				Name: "ps",
				Flags: []*Flag{
					{Name: "all", ShortName: "a", Type: ArgTypeBool},
				},
			},
		},
	}

	parsed, err := ParseFromDefinition("/docker run nginx -d -p 8080:80", def, "/")
	require.NoError(t, err)
	assert.Equal(t, []string{"docker", "run"}, parsed.CommandPath)
	assert.Equal(t, "nginx", parsed.GetString("image"))
	assert.True(t, parsed.GetBool("detach"))
	assert.Equal(t, "8080:80", parsed.GetString("port"))
}

// TestParseFromDefinition_CommandMismatch 测试命令不匹配
func TestParseFromDefinition_CommandMismatch(t *testing.T) {
	def := &Definition{Name: "expected"}

	_, err := ParseFromDefinition("/actual arg", def, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command mismatch")
}

// TestParseFromDefinition_NilDefinition 测试空定义
func TestParseFromDefinition_NilDefinition(t *testing.T) {
	_, err := ParseFromDefinition("/cmd", nil, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "root command definition is nil")
}

// TestArgTypes 测试各种参数类型
func TestArgTypes(t *testing.T) {
	tests := []struct {
		name     string
		def      *Definition
		input    string
		validate func(t *testing.T, parsed *Parsed)
	}{
		{
			name: "String type",
			def: &Definition{
				Name: "cmd",
				Arguments: []*Argument{
					{Name: "text", Type: ArgTypeString},
				},
			},
			input: "/cmd hello",
			validate: func(t *testing.T, parsed *Parsed) {
				assert.Equal(t, "hello", parsed.GetString("text"))
			},
		},
		{
			name: "Int type",
			def: &Definition{
				Name: "cmd",
				Arguments: []*Argument{
					{Name: "number", Type: ArgTypeInt},
				},
			},
			input: "/cmd 42",
			validate: func(t *testing.T, parsed *Parsed) {
				assert.Equal(t, 42, parsed.GetInt("number"))
			},
		},
		{
			name: "Bool type - explicit true",
			def: &Definition{
				Name: "cmd",
				Flags: []*Flag{
					{Name: "enabled", Type: ArgTypeBool},
				},
			},
			input: "/cmd --enabled true",
			validate: func(t *testing.T, parsed *Parsed) {
				assert.True(t, parsed.GetBool("enabled"))
			},
		},
		{
			name: "Bool type - implicit true",
			def: &Definition{
				Name: "cmd",
				Flags: []*Flag{
					{Name: "enabled", Type: ArgTypeBool},
				},
			},
			input: "/cmd --enabled",
			validate: func(t *testing.T, parsed *Parsed) {
				assert.True(t, parsed.GetBool("enabled"))
			},
		},
		{
			name: "Float type",
			def: &Definition{
				Name: "cmd",
				Arguments: []*Argument{
					{Name: "price", Type: ArgTypeFloat},
				},
			},
			input: "/cmd 19.99",
			validate: func(t *testing.T, parsed *Parsed) {
				assert.Equal(t, 19.99, parsed.GetFloat("price"))
			},
		},
		{
			name: "StringSlice type - positional",
			def: &Definition{
				Name: "cmd",
				Arguments: []*Argument{
					{Name: "files", Type: ArgTypeStringSlice},
				},
			},
			input: "/cmd file1.txt file2.txt file3.txt",
			validate: func(t *testing.T, parsed *Parsed) {
				files := parsed.Arguments["files"].([]string)
				assert.Equal(t, []string{"file1.txt", "file2.txt", "file3.txt"}, files)
			},
		},
		{
			name: "StringSlice type - flag",
			def: &Definition{
				Name: "cmd",
				Flags: []*Flag{
					{Name: "tags", Type: ArgTypeStringSlice},
				},
			},
			input: "/cmd --tags 'go rust python'",
			validate: func(t *testing.T, parsed *Parsed) {
				tags := parsed.Flags["tags"].([]string)
				assert.Equal(t, []string{"go", "rust", "python"}, tags)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseFromDefinition(tt.input, tt.def, "/")
			require.NoError(t, err)
			tt.validate(t, parsed)
		})
	}
}

// TestRequiredArguments 测试必填参数
func TestRequiredArguments(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "required", Type: ArgTypeString, Required: true},
			{Name: "optional", Type: ArgTypeString, Required: false, Default: "default"},
		},
	}

	// Missing required argument
	_, err := ParseFromDefinition("/cmd", def, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required argument")

	// With required argument
	parsed, err := ParseFromDefinition("/cmd value", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "value", parsed.GetString("required"))
	assert.Equal(t, "default", parsed.GetString("optional"))

	// With both arguments
	parsed, err = ParseFromDefinition("/cmd value1 value2", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "value1", parsed.GetString("required"))
	assert.Equal(t, "value2", parsed.GetString("optional"))
}

// TestRequiredFlags 测试必填标志
func TestRequiredFlags(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Flags: []*Flag{
			{Name: "required", Type: ArgTypeString, Required: true},
			{Name: "optional", Type: ArgTypeString, Required: false, Default: "default"},
		},
	}

	// Missing required flag
	_, err := ParseFromDefinition("/cmd", def, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required flag")

	// With required flag
	parsed, err := ParseFromDefinition("/cmd --required value", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "value", parsed.GetString("required"))
	assert.Equal(t, "default", parsed.GetString("optional"))
}

// TestShortFlags 测试短标志
func TestShortFlags(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Flags: []*Flag{
			{Name: "verbose", ShortName: "v", Type: ArgTypeBool},
			{Name: "output", ShortName: "o", Type: ArgTypeString},
		},
	}

	// Using short flags
	parsed, err := ParseFromDefinition("/cmd -v -o output.txt", def, "/")
	require.NoError(t, err)
	assert.True(t, parsed.GetBool("verbose"))
	assert.Equal(t, "output.txt", parsed.GetString("output"))

	// Using long flags
	parsed, err = ParseFromDefinition("/cmd --verbose --output output.txt", def, "/")
	require.NoError(t, err)
	assert.True(t, parsed.GetBool("verbose"))
	assert.Equal(t, "output.txt", parsed.GetString("output"))
}

// TestArgumentValidator 测试参数验证器
func TestArgumentValidator(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{
				Name:     "port",
				Type:     ArgTypeString,
				Required: true,
				Validator: func(s string) error {
					var port int
					_, err := fmt.Sscanf(s, "%d", &port)
					if err != nil || port < 1 || port > 65535 {
						return fmt.Errorf("port must be between 1 and 65535")
					}
					return nil
				},
			},
		},
	}

	// Valid port
	_, err := ParseFromDefinition("/cmd 8080", def, "/")
	assert.NoError(t, err)

	// Invalid port - out of range
	_, err = ParseFromDefinition("/cmd 70000", def, "/")
	assert.Error(t, err)
}

// TestFlagValidator 测试标志验证器
func TestFlagValidator(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Flags: []*Flag{
			{
				Name:     "email",
				Type:     ArgTypeString,
				Required: true,
				Validator: func(s string) error {
					if !strings.Contains(s, "@") {
						return fmt.Errorf("invalid email format")
					}
					return nil
				},
			},
		},
	}

	// Valid email
	_, err := ParseFromDefinition("/cmd --email user@example.com", def, "/")
	assert.NoError(t, err)

	// Invalid email
	_, err = ParseFromDefinition("/cmd --email invalid", def, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid email format")
}

// TestDefinitionValidator 测试定义级验证器
func TestDefinitionValidator(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "start", Type: ArgTypeInt, Required: true},
			{Name: "end", Type: ArgTypeInt, Required: true},
		},
		Validator: func(p *Parsed) error {
			start := p.GetInt("start")
			end := p.GetInt("end")
			if start >= end {
				return fmt.Errorf("start must be less than end")
			}
			return nil
		},
	}

	// Valid range
	_, err := ParseFromDefinition("/cmd 1 10", def, "/")
	assert.NoError(t, err)

	// Invalid range
	_, err = ParseFromDefinition("/cmd 10 1", def, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "start must be less than end")
}

// TestStringSliceMustBeLast 测试 StringSlice 必须是最后一个参数
func TestStringSliceMustBeLast(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "files", Type: ArgTypeStringSlice},
			{Name: "other", Type: ArgTypeString}, // This should cause an error
		},
	}

	_, err := ParseFromDefinition("/cmd arg1 arg2", def, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stringSlice must be the last positional argument")
}

// TestBoolParsing 测试布尔值解析的多种格式
func TestBoolParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
		wantErr  bool
	}{
		{"true", true, false},
		{"false", false, false},
		{"1", true, false},
		{"0", false, false},
		{"yes", true, false},
		{"no", false, false},
		{"on", true, false},
		{"off", false, false},
		// "invalid" is not a recognised bool literal, so the type-aware parser
		// does NOT consume it as the flag value.  Instead the flag is implicitly
		// true and "invalid" becomes a positional argument (silently ignored here
		// because the command defines no Arguments).  This matches Cobra / pflags
		// behaviour: use --flag=value or an explicit bool literal for falsy values.
		{"invalid", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			def := &Definition{
				Name: "cmd",
				Flags: []*Flag{
					{Name: "flag", Type: ArgTypeBool},
				},
			}

			parsed, err := ParseFromDefinition(fmt.Sprintf("/cmd --flag %s", tt.input), def, "/")
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, parsed.GetBool("flag"))
		})
	}
}

// TestCommandAliases 测试命令别名
func TestCommandAliases(t *testing.T) {
	parser := NewParser("/")

	def := &Definition{
		Name:    "list",
		Aliases: []string{"ls", "l"},
		Arguments: []*Argument{
			{Name: "path", Type: ArgTypeString, Default: "."},
		},
	}

	parser.Register(def)

	// Test primary name
	parsed, err := parser.Parse("/list /home")
	require.NoError(t, err)
	assert.Equal(t, "/home", parsed.GetString("path"))

	// Test alias 1
	parsed, err = parser.Parse("/ls /home")
	require.NoError(t, err)
	assert.Equal(t, "/home", parsed.GetString("path"))

	// Test alias 2
	parsed, err = parser.Parse("/l /home")
	require.NoError(t, err)
	assert.Equal(t, "/home", parsed.GetString("path"))
}

// TestParsed_GetMethods 测试 Parsed 的 Get 方法
func TestParsed_GetMethods(t *testing.T) {
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

	parsed, err := ParseFromDefinition("/cmd hello 42 --bool true --float 3.14", def, "/")
	require.NoError(t, err)

	// Test existing values
	assert.Equal(t, "hello", parsed.GetString("str"))
	assert.Equal(t, 42, parsed.GetInt("num"))
	assert.True(t, parsed.GetBool("bool"))
	assert.Equal(t, 3.14, parsed.GetFloat("float"))

	// Test non-existing values (should return zero values)
	assert.Equal(t, "", parsed.GetString("nonexistent"))
	assert.Equal(t, 0, parsed.GetInt("nonexistent"))
	assert.False(t, parsed.GetBool("nonexistent"))
	assert.Equal(t, 0.0, parsed.GetFloat("nonexistent"))
}

// TestComplexCommandTree 测试复杂命令树
func TestComplexCommandTree(t *testing.T) {
	parser := NewParser("/")

	def := &Definition{
		Name: "kubectl",
		SubCommands: []*Definition{
			{
				Name: "get",
				SubCommands: []*Definition{
					{
						Name: "pods",
						Flags: []*Flag{
							{Name: "namespace", ShortName: "n", Type: ArgTypeString, Default: "default"},
							{Name: "all-namespaces", ShortName: "A", Type: ArgTypeBool},
						},
					},
					{
						Name: "services",
						Flags: []*Flag{
							{Name: "namespace", ShortName: "n", Type: ArgTypeString, Default: "default"},
						},
					},
				},
			},
			{
				Name: "describe",
				Arguments: []*Argument{
					{Name: "resource", Type: ArgTypeString, Required: true},
					{Name: "name", Type: ArgTypeString, Required: true},
				},
			},
		},
	}

	parser.Register(def)

	// Test deep nested command
	parsed, err := parser.Parse("/kubectl get pods -n kube-system")
	require.NoError(t, err)
	assert.Equal(t, []string{"kubectl", "get", "pods"}, parsed.CommandPath)
	assert.Equal(t, "kube-system", parsed.GetString("namespace"))

	// Test another branch
	parsed, err = parser.Parse("/kubectl describe pod my-pod")
	require.NoError(t, err)
	assert.Equal(t, []string{"kubectl", "describe"}, parsed.CommandPath)
	assert.Equal(t, "pod", parsed.GetString("resource"))
	assert.Equal(t, "my-pod", parsed.GetString("name"))
}

// TestInvalidIntParsing 测试无效整数解析
func TestInvalidIntParsing(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "num", Type: ArgTypeInt, Required: true},
		},
	}

	_, err := ParseFromDefinition("/cmd not-a-number", def, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "argument num")
}

// TestInvalidFloatParsing 测试无效浮点数解析
func TestInvalidFloatParsing(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "price", Type: ArgTypeFloat, Required: true},
		},
	}

	_, err := ParseFromDefinition("/cmd not-a-float", def, "/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "argument price")
}

// TestHierarchicalFlagScoping verifies that flags typed before a subcommand
// token belong to the parent level and flags after it belong to the child level.
// This is the Cobra / GNU getopt convention; the old global-extraction approach
// would silently lose the parent-level flag.
func TestHierarchicalFlagScoping(t *testing.T) {
	// cmd tree:
	//   /debug
	//     └── runtime  (has --output flag)
	//
	// /debug itself has --verbose (bool) flag.
	def := &Definition{
		Name: "debug",
		Flags: []*Flag{
			{Name: "verbose", ShortName: "v", Type: ArgTypeBool},
		},
		SubCommands: []*Definition{
			{
				Name: "runtime",
				Flags: []*Flag{
					{Name: "output", ShortName: "o", Type: ArgTypeString},
				},
			},
		},
	}

	parser := NewParser("/")
	parser.Register(def)

	t.Run("flag before subcommand belongs to parent (scoped, leaf wins)", func(t *testing.T) {
		// --verbose appears before the subcommand → it is at the debug scope.
		// The leaf (runtime) has --output after the subcommand.
		parsed, err := parser.Parse("/debug --verbose runtime --output json")
		require.NoError(t, err)
		assert.Equal(t, []string{"debug", "runtime"}, parsed.CommandPath)
		// runtime's --output must be resolved correctly
		assert.Equal(t, "json", parsed.GetString("output"))
	})

	t.Run("bool flag before subcommand does not consume subcommand as value", func(t *testing.T) {
		// --verbose is a bool flag; "runtime" must NOT be eaten as its value.
		parsed, err := parser.Parse("/debug --verbose runtime")
		require.NoError(t, err)
		assert.Equal(t, []string{"debug", "runtime"}, parsed.CommandPath)
	})

	t.Run("bool flag with explicit false before subcommand", func(t *testing.T) {
		parsed, err := parser.Parse("/debug --verbose false runtime --output log.txt")
		require.NoError(t, err)
		assert.Equal(t, []string{"debug", "runtime"}, parsed.CommandPath)
		assert.Equal(t, "log.txt", parsed.GetString("output"))
	})

	t.Run("flags after subcommand belong to child", func(t *testing.T) {
		parsed, err := parser.Parse("/debug runtime --output result.txt")
		require.NoError(t, err)
		assert.Equal(t, []string{"debug", "runtime"}, parsed.CommandPath)
		assert.Equal(t, "result.txt", parsed.GetString("output"))
	})

	t.Run("flat command with flag at any position", func(t *testing.T) {
		// /help has no subcommands — flags are free to appear anywhere.
		helpDef := &Definition{
			Name: "help",
			Arguments: []*Argument{
				{Name: "page", Type: ArgTypeInt, Default: 1},
			},
			Flags: []*Flag{
				{Name: "topic", ShortName: "t", Type: ArgTypeString},
			},
		}
		p := NewParser("/")
		p.Register(helpDef)

		parsed, err := p.Parse("/help 2 -t basics")
		require.NoError(t, err)
		assert.Equal(t, 2, parsed.GetInt("page"))
		assert.Equal(t, "basics", parsed.GetString("topic"))
	})
}

// TestPOSIXDoubleDash verifies that "--" terminates flag parsing and all
// subsequent tokens are treated as positional arguments.
func TestPOSIXDoubleDash(t *testing.T) {
	def := &Definition{
		Name: "run",
		Arguments: []*Argument{
			{Name: "args", Type: ArgTypeStringSlice},
		},
		Flags: []*Flag{
			{Name: "detach", ShortName: "d", Type: ArgTypeBool},
		},
	}

	parsed, err := ParseFromDefinition("/run -d -- --not-a-flag another", def, "/")
	require.NoError(t, err)
	assert.True(t, parsed.GetBool("detach"))
	// "--not-a-flag" and "another" must be positional args, NOT flags
	args := parsed.Arguments["args"].([]string)
	assert.Equal(t, []string{"--not-a-flag", "another"}, args)
}

// TestBoolFlagEqualsSyntax verifies that --flag=false explicitly sets a bool
// flag to false even with the type-aware parser.
func TestBoolFlagEqualsSyntax(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Flags: []*Flag{
			{Name: "enabled", Type: ArgTypeBool},
		},
	}

	parsed, err := ParseFromDefinition("/cmd --enabled=false", def, "/")
	require.NoError(t, err)
	assert.False(t, parsed.GetBool("enabled"))

	parsed, err = ParseFromDefinition("/cmd --enabled=true", def, "/")
	require.NoError(t, err)
	assert.True(t, parsed.GetBool("enabled"))
}
