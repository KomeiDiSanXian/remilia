package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 负数位置参数回归测试。
//
// 回归背景：ParseCommandLine 曾用 `strings.HasPrefix(token, "-") && len(token) == 2`
// 判定短标志，于是 "-1"、"−3.14" 这类负数被当作标志：`/roll -1 100` 解析成
// Flags={"1": "100"}，位置参数为空，命令读不到任何参数。

// TestParseCommandLine_NegativeNumberIsPositional 验证负数不会被当成短标志。
func TestParseCommandLine_NegativeNumberIsPositional(t *testing.T) {
	args, err := ParseCommandLine("/roll -1 100")
	require.NoError(t, err)
	assert.Equal(t, "/roll", args.Command)
	assert.Equal(t, []string{"-1", "100"}, args.Positional)
	assert.Empty(t, args.Flags)
}

// TestParseCommandLine_DecimalNegativeIsPositional 验证小数负数同样保留为位置参数。
func TestParseCommandLine_DecimalNegativeIsPositional(t *testing.T) {
	args, err := ParseCommandLine("/offset -3.14")
	require.NoError(t, err)
	assert.Equal(t, []string{"-3.14"}, args.Positional)
	assert.Empty(t, args.Flags)
}

// TestParseCommandLine_ShortFlagStillParsed 验证修复没有破坏正常的短标志解析。
func TestParseCommandLine_ShortFlagStillParsed(t *testing.T) {
	args, err := ParseCommandLine("/cmd -n 5")
	require.NoError(t, err)
	assert.Equal(t, "5", args.Flags["n"])
	assert.Empty(t, args.Positional)

	args, err = ParseCommandLine("/cmd -v")
	require.NoError(t, err)
	assert.Equal(t, "true", args.Flags["v"])
	assert.Empty(t, args.Positional)
}

// TestParseCommandLine_NegativeFlagValue 验证负数依旧可以作为标志的值。
func TestParseCommandLine_NegativeFlagValue(t *testing.T) {
	args, err := ParseCommandLine("/cmd --delta -42")
	require.NoError(t, err)
	assert.Equal(t, "-42", args.Flags["delta"])
	assert.Empty(t, args.Positional)

	args, err = ParseCommandLine("/cmd --delta=-42")
	require.NoError(t, err)
	assert.Equal(t, "-42", args.Flags["delta"])
}

// TestParseFromDefinition_NegativeNumberArgs 验证含负数的命令能端到端绑定到整型参数。
func TestParseFromDefinition_NegativeNumberArgs(t *testing.T) {
	def := &Definition{
		Name: "roll",
		Arguments: []*Argument{
			{Name: "min", Type: ArgTypeInt, Required: true},
			{Name: "max", Type: ArgTypeInt, Required: true},
		},
	}

	parsed, err := ParseFromDefinition("/roll -1 100", def, "/")
	require.NoError(t, err)
	assert.Equal(t, -1, parsed.GetInt("min"))
	assert.Equal(t, 100, parsed.GetInt("max"))
}
