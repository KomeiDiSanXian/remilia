package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFuncInvokerSuccess 校验普通动作成功时原样返回文本且无失败标记。
func TestFuncInvokerSuccess(t *testing.T) {
	got := FuncInvoker{
		Name: "echo",
		Args: map[string]any{},
		Fn:   func(context.Context, map[string]any) (string, error) { return "pong", nil },
	}.Invoke(context.Background())

	require.NoError(t, got.Err)
	assert.Equal(t, "pong", got.Text)
}

// TestFuncInvokerFailureText 校验失败文案与失败标记（逐字冻结既有格式）。
func TestFuncInvokerFailureText(t *testing.T) {
	boom := errors.New("boom")
	got := FuncInvoker{
		Name: "echo",
		Args: map[string]any{},
		Fn:   func(context.Context, map[string]any) (string, error) { return "", boom },
	}.Invoke(context.Background())

	assert.Equal(t, `错误: 工具 "echo" 执行失败: boom`, got.Text)
	assert.ErrorIs(t, got.Err, boom)
}

// TestFuncInvokerTimeoutText 校验取消/超时归为"执行超时"（文案冻结）。
func TestFuncInvokerTimeoutText(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		got := FuncInvoker{
			Name: "slow",
			Args: map[string]any{},
			Fn:   func(context.Context, map[string]any) (string, error) { return "", err },
		}.Invoke(context.Background())

		assert.Equal(t, `错误: 工具 "slow" 执行超时`, got.Text)
		assert.ErrorIs(t, got.Err, err)
	}
}

// TestFuncInvokerRecordsOutcome 校验观测回调收到动作名与错误（成功为 nil）。
func TestFuncInvokerRecordsOutcome(t *testing.T) {
	boom := errors.New("boom")
	var gotName string
	var gotErr error
	FuncInvoker{
		Name:   "echo",
		Args:   map[string]any{},
		Fn:     func(context.Context, map[string]any) (string, error) { return "", boom },
		Record: func(name string, err error) { gotName, gotErr = name, err },
	}.Invoke(context.Background())

	assert.Equal(t, "echo", gotName)
	assert.ErrorIs(t, gotErr, boom)
}

// stubSkillRunner 把函数适配为 [SkillRunner]，使调用器契约不依赖插件实例。
type stubSkillRunner func(context.Context, toolkit.Skill, map[string]any) (string, error)

// Run 直接转发到被包装的函数。
func (f stubSkillRunner) Run(ctx context.Context, skill toolkit.Skill, args map[string]any) (string, error) {
	return f(ctx, skill, args)
}

// TestSkillInvokerFailureText 校验技能失败文案（逐字冻结既有格式）。
func TestSkillInvokerFailureText(t *testing.T) {
	boom := errors.New("loop failed")
	got := SkillInvoker{
		Name:  "my_skill",
		Skill: toolkit.Skill{Name: "my_skill"},
		Args:  map[string]any{},
		Runner: stubSkillRunner(func(context.Context, toolkit.Skill, map[string]any) (string, error) {
			return "", boom
		}),
	}.Invoke(context.Background())

	assert.Equal(t, `错误: 技能 "my_skill" 执行失败: loop failed`, got.Text)
	assert.ErrorIs(t, got.Err, boom)
}

// TestSkillInvokerSuccess 校验技能成功时原样返回文本且无失败标记。
func TestSkillInvokerSuccess(t *testing.T) {
	got := SkillInvoker{
		Name:  "my_skill",
		Skill: toolkit.Skill{Name: "my_skill"},
		Args:  map[string]any{},
		Runner: stubSkillRunner(func(context.Context, toolkit.Skill, map[string]any) (string, error) {
			return "skill done", nil
		}),
	}.Invoke(context.Background())

	require.NoError(t, got.Err)
	assert.Equal(t, "skill done", got.Text)
}
