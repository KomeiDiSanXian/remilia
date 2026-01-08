package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestParseCommand_Simple(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"Hello World"}`),
	}, nil)

	args, err := ctx.ParseCommand()
	assert.NoError(t, err)
	assert.Equal(t, "Hello", args.Command)
	assert.Equal(t, []string{"World"}, args.Positional)
	assert.Empty(t, args.Flags)
}

func TestParseCommand_WithFlags(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/weather Beijing --unit celsius --days 3"}`),
	}, nil)

	args, err := ctx.ParseCommand()
	assert.NoError(t, err)
	assert.Equal(t, "/weather", args.Command)
	assert.Equal(t, []string{"Beijing"}, args.Positional)
	assert.Equal(t, "celsius", args.GetFlag("unit"))
	assert.Equal(t, "3", args.GetFlag("days"))
}

func TestParseCommand_MixedArgs(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/search golang tutorial --lang en --limit 10"}`),
	}, nil)

	args, err := ctx.ParseCommand()
	assert.NoError(t, err)
	assert.Equal(t, "/search", args.Command)
	assert.Equal(t, 2, args.Len())
	assert.Equal(t, "golang", args.Get(0))
	assert.Equal(t, "tutorial", args.Get(1))
	assert.Equal(t, "en", args.GetFlag("lang"))
	assert.Equal(t, "10", args.GetFlag("limit"))
}

func TestParseCommand_ShortFlags(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/cmd -a value1 -b value2"}`),
	}, nil)

	args, err := ctx.ParseCommand()
	assert.NoError(t, err)
	assert.Equal(t, "value1", args.GetFlag("a"))
	assert.Equal(t, "value2", args.GetFlag("b"))
}

func TestParseCommand_BoolFlags(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/cmd --verbose --debug"}`),
	}, nil)

	args, err := ctx.ParseCommand()
	assert.NoError(t, err)
	assert.True(t, args.HasFlag("verbose"))
	assert.True(t, args.HasFlag("debug"))
	assert.Equal(t, "true", args.GetFlag("verbose"))
	assert.Equal(t, "true", args.GetFlag("debug"))
	assert.True(t, args.GetFlagBool("verbose"))
}

func TestParseCommand_QuotedArgs(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/say \"hello world\" --to \"Alice Bob\""}`),
	}, nil)

	args, err := ctx.ParseCommand()
	assert.NoError(t, err)
	assert.Equal(t, "hello world", args.Get(0))
	assert.Equal(t, "Alice Bob", args.GetFlag("to"))
}

func TestParseCommand_EscapedArgs(t *testing.T) {
	t.Parallel()
	// 测试转义引号: /say "hello \"world\""
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/say \"hello \\\"world\\\"\""}`),
	}, nil)

	args, err := ctx.ParseCommand()
	assert.NoError(t, err)
	assert.Equal(t, `hello "world"`, args.Get(0))
}

func TestParseCommandLine_Direct(t *testing.T) {
	t.Parallel()
	input := "/test arg1 --flag value"
	args, err := command.ParseCommandLine(input)
	assert.NoError(t, err)
	assert.Equal(t, "/test", args.Command)
	assert.Equal(t, "arg1", args.Positional[0])
	assert.Equal(t, "value", args.Flags["flag"])
}

func TestParseCommand_UnclosedQuote(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/say \"hello world"}`),
	}, nil)

	_, err := ctx.ParseCommand()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed quote")
}

func TestParseCommand_TrailingEscape(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/say hello\\"}`),
	}, nil)

	_, err := ctx.ParseCommand()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected end of string")
}

func TestParseCommand_EmptyContent(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":""}`),
	}, nil)

	_, err := ctx.ParseCommand()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty message content")
}

func TestParseCommandLine_Tokenize_Behavior(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		cmd   string
		pos   []string
	}{
		{name: "simple", input: "hello world", cmd: "hello", pos: []string{"world"}},
		{name: "multi spaces", input: "hello  world", cmd: "hello", pos: []string{"world"}},
		{name: "double quotes", input: `hello "world test"`, cmd: "hello", pos: []string{"world test"}},
		{name: "single quotes", input: `hello 'world test'`, cmd: "hello", pos: []string{"world test"}},
		{name: "nested quotes literal", input: `hello "world 'test'" foo`, cmd: "hello", pos: []string{"world 'test'", "foo"}},
		{name: "empty", input: "", cmd: "", pos: nil},
		{name: "spaces only", input: "   ", cmd: "", pos: nil},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args, err := command.ParseCommandLine(tt.input)
			if tt.cmd == "" {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.cmd, args.Command)
			assert.Equal(t, tt.pos, args.Positional)
		})
	}
}

func TestParseCommand_ComplexExample(t *testing.T) {
	t.Parallel()
	// 模拟真实场景：天气查询命令
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/weather Beijing --unit celsius --days 7 --detailed"}`),
	}, nil)

	args, err := ctx.ParseCommand()
	assert.NoError(t, err)

	assert.Equal(t, "/weather", args.Command)

	assert.Equal(t, 1, args.Len())
	assert.Equal(t, "Beijing", args.Get(0))

	assert.Equal(t, "celsius", args.GetFlag("unit"))
	assert.Equal(t, 7, args.GetFlagIntOrDefault("days", 3))
	assert.True(t, args.HasFlag("detailed"))
	assert.True(t, args.GetFlagBool("detailed"))

	t.Logf("Parsed: %s", args.String())
}

func TestParseCommand_Caching(t *testing.T) {
	t.Parallel()
	ctx := NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/cached instruction"}`),
	}, nil)

	// First call
	args1, err := ctx.ParseCommand()
	assert.NoError(t, err)
	assert.Equal(t, "/cached", args1.Command)

	// Second call
	args2, err := ctx.ParseCommand()
	assert.NoError(t, err)

	// Verify that we got the exact same object (pointer equality)
	assert.Equal(t, args1, args2, "ParseCommand should return cached result on subsequent calls")
	assert.Same(t, args1, args2, "Pointers should be identical")
}

// NOTE: command.CommandArgs methods and ParseCommandLine behavior are tested in package command.
// Here we keep only Context.ParseCommand() behavior and caching guarantees.
