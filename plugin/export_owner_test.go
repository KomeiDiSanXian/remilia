package plugin

// export_owner_test.go — 容器 key 归属跟踪的回归测试
//
// 回归背景：exportAs 不做任何冲突处理，removeExportedKeys 也无条件删除本插件
// 记录过的所有 key。若插件 B 覆盖了插件 A 导出的同名 key，A 注销时会把 B 的
// 服务一起删掉（容器里 B 的引用凭空消失）。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportedKeyOwnership_SurvivesOriginalPluginUnregister(t *testing.T) {
	pm := NewManager(nil)

	require.NoError(t, pm.Register(&Descriptor{
		Name: "owner-a",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.ExportIface[any]("shared", "api-a")
			return nil, nil
		},
	}))
	require.NoError(t, pm.Register(&Descriptor{
		Name: "owner-b",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.ExportIface[any]("shared", "api-b")
			return nil, nil
		},
	}))

	got, ok := pm.container.Get("shared")
	require.True(t, ok)
	assert.Equal(t, "api-b", got, "后来者覆盖同名 key（容器语义）")

	// A 注销：key 已归属 B，不得删除
	require.NoError(t, pm.Unregister(context.Background(), "owner-a"))
	got, ok = pm.container.Get("shared")
	require.True(t, ok, "先注册者注销不得删除后来者的同名服务")
	assert.Equal(t, "api-b", got)

	// B 注销：归属仍为 B，正常释放
	require.NoError(t, pm.Unregister(context.Background(), "owner-b"))
	_, ok = pm.container.Get("shared")
	assert.False(t, ok)
}

func TestExportedKeyOwnership_OwnKeyStillRemoved(t *testing.T) {
	pm := NewManager(nil)

	require.NoError(t, pm.Register(&Descriptor{
		Name: "solo",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.ExportIface[any]("solo:extra", "extra-api")
			return "solo-api", nil
		},
	}))

	_, ok := pm.container.Get("solo:extra")
	require.True(t, ok)

	require.NoError(t, pm.Unregister(context.Background(), "solo"))
	_, ok = pm.container.Get("solo:extra")
	assert.False(t, ok, "自己导出的 key 注销时必须清理")
	_, ok = pm.container.Get("solo")
	assert.False(t, ok, "插件主 key 注销时必须清理")
}
