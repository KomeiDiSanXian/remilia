package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPluginCommandCatalogSourceResolution 校验命令目录适配器的来源解析契约。
//
// "这个动作名有没有真实命令"是装配侧（executeToolResult）的判定：未登记命令
// 模式的动作不被 Pattern 命中，据此把控制权交回动作自身的 Execute；一旦命中，
// 结果就完全由命令通道负责。因此 Run 在无命令模式时必须报错而不是静默成功——
// 否则会退化成"占位 Execute 冒充成功"。
//
// 调用器自身的契约用例见 builtin/ai/runtime。
func TestPluginCommandCatalogSourceResolution(t *testing.T) {
	c := pluginCommandCatalog{p: &Plugin{cmdPatterns: map[string]string{}}}

	_, ok := c.Pattern("no_such_command")
	assert.False(t, ok, "未登记命令模式的动作不得被 Pattern 命中")

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)
	text, err := c.Run(ctx, "no_such_command", map[string]any{}, &execution.CaptureSender{})
	assert.Empty(t, text)
	require.Error(t, err, "未登记命令模式时 Run 必须报错，不得静默返回成功")
}
