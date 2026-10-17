package engine

// registry_metadata_sync_test.go — 定义变更后命令注册表元数据同步的回归测试
//
// 回归背景：OnCommand 先把裸定义（只有名字）同步进 command.Registry，
// 随后插件通过 SetDefinition/链式元数据方法补上 Description/Usage/Aliases。
// 这些方法只刷新引擎内的 commandInfoCache（GetAllCommands/FindCommand 走它），
// 不同步 Registry（Trie + Meta，Lookup/List/Complete 走它）——两条读路径
// 出现元数据漂移：/help 显示新描述、Registry.Lookup 返回旧定义，
// 别名也只加进了 matcher 而没进 Registry 的别名表。

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	ctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_SetDefinitionSyncsCommandRegistry(t *testing.T) {
	reg := command.NewCommandRegistry()
	eng := newEngineForTest(t, WithCommandRegistry(reg))

	m := eng.OnCommand("", "/search")
	m.SetDefinition(&command.Definition{
		Name:        "search",
		Description: "搜索内容",
		Usage:       "/search <keyword>",
		Aliases:     []string{"find"},
	})

	// Registry 侧：定义与别名都可见（registry 使用纯命令名，与 trie 补全约定一致）
	meta, ok := reg.Lookup("search")
	require.True(t, ok)
	assert.Equal(t, "搜索内容", meta.Description)
	assert.Equal(t, "/search <keyword>", meta.Usage)

	aliasMeta, ok := reg.Lookup("find")
	require.True(t, ok, "别名必须进入 Registry 的别名表")
	assert.Equal(t, "search", aliasMeta.Name)
}

func TestEngine_MutateDefinitionSyncsCommandRegistry(t *testing.T) {
	reg := command.NewCommandRegistry()
	eng := newEngineForTest(t, WithCommandRegistry(reg))

	m := eng.OnCommand("", "/ping")
	m.SetDefinition(&command.Definition{Name: "ping", Description: "old"})
	m.SetDescription("new")

	meta, ok := reg.Lookup("ping")
	require.True(t, ok)
	assert.Equal(t, "new", meta.Description, "链式元数据变更必须同步到 Registry")
}

func TestEngine_SetSourceSyncsCommandRegistry(t *testing.T) {
	reg := command.NewCommandRegistry()
	eng := newEngineForTest(t, WithCommandRegistry(reg))

	m := eng.OnCommand("", "/stat")
	m.SetDefinition(&command.Definition{Name: "stat"})
	m.Handle(func(*ctx.Context) error { return nil })
	m.SetSource("plugin:metrics")

	meta, ok := reg.Lookup("stat")
	require.True(t, ok)
	assert.Equal(t, "plugin:metrics", meta.Source)
}
