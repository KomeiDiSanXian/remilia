package sauce

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSauceCmdCtx 构造事件内容为完整命令文本的 Context（供 ParseCommand 从 Positional 解析）。
func newSauceCmdCtx(content string) *eventctx.Context {
	evt := &replyQuoteEvent{
		segments: []platform.Segment{{Type: platform.SegmentText, Text: content}},
	}
	return eventctx.NewContextFromEvent(evt, mock.NewSender())
}

// TestResolveEnginesFromCommand 验证 -engine 参数能被正确解析。
//
// 回归：框架解析器只识别 --engine（双横线）与单字符短标志；
// 单横线多字符 -engine 会落入 Positional，此前只查增强解析 Flags
// 导致参数永远读不到、回退全部引擎。
func TestResolveEnginesFromCommand(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"saucenao_api_key": "key"}}}

	parse := func(content string) (engineSet, error) {
		// 模拟 OnParseCommand(def) 注入的增强解析结果（真实流程中由规则设置）
		def := command.NewDef("sauce").
			Flag("engine", "", "引擎", command.ArgTypeString).
			Build()
		parsed, perr := command.ParseFromDefinition(content, def, "/")
		require.NoError(t, perr)
		ctx := newSauceCmdCtx(content)
		ctx.SetParsedCommand(parsed)
		return p.resolveEnginesFromCommand(ctx)
	}

	t.Run("single engine", func(t *testing.T) {
		s, err := parse("/sauce -engine iqdb")
		require.NoError(t, err)
		assert.Len(t, s, 1)
		assert.True(t, s["iqdb"])
		assert.False(t, s["tracemoe"])
		assert.False(t, s["animetrace"])
	})

	t.Run("multiple engines comma", func(t *testing.T) {
		s, err := parse("/sauce -engine tracemoe,animetrace")
		require.NoError(t, err)
		assert.Len(t, s, 2)
		assert.True(t, s["tracemoe"])
		assert.True(t, s["animetrace"])
	})

	t.Run("no flag defaults to all", func(t *testing.T) {
		s, err := parse("/sauce")
		require.NoError(t, err)
		assert.True(t, s["iqdb"])
		assert.True(t, s["tracemoe"])
		assert.True(t, s["saucenao"])
	})

	t.Run("double dash form works too", func(t *testing.T) {
		s, err := parse("/sauce --engine animetrace")
		require.NoError(t, err)
		assert.Len(t, s, 1)
		assert.True(t, s["animetrace"])
	})

	t.Run("short flag -e works via enhanced parser", func(t *testing.T) {
		// Flag 定义了 shortName "e"，增强解析器能原生识别 -e <value>
		def := command.NewDef("sauce").
			Flag("engine", "e", "引擎", command.ArgTypeString).
			Build()
		parsed, perr := command.ParseFromDefinition("/sauce -e iqdb", def, "/")
		require.NoError(t, perr)
		assert.Equal(t, "iqdb", parsed.GetString("engine"))

		ctx := newSauceCmdCtx("/sauce -e iqdb")
		ctx.SetParsedCommand(parsed)
		s, err := p.resolveEnginesFromCommand(ctx)
		require.NoError(t, err)
		assert.Len(t, s, 1)
		assert.True(t, s["iqdb"])
	})

	t.Run("engine flag with attached value", func(t *testing.T) {
		s, err := parse("/sauce -engine=iqdb")
		require.NoError(t, err)
		assert.True(t, s["iqdb"])
	})

	t.Run("engine tasks only submit selected", func(t *testing.T) {
		s, err := parse("/sauce -engine iqdb")
		require.NoError(t, err)
		tasks := p.engineTasks(context.TODO(), engineInput{}, 3, s)
		require.Len(t, tasks, 1)
		assert.Equal(t, "IQDB", tasks[0].name)
	})
}
