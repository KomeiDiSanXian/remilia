package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 子命令层级参数归属回归测试。
//
// 回归背景：buildParsedFromLevels 只绑定叶子层级的 flag 与位置参数，
// 出现在子命令 token 之前的父层 flag/参数会被静默丢弃：
// `/debug --verbose runtime` 中 --verbose 既不出现在 Parsed.Flags 里，
// 也不会报错，调用方完全拿不到用户输入。

// TestParseFromDefinition_ParentFlagVisible 验证父层 flag 能被绑定读取。
func TestParseFromDefinition_ParentFlagVisible(t *testing.T) {
	def := &Definition{
		Name: "debug",
		Flags: []*Flag{
			{Name: "verbose", ShortName: "v", Type: ArgTypeBool},
		},
		SubCommands: []*Definition{{
			Name: "runtime",
			Flags: []*Flag{
				{Name: "output", ShortName: "o", Type: ArgTypeString},
			},
		}},
	}

	parsed, err := ParseFromDefinition("/debug --verbose runtime --output json", def, "/")
	require.NoError(t, err)
	assert.Equal(t, []string{"debug", "runtime"}, parsed.CommandPath)
	assert.True(t, parsed.GetBool("verbose"))
	assert.Equal(t, "json", parsed.GetString("output"))

	parsed, err = ParseFromDefinition("/debug -v runtime", def, "/")
	require.NoError(t, err)
	assert.True(t, parsed.GetBool("verbose"))
}

// TestParseFromDefinition_ParentRequiredNotEnforcedForSubcommand 验证
// 父层必填 flag 只在父层实际执行（无子命令）时校验：子命令不执行父层逻辑，
// `/auth status` 不应因为 `/auth` 声明了必填 --token 而失败。
func TestParseFromDefinition_ParentRequiredNotEnforcedForSubcommand(t *testing.T) {
	def := &Definition{
		Name: "auth",
		Flags: []*Flag{
			{Name: "token", Type: ArgTypeString, Required: true},
		},
		SubCommands: []*Definition{{Name: "status"}},
	}

	parsed, err := ParseFromDefinition("/auth status", def, "/")
	require.NoError(t, err)
	assert.Equal(t, []string{"auth", "status"}, parsed.CommandPath)

	// 提供时正常绑定
	parsed, err = ParseFromDefinition("/auth --token abc status", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "abc", parsed.GetString("token"))

	// 父层自身作为叶子执行时必填校验保持不变
	_, err = ParseFromDefinition("/auth", def, "/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag --token is missing")
}

// TestParseFromDefinition_ParentPositionalBound 验证父层位置参数能被绑定读取。
func TestParseFromDefinition_ParentPositionalBound(t *testing.T) {
	def := &Definition{
		Name: "deploy",
		Arguments: []*Argument{
			{Name: "env", Type: ArgTypeString, Required: true},
		},
		SubCommands: []*Definition{{Name: "status"}},
	}

	parsed, err := ParseFromDefinition("/deploy prod status", def, "/")
	require.NoError(t, err)
	assert.Equal(t, []string{"deploy", "status"}, parsed.CommandPath)
	assert.Equal(t, "prod", parsed.GetString("env"))

	// 父层参数缺失时不因父层必填而失败（与 flag 同规则）
	parsed, err = ParseFromDefinition("/deploy status", def, "/")
	require.NoError(t, err)
	assert.Equal(t, []string{"deploy", "status"}, parsed.CommandPath)
}

// TestParseFromDefinition_LeafRequiredStillEnforced 验证叶子层必填校验未被削弱。
func TestParseFromDefinition_LeafRequiredStillEnforced(t *testing.T) {
	def := &Definition{
		Name: "job",
		SubCommands: []*Definition{{
			Name: "run",
			Arguments: []*Argument{
				{Name: "script", Type: ArgTypeString, Required: true},
			},
		}},
	}

	_, err := ParseFromDefinition("/job run", def, "/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required argument script is missing")
}

// TestParseFromDefinition_SameNameFlagKeepsExplicitValue 验证父子层级
// 声明同名 flag 时，用户显式提供的值不会被默认值覆盖。
func TestParseFromDefinition_SameNameFlagKeepsExplicitValue(t *testing.T) {
	def := &Definition{
		Name: "cmd",
		Flags: []*Flag{
			{Name: "mode", Type: ArgTypeString, Default: "fast"},
		},
		SubCommands: []*Definition{{
			Name: "sub",
			Flags: []*Flag{
				{Name: "mode", Type: ArgTypeString, Default: "slow"},
			},
		}},
	}

	parsed, err := ParseFromDefinition("/cmd --mode turbo sub", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "turbo", parsed.GetString("mode"))

	parsed, err = ParseFromDefinition("/cmd sub --mode careful", def, "/")
	require.NoError(t, err)
	assert.Equal(t, "careful", parsed.GetString("mode"))
}
