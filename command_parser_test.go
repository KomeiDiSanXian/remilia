package remilia

import (
	"testing"

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

	// 测试转义空格: /say hello\ world
	ctx = NewContext(&dto.Payload{
		Detail: []byte(`{"content":"/say hello\\ world"}`),
	}, nil)
	args, err = ctx.ParseCommand()
	assert.NoError(t, err)
	assert.Equal(t, "hello world", args.Get(0))
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

func TestCommandArgs_Get(t *testing.T) {
	t.Parallel()
	args := &CommandArgs{
		Positional: []string{"a", "b", "c"},
	}

	assert.Equal(t, "a", args.Get(0))
	assert.Equal(t, "b", args.Get(1))
	assert.Equal(t, "c", args.Get(2))
	assert.Equal(t, "", args.Get(3))  // 瓒呭嚭鑼冨洿
	assert.Equal(t, "", args.Get(-1)) // 璐熸暟绱㈠紩
}

func TestCommandArgs_GetInt(t *testing.T) {
	t.Parallel()
	args := &CommandArgs{
		Positional: []string{"123", "abc", "456"},
	}

	val, err := args.GetInt(0)
	assert.NoError(t, err)
	assert.Equal(t, 123, val)

	_, err = args.GetInt(1)
	assert.Error(t, err) // 涓嶆槸鏁板瓧

	val = args.GetIntOrDefault(1, 999)
	assert.Equal(t, 999, val)

	val = args.GetIntOrDefault(2, 999)
	assert.Equal(t, 456, val)
}

func TestCommandArgs_GetFlagInt(t *testing.T) {
	t.Parallel()
	args := &CommandArgs{
		Flags: map[string]string{
			"count":   "42",
			"invalid": "abc",
		},
	}

	val, err := args.GetFlagInt("count")
	assert.NoError(t, err)
	assert.Equal(t, 42, val)

	_, err = args.GetFlagInt("invalid")
	assert.Error(t, err)

	val = args.GetFlagIntOrDefault("missing", 100)
	assert.Equal(t, 100, val)

	val = args.GetFlagIntOrDefault("count", 100)
	assert.Equal(t, 42, val)
}

func TestCommandArgs_GetFlagOrDefault(t *testing.T) {
	t.Parallel()
	args := &CommandArgs{
		Flags: map[string]string{
			"key1": "value1",
		},
	}

	assert.Equal(t, "value1", args.GetFlagOrDefault("key1", "default"))
	assert.Equal(t, "default", args.GetFlagOrDefault("key2", "default"))
}

func TestCommandArgs_HasFlag(t *testing.T) {
	t.Parallel()
	args := &CommandArgs{
		Flags: map[string]string{
			"verbose": "true",
		},
	}

	assert.True(t, args.HasFlag("verbose"))
	assert.False(t, args.HasFlag("debug"))
}

func TestCommandArgs_Len(t *testing.T) {
	t.Parallel()
	args := &CommandArgs{
		Positional: []string{"a", "b", "c"},
	}

	assert.Equal(t, 3, args.Len())
}

func TestCommandArgs_String(t *testing.T) {
	t.Parallel()
	args := &CommandArgs{
		Command:    "/test",
		Positional: []string{"arg1"},
		Flags:      map[string]string{"key": "value"},
	}

	str := args.String()
	assert.Contains(t, str, "/test")
	assert.Contains(t, str, "arg1")
	assert.Contains(t, str, "key")
	assert.Contains(t, str, "value")
}

func TestTokenize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  string
		expect []string
	}{
		{
			input:  "hello world",
			expect: []string{"hello", "world"},
		},
		{
			input:  "hello  world",
			expect: []string{"hello", "world"}, // 澶氫釜绌烘牸
		},
		{
			input:  `hello "world test"`,
			expect: []string{"hello", "world test"},
		},
		{
			input:  `hello 'world test'`,
			expect: []string{"hello", "world test"},
		},
		{
			input:  `hello "world 'test'" foo`,
			expect: []string{"hello", "world 'test'", "foo"},
		},
		{
			input:  "",
			expect: []string{},
		},
		{
			input:  "   ",
			expect: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := tokenize(tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.expect, result)
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
