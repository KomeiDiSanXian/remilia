package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
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

// TestSkillInvokerRecordsOutcome 校验技能调用同样进入观测回调
// （此前只有 FuncInvoker 上报，命令与 Skill 路径不上报会让指标系统性偏低）。
func TestSkillInvokerRecordsOutcome(t *testing.T) {
	boom := errors.New("loop failed")
	var gotName string
	var gotErr error
	SkillInvoker{
		Name:  "my_skill",
		Skill: toolkit.Skill{Name: "my_skill"},
		Args:  map[string]any{},
		Runner: stubSkillRunner(func(context.Context, toolkit.Skill, map[string]any) (string, error) {
			return "", boom
		}),
		Record: func(name string, err error) { gotName, gotErr = name, err },
	}.Invoke(context.Background())

	assert.Equal(t, "my_skill", gotName)
	assert.ErrorIs(t, gotErr, boom)
}

// stubCommandCatalog 把固定结果适配为 [CommandCatalog]，使调用器契约不依赖
// 插件实例（来源解析由装配侧完成，调用器只负责跑命令并如实汇报）。
type stubCommandCatalog struct {
	text string
	err  error
}

// Run 直接返回预设结果。
func (s stubCommandCatalog) Run(*eventctx.Context, string, map[string]any, *execution.CaptureSender) (string, error) {
	return s.text, s.err
}

// TestCommandInvokerReportsCapturedText 校验命令成功时原样返回捕获文本。
func TestCommandInvokerReportsCapturedText(t *testing.T) {
	got := CommandInvoker{
		Catalog: stubCommandCatalog{text: "已生成图表"},
		Name:    "chart",
		Args:    map[string]any{},
		CS:      &execution.CaptureSender{},
	}.Invoke(context.Background())

	require.NoError(t, got.Err)
	assert.Equal(t, "已生成图表", got.Text)
}

// TestCommandInvokerEmptyOutputIsNotFakeSuccess 冻结"命中命令但没有文本输出"的
// 语义：如实回填"未返回文本"且 Err 为空，绝不回退成占位 Execute 冒充成功。
// 回退判据是装配侧的 Pattern 命中，不是文本是否为空。
func TestCommandInvokerEmptyOutputIsNotFakeSuccess(t *testing.T) {
	got := CommandInvoker{
		Catalog: stubCommandCatalog{},
		Name:    "chart",
		Args:    map[string]any{},
		CS:      &execution.CaptureSender{},
	}.Invoke(context.Background())

	require.NoError(t, got.Err)
	assert.Equal(t, `(命令 "chart" 已执行，未返回文本)`, got.Text)
	assert.NotEmpty(t, got.Text, "空输出不得返回空文本，否则调用方会误判为未命中命令")
}

// TestCommandInvokerFailureIsTyped 校验命令失败产出类型化错误与可读文案
// （命令路径此前永不置 Err，失败既不计入重试预算也不触发反思）。
func TestCommandInvokerFailureIsTyped(t *testing.T) {
	boom := errors.New("handler failed")
	got := CommandInvoker{
		Catalog: stubCommandCatalog{err: boom},
		Name:    "chart",
		Args:    map[string]any{},
		CS:      &execution.CaptureSender{},
	}.Invoke(context.Background())

	assert.Equal(t, `错误: 命令 "chart" 执行失败: handler failed`, got.Text)
	assert.ErrorIs(t, got.Err, boom)
}

// TestCommandInvokerTimeoutText 校验取消/超时归为"执行超时"。
func TestCommandInvokerTimeoutText(t *testing.T) {
	got := CommandInvoker{
		Catalog: stubCommandCatalog{err: context.DeadlineExceeded},
		Name:    "chart",
		Args:    map[string]any{},
		CS:      &execution.CaptureSender{},
	}.Invoke(context.Background())

	assert.Equal(t, `错误: 命令 "chart" 执行超时`, got.Text)
	assert.ErrorIs(t, got.Err, context.DeadlineExceeded)
}

// TestCommandInvokerRecordsOutcome 校验观测回调覆盖成功与失败两条路径。
func TestCommandInvokerRecordsOutcome(t *testing.T) {
	var names []string
	var errs []error
	record := func(name string, err error) {
		names = append(names, name)
		errs = append(errs, err)
	}
	boom := errors.New("boom")

	CommandInvoker{Catalog: stubCommandCatalog{text: "ok"}, Name: "ok_cmd", Args: map[string]any{}, CS: &execution.CaptureSender{}, Record: record}.Invoke(context.Background())
	CommandInvoker{Catalog: stubCommandCatalog{err: boom}, Name: "bad_cmd", Args: map[string]any{}, CS: &execution.CaptureSender{}, Record: record}.Invoke(context.Background())

	assert.Equal(t, []string{"ok_cmd", "bad_cmd"}, names)
	assert.Equal(t, []error{nil, boom}, errs)
}
