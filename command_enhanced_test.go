package remilia

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandParser_BasicCommand(t *testing.T) {
	parser := NewCommandParser("/")

	// 注册简单命令
	parser.Register(&CommandDefinition{
		Name:        "ping",
		Description: "测试连接",
	})

	parsed, err := parser.Parse("/ping")
	require.NoError(t, err)
	assert.Equal(t, []string{"ping"}, parsed.CommandPath)
	assert.Equal(t, "ping", parsed.Definition.Name)
}

func TestCommandParser_SubCommands(t *testing.T) {
	parser := NewCommandParser("/")

	// 注册带子命令的命令
	parser.Register(&CommandDefinition{
		Name:        "admin",
		Description: "管理员命令",
		SubCommands: []*CommandDefinition{
			{
				Name:        "user",
				Description: "用户管理",
				SubCommands: []*CommandDefinition{
					{
						Name:        "list",
						Description: "列出用户",
					},
					{
						Name:        "add",
						Description: "添加用户",
						Arguments: []*Argument{
							{Name: "username", Type: ArgTypeString, Required: true},
							{Name: "email", Type: ArgTypeString, Required: false},
						},
					},
				},
			},
		},
	})

	// 测试多层子命令
	parsed, err := parser.Parse("/admin user list")
	require.NoError(t, err)
	assert.Equal(t, []string{"admin", "user", "list"}, parsed.CommandPath)
	assert.Equal(t, "list", parsed.Definition.Name)

	// 测试带参数的子命令
	parsed, err = parser.Parse("/admin user add john john@example.com")
	require.NoError(t, err)
	assert.Equal(t, []string{"admin", "user", "add"}, parsed.CommandPath)
	assert.Equal(t, "john", parsed.GetString("username"))
	assert.Equal(t, "john@example.com", parsed.GetString("email"))
}

func TestCommandParser_Arguments(t *testing.T) {
	parser := NewCommandParser("/")

	parser.Register(&CommandDefinition{
		Name: "weather",
		Arguments: []*Argument{
			{Name: "city", Type: ArgTypeString, Required: true, Description: "城市名称"},
			{Name: "days", Type: ArgTypeInt, Required: false, Default: 3, Description: "天数"},
		},
	})

	t.Run("所有参数", func(t *testing.T) {
		parsed, err := parser.Parse("/weather Beijing 7")
		require.NoError(t, err)
		assert.Equal(t, "Beijing", parsed.GetString("city"))
		assert.Equal(t, 7, parsed.GetInt("days"))
	})

	t.Run("缺少可选参数使用默认值", func(t *testing.T) {
		parsed, err := parser.Parse("/weather Shanghai")
		require.NoError(t, err)
		assert.Equal(t, "Shanghai", parsed.GetString("city"))
		assert.Equal(t, 3, parsed.GetInt("days"))
	})

	t.Run("缺少必需参数报错", func(t *testing.T) {
		_, err := parser.Parse("/weather")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required argument")
	})
}

func TestCommandParser_Flags(t *testing.T) {
	parser := NewCommandParser("/")

	parser.Register(&CommandDefinition{
		Name: "search",
		Arguments: []*Argument{
			{Name: "query", Type: ArgTypeString, Required: true},
		},
		Flags: []*Flag{
			{Name: "limit", ShortName: "l", Type: ArgTypeInt, Default: 10},
			{Name: "sort", ShortName: "s", Type: ArgTypeString, Default: "relevance"},
			{Name: "verbose", ShortName: "v", Type: ArgTypeBool, Default: false},
		},
	})

	t.Run("长选项", func(t *testing.T) {
		parsed, err := parser.Parse("/search hello --limit 20 --sort date")
		require.NoError(t, err)
		assert.Equal(t, "hello", parsed.GetString("query"))
		assert.Equal(t, 20, parsed.GetInt("limit"))
		assert.Equal(t, "date", parsed.GetString("sort"))
	})

	t.Run("短选项", func(t *testing.T) {
		parsed, err := parser.Parse("/search world -l 5 -v")
		require.NoError(t, err)
		assert.Equal(t, "world", parsed.GetString("query"))
		assert.Equal(t, 5, parsed.GetInt("limit"))
		assert.True(t, parsed.GetBool("verbose"))
	})

	t.Run("默认值", func(t *testing.T) {
		parsed, err := parser.Parse("/search test")
		require.NoError(t, err)
		assert.Equal(t, 10, parsed.GetInt("limit"))
		assert.Equal(t, "relevance", parsed.GetString("sort"))
		assert.False(t, parsed.GetBool("verbose"))
	})
}

func TestCommandParser_TypeValidation(t *testing.T) {
	parser := NewCommandParser("/")

	parser.Register(&CommandDefinition{
		Name: "set",
		Arguments: []*Argument{
			{Name: "count", Type: ArgTypeInt, Required: true},
			{Name: "ratio", Type: ArgTypeFloat, Required: false},
			{Name: "enabled", Type: ArgTypeBool, Required: false},
		},
	})

	t.Run("有效的类型", func(t *testing.T) {
		parsed, err := parser.Parse("/set 42 3.14 true")
		require.NoError(t, err)
		assert.Equal(t, 42, parsed.GetInt("count"))
		assert.Equal(t, 3.14, parsed.GetFloat("ratio"))
		assert.True(t, parsed.GetBool("enabled"))
	})

	t.Run("无效的整数", func(t *testing.T) {
		_, err := parser.Parse("/set abc")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "count")
	})

	t.Run("无效的布尔值", func(t *testing.T) {
		_, err := parser.Parse("/set 10 0.5 maybe")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "boolean")
	})
}

func TestCommandParser_CustomValidator(t *testing.T) {
	parser := NewCommandParser("/")

	parser.Register(&CommandDefinition{
		Name: "age",
		Arguments: []*Argument{
			{
				Name:     "value",
				Type:     ArgTypeInt,
				Required: true,
				Validator: func(s string) error {
					val, _ := parseValue(s, ArgTypeInt)
					age := val.(int)
					if age < 0 || age > 150 {
						return fmt.Errorf("age must be between 0 and 150")
					}
					return nil
				},
			},
		},
	})

	t.Run("有效年龄", func(t *testing.T) {
		_, err := parser.Parse("/age 25")
		assert.NoError(t, err)
	})

	t.Run("无效年龄", func(t *testing.T) {
		_, err := parser.Parse("/age 200")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "age must be between")
	})
}

func TestCommandParser_Aliases(t *testing.T) {
	parser := NewCommandParser("/")

	parser.Register(&CommandDefinition{
		Name:    "help",
		Aliases: []string{"h", "?"},
	})

	tests := []string{"/help", "/h", "/?"}
	for _, input := range tests {
		parsed, err := parser.Parse(input)
		require.NoError(t, err, "input: %s", input)
		assert.Equal(t, "help", parsed.Definition.Name)
	}
}

func TestCommandParser_ComplexExample(t *testing.T) {
	parser := NewCommandParser("/")

	// 构建一个复杂的命令树
	parser.Register(&CommandDefinition{
		Name:        "bot",
		Description: "机器人管理",
		SubCommands: []*CommandDefinition{
			{
				Name:        "plugin",
				Description: "插件管理",
				SubCommands: []*CommandDefinition{
					{
						Name:        "list",
						Description: "列出所有插件",
						Flags: []*Flag{
							{Name: "verbose", ShortName: "v", Type: ArgTypeBool, Default: false},
						},
					},
					{
						Name:        "enable",
						Description: "启用插件",
						Arguments: []*Argument{
							{Name: "name", Type: ArgTypeString, Required: true},
						},
					},
					{
						Name:        "disable",
						Description: "禁用插件",
						Arguments: []*Argument{
							{Name: "name", Type: ArgTypeString, Required: true},
						},
						Flags: []*Flag{
							{Name: "force", ShortName: "f", Type: ArgTypeBool, Default: false},
						},
					},
				},
			},
			{
				Name:        "config",
				Description: "配置管理",
				SubCommands: []*CommandDefinition{
					{
						Name:        "get",
						Description: "获取配置",
						Arguments: []*Argument{
							{Name: "key", Type: ArgTypeString, Required: true},
						},
					},
					{
						Name:        "set",
						Description: "设置配置",
						Arguments: []*Argument{
							{Name: "key", Type: ArgTypeString, Required: true},
							{Name: "value", Type: ArgTypeString, Required: true},
						},
					},
				},
			},
		},
	})

	t.Run("插件列表", func(t *testing.T) {
		parsed, err := parser.Parse("/bot plugin list -v")
		require.NoError(t, err)
		assert.Equal(t, []string{"bot", "plugin", "list"}, parsed.CommandPath)
		assert.True(t, parsed.GetBool("verbose"))
	})

	t.Run("启用插件", func(t *testing.T) {
		parsed, err := parser.Parse("/bot plugin enable weather")
		require.NoError(t, err)
		assert.Equal(t, "weather", parsed.GetString("name"))
	})

	t.Run("禁用插件带 force", func(t *testing.T) {
		parsed, err := parser.Parse("/bot plugin disable weather --force")
		require.NoError(t, err)
		assert.Equal(t, "weather", parsed.GetString("name"))
		assert.True(t, parsed.GetBool("force"))
	})

	t.Run("设置配置", func(t *testing.T) {
		parsed, err := parser.Parse("/bot config set timeout 30")
		require.NoError(t, err)
		assert.Equal(t, "timeout", parsed.GetString("key"))
		assert.Equal(t, "30", parsed.GetString("value"))
	})
}

func TestCommandParser_GenerateHelp(t *testing.T) {
	parser := NewCommandParser("/")

	parser.Register(&CommandDefinition{
		Name:        "weather",
		Description: "查询天气",
		Arguments: []*Argument{
			{Name: "city", Type: ArgTypeString, Required: true, Description: "城市名称"},
			{Name: "days", Type: ArgTypeInt, Required: false, Description: "天数"},
		},
		Flags: []*Flag{
			{Name: "unit", ShortName: "u", Type: ArgTypeString, Description: "温度单位"},
			{Name: "verbose", ShortName: "v", Type: ArgTypeBool, Description: "详细输出"},
		},
	})

	parser.Register(&CommandDefinition{
		Name:        "admin",
		Description: "管理员命令",
		SubCommands: []*CommandDefinition{
			{Name: "user", Description: "用户管理"},
			{Name: "system", Description: "系统管理"},
		},
	})

	t.Run("生成所有命令帮助", func(t *testing.T) {
		help := parser.GenerateHelp()
		assert.Contains(t, help, "weather")
		assert.Contains(t, help, "admin")
		assert.Contains(t, help, "查询天气")
		assert.Contains(t, help, "管理员命令")
	})

	t.Run("生成特定命令帮助", func(t *testing.T) {
		help := parser.GenerateHelp("weather")
		assert.Contains(t, help, "Command: /weather")
		assert.Contains(t, help, "Arguments:")
		assert.Contains(t, help, "city")
		assert.Contains(t, help, "Options:")
		assert.Contains(t, help, "--unit")
		assert.Contains(t, help, "-v")
	})

	t.Run("命令不存在", func(t *testing.T) {
		help := parser.GenerateHelp("notfound")
		assert.Contains(t, help, "Command not found")
	})
}

func TestCommandParser_RequiredFlags(t *testing.T) {
	parser := NewCommandParser("/")

	parser.Register(&CommandDefinition{
		Name: "login",
		Flags: []*Flag{
			{Name: "username", Type: ArgTypeString, Required: true},
			{Name: "password", Type: ArgTypeString, Required: true},
			{Name: "remember", Type: ArgTypeBool, Required: false, Default: false},
		},
	})

	t.Run("提供所有必需参数", func(t *testing.T) {
		parsed, err := parser.Parse("/login --username admin --password secret")
		require.NoError(t, err)
		assert.Equal(t, "admin", parsed.GetString("username"))
		assert.Equal(t, "secret", parsed.GetString("password"))
	})

	t.Run("缺少必需参数", func(t *testing.T) {
		_, err := parser.Parse("/login --username admin")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required flag")
		assert.Contains(t, err.Error(), "password")
	})
}

func TestCommandParser_RealWorldExample(t *testing.T) {
	// 模拟真实的机器人命令系统
	parser := NewCommandParser("/")

	// 注册多个实用命令
	parser.Register(&CommandDefinition{
		Name:        "weather",
		Aliases:     []string{"w"},
		Description: "查询天气信息",
		Arguments: []*Argument{
			{Name: "city", Type: ArgTypeString, Required: true, Description: "城市名称"},
		},
		Flags: []*Flag{
			{Name: "days", ShortName: "d", Type: ArgTypeInt, Default: 3, Description: "预报天数"},
			{Name: "unit", ShortName: "u", Type: ArgTypeString, Default: "celsius", Description: "温度单位 (celsius/fahrenheit)"},
		},
	})

	parser.Register(&CommandDefinition{
		Name:        "remind",
		Aliases:     []string{"r", "reminder"},
		Description: "设置提醒",
		Arguments: []*Argument{
			{Name: "message", Type: ArgTypeString, Required: true, Description: "提醒内容"},
			{Name: "time", Type: ArgTypeString, Required: true, Description: "时间 (格式: HH:MM)"},
		},
		Flags: []*Flag{
			{Name: "repeat", Type: ArgTypeBool, Default: false, Description: "是否重复"},
		},
	})

	t.Run("天气查询 - 完整参数", func(t *testing.T) {
		parsed, err := parser.Parse("/weather Beijing --days 7 --unit fahrenheit")
		require.NoError(t, err)
		assert.Equal(t, "Beijing", parsed.GetString("city"))
		assert.Equal(t, 7, parsed.GetInt("days"))
		assert.Equal(t, "fahrenheit", parsed.GetString("unit"))
	})

	t.Run("天气查询 - 使用别名", func(t *testing.T) {
		parsed, err := parser.Parse("/w Shanghai")
		require.NoError(t, err)
		assert.Equal(t, "Shanghai", parsed.GetString("city"))
		assert.Equal(t, 3, parsed.GetInt("days")) // 默认值
	})

	t.Run("提醒 - 带重复", func(t *testing.T) {
		parsed, err := parser.Parse("/remind '吃药' 09:00 --repeat")
		require.NoError(t, err)
		assert.Equal(t, "吃药", parsed.GetString("message"))
		assert.Equal(t, "09:00", parsed.GetString("time"))
		assert.True(t, parsed.GetBool("repeat"))
	})
}
