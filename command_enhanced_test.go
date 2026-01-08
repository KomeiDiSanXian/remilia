package remilia

import (
	"fmt"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandParser_BasicCommand(t *testing.T) {
	parser := command.NewCommandParser("/")

	parser.Register(&command.CommandDefinition{
		Name:        "ping",
		Description: "测试连接",
	})

	parsed, err := parser.Parse("/ping")
	require.NoError(t, err)
	assert.Equal(t, []string{"ping"}, parsed.CommandPath)
	assert.Equal(t, "ping", parsed.Definition.Name)
}

func TestCommandParser_SubCommands(t *testing.T) {
	parser := command.NewCommandParser("/")

	parser.Register(&command.CommandDefinition{
		Name:        "admin",
		Description: "管理员命令",
		SubCommands: []*command.CommandDefinition{
			{
				Name:        "user",
				Description: "用户管理",
				SubCommands: []*command.CommandDefinition{
					{
						Name:        "list",
						Description: "列出用户",
					},
					{
						Name:        "add",
						Description: "添加用户",
						Arguments: []*command.Argument{
							{Name: "username", Type: command.ArgTypeString, Required: true},
							{Name: "email", Type: command.ArgTypeString, Required: false},
						},
					},
				},
			},
		},
	})

	parsed, err := parser.Parse("/admin user list")
	require.NoError(t, err)
	assert.Equal(t, []string{"admin", "user", "list"}, parsed.CommandPath)
	assert.Equal(t, "list", parsed.Definition.Name)

	parsed, err = parser.Parse("/admin user add john john@example.com")
	require.NoError(t, err)
	assert.Equal(t, []string{"admin", "user", "add"}, parsed.CommandPath)
	assert.Equal(t, "john", parsed.GetString("username"))
	assert.Equal(t, "john@example.com", parsed.GetString("email"))
}

func TestCommandParser_Arguments(t *testing.T) {
	parser := command.NewCommandParser("/")

	parser.Register(&command.CommandDefinition{
		Name: "echo",
		Arguments: []*command.Argument{
			{Name: "text", Type: command.ArgTypeString, Required: true},
			{Name: "times", Type: command.ArgTypeInt, Required: false, Default: 1},
		},
	})

	parsed, err := parser.Parse("/echo hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", parsed.GetString("text"))
	assert.Equal(t, 1, parsed.GetInt("times"))

	parsed, err = parser.Parse("/echo hello 3")
	require.NoError(t, err)
	assert.Equal(t, "hello", parsed.GetString("text"))
	assert.Equal(t, 3, parsed.GetInt("times"))
	_, err = parser.Parse("/echo")
	require.Error(t, err)
}

func TestCommandParser_Flags(t *testing.T) {
	parser := command.NewCommandParser("/")

	parser.Register(&command.CommandDefinition{
		Name: "search",
		Flags: []*command.Flag{
			{Name: "query", ShortName: "q", Type: command.ArgTypeString, Required: true},
			{Name: "limit", ShortName: "l", Type: command.ArgTypeInt, Required: false, Default: 10},
			{Name: "verbose", ShortName: "v", Type: command.ArgTypeBool, Required: false, Default: false},
		},
	})

	parsed, err := parser.Parse("/search --query golang")
	require.NoError(t, err)
	assert.Equal(t, "golang", parsed.GetString("query"))
	assert.Equal(t, 10, parsed.GetInt("limit"))
	assert.False(t, parsed.GetBool("verbose"))

	parsed, err = parser.Parse("/search -q golang -l 5 -v true")
	require.NoError(t, err)
	assert.Equal(t, "golang", parsed.GetString("query"))
	assert.Equal(t, 5, parsed.GetInt("limit"))
	assert.True(t, parsed.GetBool("verbose"))
	_, err = parser.Parse("/search")
	require.Error(t, err)
}

func TestCommandParser_CustomValidator(t *testing.T) {
	parser := command.NewCommandParser("/")

	parser.Register(&command.CommandDefinition{
		Name:      "age",
		Arguments: []*command.Argument{{Name: "n", Type: command.ArgTypeInt, Required: true}},
		Validator: func(cmd *command.ParsedCommand) error {
			if cmd.GetInt("n") < 0 {
				return fmt.Errorf("age must be non-negative")
			}
			return nil
		},
	})

	_, err := parser.Parse("/age -1")
	require.Error(t, err)
}
