package ai

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/stretchr/testify/assert"
)

// TestCommandInvokerUnknownToolReturnsEmpty 校验无对应真实命令时返回空文本，
// 调用方据此回退到普通动作路径（既有回退语义）。
//
// 本用例走真实适配器（pluginCommandCatalog），因此留在装配侧；调用器自身的
// 契约用例见 builtin/ai/runtime。
func TestCommandInvokerUnknownToolReturnsEmpty(t *testing.T) {
	got := runtime.CommandInvoker{
		Catalog: pluginCommandCatalog{p: &Plugin{cmdPatterns: map[string]string{}}},
		Name:    "no_such_command",
		Args:    map[string]any{},
		CS:      &execution.CaptureSender{},
	}.Invoke(context.Background())

	assert.Empty(t, got.Text)
	assert.NoError(t, got.Err)
}
