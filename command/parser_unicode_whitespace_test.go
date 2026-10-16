package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unicode 空白分隔回归测试。
//
// 回归背景：tokenize 与命令提取辅助函数以前只把 ' '、'\t'、'\n' 当分隔符，
// 而引擎侧提取命令词使用 unicode.IsSpace。两者不一致时，含全角空格
// （U+3000）等 Unicode 空白的输入会让命令词与参数粘成一个 token：
// `/cmd　参数` 解析整体失败 → OnParseCommand 规则不匹配 → 整条消息被静默丢弃。

// TestParseCommandLine_UnicodeWhitespace 验证 Unicode 空白与普通空格一样分隔 token。
func TestParseCommandLine_UnicodeWhitespace(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCmd   string
		expectedPos   []string
		expectedFlags map[string]string
	}{
		{
			name:          "全角空格分隔命令与参数",
			input:         "/cmd\u3000arg1\u3000arg2",
			expectedCmd:   "/cmd",
			expectedPos:   []string{"arg1", "arg2"},
			expectedFlags: map[string]string{},
		},
		{
			name:          "全角空格分隔标志与值",
			input:         "/cmd\u3000--unit\u3000celsius",
			expectedCmd:   "/cmd",
			expectedPos:   []string{},
			expectedFlags: map[string]string{"unit": "celsius"},
		},
		{
			name:          "不间断空格",
			input:         "/cmd\u00a0arg1",
			expectedCmd:   "/cmd",
			expectedPos:   []string{"arg1"},
			expectedFlags: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := ParseCommandLine(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCmd, args.Command)
			assert.Equal(t, tt.expectedPos, args.Positional)
			assert.Equal(t, tt.expectedFlags, args.Flags)
		})
	}
}

// TestParseCommandLine_UnicodeWhitespaceInsideQuotes 验证引号内的
// Unicode 空白仍按原样保留，不会被当作分隔符。
func TestParseCommandLine_UnicodeWhitespaceInsideQuotes(t *testing.T) {
	args, err := ParseCommandLine("/cmd \"a\u3000b\"")
	require.NoError(t, err)
	require.Len(t, args.Positional, 1)
	assert.Equal(t, "a\u3000b", args.Positional[0])
}

// TestParseFromDefinition_FullWidthSpace 验证含全角空格的命令能被端到端解析。
//
// 这是最初暴露问题的场景：引擎按 unicode.IsSpace 提取命令词，
// 解析层必须按同一套空白规则分词，否则子命令/参数整体失效。
func TestParseFromDefinition_FullWidthSpace(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Arguments: []*Argument{
			{Name: "target", Type: ArgTypeString, Required: true},
		},
	}

	parsed, err := ParseFromDefinition("/cmd\u3000hello", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "hello", parsed.GetString("target"))
	assert.Equal(t, []string{"cmd"}, parsed.CommandPath)
}

// TestExtractCommandHelpers_UnicodeWhitespace 验证命令提取辅助函数
// 与解析器、引擎使用同一套空白判定。
func TestExtractCommandHelpers_UnicodeWhitespace(t *testing.T) {
	assert.Equal(t, "/cmd", ExtractCommandFast("/cmd\u3000arg", "/"))
	assert.Equal(t, "/cmd", ExtractCommandFast("/cmd\u00a0arg", "/"))

	cmd, args := ExtractCommandAndArgs("/cmd\u3000arg1\u3000arg2", "/")
	assert.Equal(t, "/cmd", cmd)
	assert.Equal(t, "arg1\u3000arg2", args)
}
