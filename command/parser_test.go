package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenize 测试分词函数
func TestTokenize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		wantErr  bool
	}{
		{
			name:     "simple tokens",
			input:    "hello world test",
			expected: []string{"hello", "world", "test"},
			wantErr:  false,
		},
		{
			name:     "quoted string with double quotes",
			input:    `hello "world test" foo`,
			expected: []string{"hello", "world test", "foo"},
			wantErr:  false,
		},
		{
			name:     "quoted string with single quotes",
			input:    `hello 'world test' foo`,
			expected: []string{"hello", "world test", "foo"},
			wantErr:  false,
		},
		{
			name:     "escaped characters",
			input:    `hello\ world test`,
			expected: []string{"hello world", "test"},
			wantErr:  false,
		},
		{
			name:     "escaped quote",
			input:    `hello \"world\" test`,
			expected: []string{"hello", `"world"`, "test"},
			wantErr:  false,
		},
		{
			name:     "mixed quotes and escapes",
			input:    `cmd "arg with spaces" 'another arg' normal\ arg`,
			expected: []string{"cmd", "arg with spaces", "another arg", "normal arg"},
			wantErr:  false,
		},
		{
			name:     "multiple spaces",
			input:    "hello    world     test",
			expected: []string{"hello", "world", "test"},
			wantErr:  false,
		},
		{
			name:     "tabs and newlines",
			input:    "hello\tworld\ntest",
			expected: []string{"hello", "world", "test"},
			wantErr:  false,
		},
		{
			name:     "unclosed double quote",
			input:    `hello "world`,
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "unclosed single quote",
			input:    `hello 'world`,
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "escape at end",
			input:    `hello world\`,
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
			wantErr:  false,
		},
		{
			name:     "only spaces",
			input:    "   ",
			expected: []string{},
			wantErr:  false,
		},
		{
			name:     "quote inside different quote type",
			input:    `"it's working" and 'he said "hello"'`,
			expected: []string{"it's working", "and", `he said "hello"`},
			wantErr:  false,
		},
		{
			name:     "special escape sequences",
			input:    `hello\nworld test\ttab`,
			expected: []string{"hello\nworld", "test\ttab"},
			wantErr:  false,
		},
		{
			name:     "escaped backslash",
			input:    `path\\to\\file`,
			expected: []string{`path\to\file`},
			wantErr:  false,
		},
		{
			name:     "carriage return escape",
			input:    `line1\rline2`,
			expected: []string{"line1\rline2"},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tokenize(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseCommandLine 测试命令行解析
func TestParseCommandLine(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCmd   string
		expectedPos   []string
		expectedFlags map[string]string
		wantErr       bool
	}{
		{
			name:          "simple command",
			input:         "/weather Beijing",
			expectedCmd:   "/weather",
			expectedPos:   []string{"Beijing"},
			expectedFlags: map[string]string{},
			wantErr:       false,
		},
		{
			name:          "command with long flags",
			input:         "/weather Beijing --unit celsius --days 3",
			expectedCmd:   "/weather",
			expectedPos:   []string{"Beijing"},
			expectedFlags: map[string]string{"unit": "celsius", "days": "3"},
			wantErr:       false,
		},
		{
			name:          "command with short flags",
			input:         "/search golang -l 20 -v",
			expectedCmd:   "/search",
			expectedPos:   []string{"golang"},
			expectedFlags: map[string]string{"l": "20", "v": "true"},
			wantErr:       false,
		},
		{
			name:          "boolean flag without value",
			input:         "/git commit --amend",
			expectedCmd:   "/git",
			expectedPos:   []string{"commit"},
			expectedFlags: map[string]string{"amend": "true"},
			wantErr:       false,
		},
		{
			name:          "multiple positional args",
			input:         "/copy source.txt dest.txt backup.txt",
			expectedCmd:   "/copy",
			expectedPos:   []string{"source.txt", "dest.txt", "backup.txt"},
			expectedFlags: map[string]string{},
			wantErr:       false,
		},
		{
			name:          "quoted arguments",
			input:         `/echo "hello world" --prefix ">>>"`,
			expectedCmd:   "/echo",
			expectedPos:   []string{"hello world"},
			expectedFlags: map[string]string{"prefix": ">>>"},
			wantErr:       false,
		},
		{
			name:          "mixed positional and flags",
			input:         "/search keyword --limit 10 category --verbose",
			expectedCmd:   "/search",
			expectedPos:   []string{"keyword", "category"},
			expectedFlags: map[string]string{"limit": "10", "verbose": "true"},
			wantErr:       false,
		},
		{
			// POSIX "--" terminates flag parsing; remaining tokens (none here) are
			// positional.  This is valid, not an error.
			name:          "double-dash end-of-flags sentinel",
			input:         "/cmd --",
			expectedCmd:   "/cmd",
			expectedPos:   []string{},
			expectedFlags: map[string]string{},
			wantErr:       false,
		},
		{
			name:          "no tokens",
			input:         "",
			expectedCmd:   "",
			expectedPos:   nil,
			expectedFlags: nil,
			wantErr:       true,
		},
		{
			name:          "command only",
			input:         "/help",
			expectedCmd:   "/help",
			expectedPos:   []string{},
			expectedFlags: map[string]string{},
			wantErr:       false,
		},
		{
			name:          "negative number as flag value",
			input:         "/weather --days -1 --temp -5",
			expectedCmd:   "/weather",
			expectedPos:   []string{},
			expectedFlags: map[string]string{"days": "-1", "temp": "-5"},
			wantErr:       false,
		},
		{
			name:          "value starting with dash",
			input:         "/search --pattern -foo --exclude -bar",
			expectedCmd:   "/search",
			expectedPos:   []string{},
			expectedFlags: map[string]string{"pattern": "-foo", "exclude": "-bar"},
			wantErr:       false,
		},
		{
			name:          "equals sign syntax",
			input:         "/config --key=value --number=-42",
			expectedCmd:   "/config",
			expectedPos:   []string{},
			expectedFlags: map[string]string{"key": "value", "number": "-42"},
			wantErr:       false,
		},
		{
			name:          "short flag with negative value",
			input:         "/calc -n -10 -x -20",
			expectedCmd:   "/calc",
			expectedPos:   []string{},
			expectedFlags: map[string]string{"n": "-10", "x": "-20"},
			wantErr:       false,
		},
		{
			name:          "multiple short flags without values",
			input:         "/docker run nginx -d -p",
			expectedCmd:   "/docker",
			expectedPos:   []string{"run", "nginx"},
			expectedFlags: map[string]string{"d": "true", "p": "true"},
			wantErr:       false,
		},
		{
			name:          "mixed short flags and values",
			input:         "/cmd -v -o output.txt -f",
			expectedCmd:   "/cmd",
			expectedPos:   []string{},
			expectedFlags: map[string]string{"v": "true", "o": "output.txt", "f": "true"},
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseCommandLine(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCmd, result.Command)
			assert.Equal(t, tt.expectedPos, result.Positional)
			assert.Equal(t, tt.expectedFlags, result.Flags)
			assert.Equal(t, tt.input, result.Raw)
		})
	}
}

// TestArgs_Get 测试获取位置参数
func TestArgs_Get(t *testing.T) {
	args, err := ParseCommandLine("/cmd arg0 arg1 arg2")
	require.NoError(t, err)

	assert.Equal(t, "arg0", args.Get(0))
	assert.Equal(t, "arg1", args.Get(1))
	assert.Equal(t, "arg2", args.Get(2))
	assert.Equal(t, "", args.Get(3))  // out of bounds
	assert.Equal(t, "", args.Get(-1)) // negative index
}

// TestArgs_GetFlag 测试获取标志
func TestArgs_GetFlag(t *testing.T) {
	args, err := ParseCommandLine("/cmd --key1 value1 --key2 value2")
	require.NoError(t, err)

	assert.Equal(t, "value1", args.GetFlag("key1"))
	assert.Equal(t, "value2", args.GetFlag("key2"))
	assert.Equal(t, "", args.GetFlag("nonexistent"))
}

// TestArgs_GetFlagOrDefault 测试获取标志或默认值
func TestArgs_GetFlagOrDefault(t *testing.T) {
	args, err := ParseCommandLine("/cmd --key value")
	require.NoError(t, err)

	assert.Equal(t, "value", args.GetFlagOrDefault("key", "default"))
	assert.Equal(t, "default", args.GetFlagOrDefault("nonexistent", "default"))
}

// TestArgs_HasFlag 测试是否存在标志
func TestArgs_HasFlag(t *testing.T) {
	args, err := ParseCommandLine("/cmd --flag1 --flag2 value")
	require.NoError(t, err)

	assert.True(t, args.HasFlag("flag1"))
	assert.True(t, args.HasFlag("flag2"))
	assert.False(t, args.HasFlag("flag3"))
}

// TestArgs_GetInt 测试获取整型位置参数
func TestArgs_GetInt(t *testing.T) {
	args, err := ParseCommandLine("/cmd 42 abc -10")
	require.NoError(t, err)

	val, err := args.GetInt(0)
	assert.NoError(t, err)
	assert.Equal(t, 42, val)

	val, err = args.GetInt(2)
	assert.NoError(t, err)
	assert.Equal(t, -10, val)

	_, err = args.GetInt(1) // "abc" is not an int
	assert.Error(t, err)

	_, err = args.GetInt(10) // out of bounds
	assert.Error(t, err)
}

// TestArgs_GetFlagInt 测试获取整型标志
func TestArgs_GetFlagInt(t *testing.T) {
	args, err := ParseCommandLine("/cmd --port 8080 --invalid abc")
	require.NoError(t, err)

	val, err := args.GetFlagInt("port")
	assert.NoError(t, err)
	assert.Equal(t, 8080, val)

	_, err = args.GetFlagInt("invalid")
	assert.Error(t, err)

	_, err = args.GetFlagInt("nonexistent")
	assert.Error(t, err)
}

// TestArgs_GetIntOrDefault 测试获取整型或默认值
func TestArgs_GetIntOrDefault(t *testing.T) {
	args, err := ParseCommandLine("/cmd 100 abc")
	require.NoError(t, err)

	assert.Equal(t, 100, args.GetIntOrDefault(0, 999))
	assert.Equal(t, 999, args.GetIntOrDefault(1, 999))  // invalid int
	assert.Equal(t, 999, args.GetIntOrDefault(10, 999)) // out of bounds
}

// TestArgs_GetFlagIntOrDefault 测试获取整型标志或默认值
func TestArgs_GetFlagIntOrDefault(t *testing.T) {
	args, err := ParseCommandLine("/cmd --count 50 --invalid text")
	require.NoError(t, err)

	assert.Equal(t, 50, args.GetFlagIntOrDefault("count", 100))
	assert.Equal(t, 100, args.GetFlagIntOrDefault("invalid", 100))
	assert.Equal(t, 100, args.GetFlagIntOrDefault("nonexistent", 100))
}

// TestArgs_GetBool 测试获取布尔型位置参数
func TestArgs_GetBool(t *testing.T) {
	args, err := ParseCommandLine("/cmd true false 1 0 abc")
	require.NoError(t, err)

	val, err := args.GetBool(0)
	assert.NoError(t, err)
	assert.True(t, val)

	val, err = args.GetBool(1)
	assert.NoError(t, err)
	assert.False(t, val)

	val, err = args.GetBool(2)
	assert.NoError(t, err)
	assert.True(t, val)

	val, err = args.GetBool(3)
	assert.NoError(t, err)
	assert.False(t, val)

	_, err = args.GetBool(4) // "abc" is not a bool
	assert.Error(t, err)

	_, err = args.GetBool(10) // out of bounds
	assert.Error(t, err)
}

// TestArgs_GetFlagBool 测试获取布尔型标志
func TestArgs_GetFlagBool(t *testing.T) {
	args, err := ParseCommandLine("/cmd --verbose true --debug false --enable")
	require.NoError(t, err)

	assert.True(t, args.GetFlagBool("verbose"))
	assert.False(t, args.GetFlagBool("debug"))
	assert.True(t, args.GetFlagBool("enable"))
	assert.False(t, args.GetFlagBool("nonexistent"))
}

// TestArgs_Len 测试参数长度
func TestArgs_Len(t *testing.T) {
	args, err := ParseCommandLine("/cmd a b c")
	require.NoError(t, err)
	assert.Equal(t, 3, args.Len())

	args, err = ParseCommandLine("/cmd")
	require.NoError(t, err)
	assert.Equal(t, 0, args.Len())
}

// TestArgs_String 测试字符串表示
func TestArgs_String(t *testing.T) {
	input := "/cmd arg1 --flag value"
	args, err := ParseCommandLine(input)
	require.NoError(t, err)
	assert.Equal(t, input, args.String())
}
