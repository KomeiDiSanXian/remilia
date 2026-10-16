package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 反斜杠转义回归测试。
//
// 回归背景：tokenize 对未定义的转义序列只输出被转义字符本身、丢掉反斜杠，
// 且未加引号时也把 `\n`、`\t`、`\r` 改写成控制字符。用户输入
// `C:\Users\name` 会被改写成 `C:Users` + 换行 + `ames`，路径、正则等
// 字面文本全部失效。

// TestTokenize_UnknownEscapeKeepsBackslash 验证未定义转义序列原样保留。
func TestTokenize_UnknownEscapeKeepsBackslash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Windows 路径（未加引号）",
			input:    `C:\Users\name`,
			expected: []string{`C:\Users\name`},
		},
		{
			name:     "Windows 路径（加引号）",
			input:    `"C:\Users\Desktop\shell"`,
			expected: []string{`C:\Users\Desktop\shell`},
		},
		{
			name:     "正则字面量",
			input:    `\d+\s+`,
			expected: []string{`\d+\s+`},
		},
		{
			name:     "未知转义混合",
			input:    `a\Ub\sc`,
			expected: []string{`a\Ub\sc`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := tokenize(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, tokens)
		})
	}
}

// TestTokenize_ControlEscapesOnlyInsideQuotes 验证控制字符转义仅在引号内生效。
func TestTokenize_ControlEscapesOnlyInsideQuotes(t *testing.T) {
	tokens, err := tokenize(`hello\nworld`)
	require.NoError(t, err)
	assert.Equal(t, []string{`hello\nworld`}, tokens)

	tokens, err = tokenize(`hello "\nworld"`)
	require.NoError(t, err)
	assert.Equal(t, []string{"hello", "\nworld"}, tokens)
}

// TestTokenize_DefinedEscapesUnchanged 验证既定转义在修复后行为不变。
func TestTokenize_DefinedEscapesUnchanged(t *testing.T) {
	tokens, err := tokenize(`hello\ world`)
	require.NoError(t, err)
	assert.Equal(t, []string{"hello world"}, tokens)

	tokens, err = tokenize(`path\\to\\file`)
	require.NoError(t, err)
	assert.Equal(t, []string{`path\to\file`}, tokens)

	tokens, err = tokenize(`say \"hi\"`)
	require.NoError(t, err)
	assert.Equal(t, []string{"say", `"hi"`}, tokens)

	tokens, err = tokenize(`it\'s`)
	require.NoError(t, err)
	assert.Equal(t, []string{"it's"}, tokens)
}

// TestParseFromDefinition_WindowsPathArgument 验证含反斜杠的路径参数
// 能端到端绑定到位置参数，不再被改写。
func TestParseFromDefinition_WindowsPathArgument(t *testing.T) {
	def := &Definition{
		Name: "save",
		Arguments: []*Argument{
			{Name: "path", Type: ArgTypeString, Required: true},
		},
	}

	parsed, err := ParseFromDefinition(`/save C:\Users\tools`, def, "/")
	require.NoError(t, err)
	assert.Equal(t, `C:\Users\tools`, parsed.GetString("path"))
}
