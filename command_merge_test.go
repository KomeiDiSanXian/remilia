package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/stretchr/testify/require"
)

func TestCommandParser_UsesSharedRawParser(t *testing.T) {
	// 这个用例的意义：确保增强解析器与基础解析器确实走同一个入口
	// （如果未来又引入第二套 tokenize/parse，很容易在这里分叉）。
	input := `/echo "hello world" --unit celsius`

	raw, err := command.ParseCommandLine(input)
	require.NoError(t, err)
	require.Equal(t, "/echo", raw.Command)
	require.Equal(t, []string{"hello world"}, raw.Positional)
	require.Equal(t, "celsius", raw.Flags["unit"])

	p := command.NewParser("/")
	p.Register(&command.Definition{Name: "echo"})

	parsed, err := p.Parse(input)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.NotNil(t, parsed.Definition)
	require.Equal(t, []string{"echo"}, parsed.CommandPath)
}

func TestCommandParser_StringSliceArgument_LastOnly(t *testing.T) {
	p := command.NewParser("/")
	p.Register(&command.Definition{
		Name:      "tags",
		Arguments: []*command.Argument{{Name: "vals", Type: command.ArgTypeStringSlice, Required: true}},
	})

	parsed, err := p.Parse("/tags a b c")
	require.NoError(t, err)
	vals, ok := parsed.Arguments["vals"].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"a", "b", "c"}, vals)
}

func TestCommandParser_StringSliceNotLast_ShouldError(t *testing.T) {
	p := command.NewParser("/")
	p.Register(&command.Definition{
		Name: "bad",
		Arguments: []*command.Argument{
			{Name: "vals", Type: command.ArgTypeStringSlice, Required: false},
			{Name: "x", Type: command.ArgTypeString, Required: false},
		},
	})

	_, err := p.Parse("/bad a b")
	require.Error(t, err)
}

func TestCommandParser_StringSliceFlag(t *testing.T) {
	p := command.NewParser("/")
	p.Register(&command.Definition{
		Name:  "tags",
		Flags: []*command.Flag{{Name: "tags", Type: command.ArgTypeStringSlice, Required: true}},
	})

	parsed, err := p.Parse(`/tags --tags "a b c"`)
	require.NoError(t, err)

	vals, ok := parsed.Flags["tags"].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"a", "b", "c"}, vals)
}

func TestCommandParser_StringSliceFlag_WithoutQuotes_NotSupported(t *testing.T) {
	p := command.NewParser("/")
	p.Register(&command.Definition{
		Name:      "tags",
		Flags:     []*command.Flag{{Name: "tags", Type: command.ArgTypeStringSlice, Required: true}},
		Arguments: []*command.Argument{{Name: "rest", Type: command.ArgTypeStringSlice, Required: false}},
	})

	parsed, err := p.Parse(`/tags --tags a b c`)
	require.NoError(t, err)

	vals, ok := parsed.Flags["tags"].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"a"}, vals)

	rest, ok := parsed.Arguments["rest"].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"b", "c"}, rest)
}
