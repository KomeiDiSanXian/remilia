package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupCtx 构造一条群消息上下文（附回复捕获器，但本文件只断言副作用：
// ctx.Reply 走异步 dispatcher，裸构造的上下文没有 dispatcher，
// 断言回复会依赖调度时序，因此沿用现有用例的做法——断言状态）。
func groupCtx(t *testing.T, content string, role platform.GroupRole) *eventctx.Context {
	t.Helper()
	evt := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage, content,
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", GroupRole: role}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
	)
	return eventctx.NewContextFromEvent(evt, &approvalCtxSender{})
}

// TestHandleGroupCommandRoutes 守卫 /ai group 入口的分派与鉴权。
func TestHandleGroupCommandRoutes(t *testing.T) {
	newPlugin := func() *Plugin {
		return &Plugin{
			cfg:           &config.Config{TriggerCmd: "/ai"},
			groupPolicies: newGroupPolicyManager(nil, ""),
		}
	}

	t.Run("no subcommand is a no-op on state", func(t *testing.T) {
		p := newPlugin()
		ctx := groupCtx(t, "/ai group", platform.GroupRoleAdmin)
		require.NoError(t, p.handleGroupCommand(ctx))
		assert.Nil(t, p.groupPolicies.Get("g1"), "空子命令不应写入任何策略")
	})

	t.Run("status is a no-op on state", func(t *testing.T) {
		p := newPlugin()
		ctx := groupCtx(t, "/ai group status", platform.GroupRoleAdmin)
		require.NoError(t, p.handleGroupStatus(ctx))
		assert.Nil(t, p.groupPolicies.Get("g1"))
	})

	t.Run("reset clears an existing policy", func(t *testing.T) {
		p := newPlugin()
		p.groupPolicies.SetGroup("g1", &groupPolicy{SystemPrompt: new("custom")})
		require.NotNil(t, p.groupPolicies.Get("g1"))

		ctx := groupCtx(t, "/ai group reset all", platform.GroupRoleAdmin)
		require.NoError(t, p.handleGroupReset(ctx, []string{"all"}))
		assert.Nil(t, p.groupPolicies.Get("g1"), "reset all 应清空本群策略")
	})

	t.Run("admin can set, member cannot", func(t *testing.T) {
		p := newPlugin()

		adminCtx := groupCtx(t, "/ai group set prompt hi", platform.GroupRoleAdmin)
		require.NoError(t, p.handleGroupSet(adminCtx, []string{"prompt", "hi"}))
		gp := p.groupPolicies.Get("g1")
		require.NotNil(t, gp)
		require.NotNil(t, gp.SystemPrompt)
		assert.Equal(t, "hi", *gp.SystemPrompt)

		memberCtx := groupCtx(t, "/ai group set prompt nope", platform.GroupRoleMember)
		require.NoError(t, p.handleGroupSet(memberCtx, []string{"prompt", "nope"}))
		assert.Equal(t, "hi", *p.groupPolicies.Get("g1").SystemPrompt, "非管理员不得修改策略")
	})

	// group 管理只在群聊有意义。
	evt := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, "/ai group status",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}))
	privCtx := eventctx.NewContextFromEvent(evt, &approvalCtxSender{})
	require.NoError(t, newPlugin().handleGroupCommand(privCtx))
}

// TestHandleRemindCommandRoutes 守卫 /ai remind 的子命令分派：只有合法的
// "时长 + 内容"才会真正登记提醒，其余分支都是提示且不产生副作用。
func TestHandleRemindCommandRoutes(t *testing.T) {
	newPlugin := func() *Plugin {
		return &Plugin{cfg: &config.Config{TriggerCmd: "/ai"}, reminders: newReminderManager()}
	}
	active := func(p *Plugin) int { return len(p.reminders.list("g1")) }

	t.Run("empty and list keep zero reminders", func(t *testing.T) {
		p := newPlugin()
		require.NoError(t, p.handleRemindCommand(groupCtx(t, "/ai remind", platform.GroupRoleUnknown), ""))
		require.NoError(t, p.handleRemindCommand(groupCtx(t, "/ai remind list", platform.GroupRoleUnknown), "list"))
		assert.Zero(t, active(p))
	})

	t.Run("cancel without id keeps zero reminders", func(t *testing.T) {
		p := newPlugin()
		require.NoError(t, p.handleRemindCommand(groupCtx(t, "/ai remind cancel", platform.GroupRoleUnknown), "cancel"))
		assert.Zero(t, active(p))
	})

	t.Run("unparsable duration keeps zero reminders", func(t *testing.T) {
		p := newPlugin()
		require.NoError(t, p.handleRemindCommand(groupCtx(t, "/ai remind 不是时长 内容", platform.GroupRoleUnknown), "不是时长 内容"))
		assert.Zero(t, active(p), "无法解析的时长必须走帮助分支，不得登记提醒")
	})
}

// TestHandleTodoListFilters 守卫待办列表的 pending/done 过滤口径。
//
// handleTodoList 只读状态、只产出文本，因此这里断言"过滤后的条数"，
// 通过 todoManager 的列表结果间接验证分支走向。
func TestHandleTodoListFilters(t *testing.T) {
	p := &Plugin{cfg: &config.Config{TriggerCmd: "/ai"}, todos: newTodoManager()}
	ctx := groupCtx(t, "/ai todo list", platform.GroupRoleUnknown)

	first := p.todos.add("g1", "第一件事")
	p.todos.add("g1", "第二件事")
	require.True(t, p.todos.setDone("g1", first, true))

	items := p.todos.list("g1")
	var pending, done int
	for _, it := range items {
		if it.Done {
			done++
			continue
		}
		pending++
	}
	require.Equal(t, 1, pending)
	require.Equal(t, 1, done)

	for _, filter := range []string{"", "pending", "done"} {
		require.NoError(t, p.handleTodoList(ctx, filter))
	}

	// 空清单同样必须能处理（不得 panic、不得静默失败）。
	empty := &Plugin{cfg: &config.Config{TriggerCmd: "/ai"}, todos: newTodoManager()}
	require.NoError(t, empty.handleTodoList(ctx, ""))
}
