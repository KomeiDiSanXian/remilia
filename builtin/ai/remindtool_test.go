package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReminderTools_SetListCancel(t *testing.T) {
	p := &Plugin{
		lifecycleCtx: context.Background(),
		reminders:    newReminderManager(),
	}
	sender := &fakeNotifierSender{}
	toolCtx := withToolSource(context.Background(), toolSource{
		userID: "u1", chatID: "chat_1", isGroup: true, sender: sender, p: p,
	})

	tools := p.buildReminderTools()
	set := findTestTool(t, tools, setReminderToolName)
	list := findTestTool(t, tools, listRemindersToolName)
	cancel := findTestTool(t, tools, cancelReminderToolName)

	// 参数缺失 / 时长非法
	_, err := set.Execute(toolCtx, map[string]any{"duration": "abc", "content": "x"})
	assert.Error(t, err)
	_, err = set.Execute(toolCtx, map[string]any{"duration": "5分钟"})
	assert.Error(t, err)

	// 正常设置
	out, err := set.Execute(toolCtx, map[string]any{"duration": "5分钟", "content": "去喝水"})
	require.NoError(t, err)
	assert.Contains(t, out, "R1")
	assert.Contains(t, out, "去喝水")

	// 同会话第二条
	out, err = set.Execute(toolCtx, map[string]any{"duration": "1小时", "content": "开会"})
	require.NoError(t, err)
	assert.Contains(t, out, "R2")

	// list
	out, err = list.Execute(toolCtx, map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "R1")
	assert.Contains(t, out, "开会")

	// 不同会话隔离：另一个会话看不到
	otherCtx := withToolSource(context.Background(), toolSource{
		userID: "u2", chatID: "chat_2", isGroup: false, sender: sender, p: p,
	})
	out, err = list.Execute(otherCtx, map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "没有活跃的提醒")

	// cancel 命中与未命中
	out, err = cancel.Execute(toolCtx, map[string]any{"id": "R1"})
	require.NoError(t, err)
	assert.Contains(t, out, "已取消")
	out, err = cancel.Execute(toolCtx, map[string]any{"id": "R99"})
	require.NoError(t, err)
	assert.Contains(t, out, "未找到")

	// 无工具源上下文
	_, err = set.Execute(context.Background(), map[string]any{"duration": "5分钟", "content": "x"})
	assert.Error(t, err)
}

func TestReminderTools_ToolSourceNoSender(t *testing.T) {
	p := &Plugin{lifecycleCtx: context.Background(), reminders: newReminderManager()}
	toolCtx := withToolSource(context.Background(), toolSource{chatID: "c", p: p}) // sender nil
	set := findTestTool(t, p.buildReminderTools(), setReminderToolName)
	_, err := set.Execute(toolCtx, map[string]any{"duration": "5分钟", "content": "x"})
	assert.Error(t, err)
}

func findTestTool(t *testing.T, tools []Tool, name string) Tool {
	t.Helper()
	for _, tl := range tools {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("tool %q not found", name)
	return Tool{}
}
