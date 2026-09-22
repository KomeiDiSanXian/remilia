// Package ai freeze_execute_test.go — 执行语义（Execute 三态）的冻结用例。
//
// 对应 docs/notes/27-ai-freeze-checklist.md 的 §H 7–11：命令工具占位执行、
// 来源解析优先级、命令输出的附件捕获、错误前缀判定、Skill 循环走非流式。
package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/runtime"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEventProcessor 冒充引擎事件处理器，用于模拟"真实命令"路径：
// execution.RunCommand 会把合成事件交给它，由它决定命令 handler 产生什么输出。
type fakeEventProcessor struct {
	onSync func(sender platform.Sender)
}

func (f *fakeEventProcessor) ProcessPlatformEvent(platform.Event, platform.Sender, ...platform.Capabilities) {
}

func (f *fakeEventProcessor) ProcessPlatformEventSync(_ platform.Event, sender platform.Sender, _ ...platform.Capabilities) {
	if f.onSync != nil {
		f.onSync(sender)
	}
}

// TestCommandToolExecuteIsPlaceholder 冻结自动发现的命令工具（B2）：
// 其 Execute 只回一句占位文案，不产生真实副作用。
func TestCommandToolExecuteIsPlaceholder(t *testing.T) {
	tool := catalog.ToolFromCommand(engine.CommandInfo{Command: "/sauce", Description: "浇头"})
	require.NotNil(t, tool)
	assert.Equal(t, "sauce", tool.Name)

	out, err := tool.Execute(context.Background(), map[string]any{"arguments": "extra"})
	require.NoError(t, err)
	assert.Equal(t, "[命令 /sauce 已触发]", out)
}

// TestExecuteToolResolutionOrder 冻结来源解析优先级（B1）：
// owner Skill > 系统 Skill > 真实命令 > 工具自身 Execute。
func TestExecuteToolResolutionOrder(t *testing.T) {
	const name = "conflict"

	const owner = "owner-1"
	evt := platform.NewSyntheticEvent("c2c", "test", platform.WithSyntheticSender(platform.UserInfo{ID: owner}))
	ctx := eventctx.NewContextFromEvent(evt, nil)

	build := func(ownerSkill, systemSkill, realCommand, plainTool bool) *Plugin {
		p := &Plugin{
			reg:      toolkit.NewToolRegistry(),
			skillReg: toolkit.NewSkillRegistry(),
			cfg:      &config.Config{SkillMaxDepth: 1, SkillTimeout: time.Minute},
			prov: &mockProvider{chatFn: func(_ context.Context, req *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				if len(req.Messages) > 0 && req.Messages[0].Content == "owner-prompt" {
					return &protocol.ChatResponse{Content: "from-owner-skill"}, nil
				}
				return &protocol.ChatResponse{Content: "from-system-skill"}, nil
			}},
		}
		if ownerSkill {
			p.skillReg.Register(toolkit.Skill{Name: name, OwnerID: owner, Prompt: "owner-prompt", Enabled: true})
		}
		if systemSkill {
			p.skillReg.Register(toolkit.Skill{Name: name, OwnerID: toolkit.OwnerSystem, Prompt: "system-prompt", Enabled: true})
		}
		if plainTool {
			p.reg.Register(toolkit.Tool{Name: name, Description: "plain", Execute: func(context.Context, map[string]any) (string, error) {
				return "from-plain-execute", nil
			}})
		}
		if realCommand {
			p.cmdPatterns = map[string]string{name: "/conflict"}
			p.syncer = &fakeEventProcessor{onSync: func(sender platform.Sender) {
				_, _ = sender.Send(context.Background(), platform.SendRequest{
					Message: platform.OutboundMessage{Text: "from-real-command"},
				})
			}}
		}
		return p
	}

	run := func(p *Plugin) string {
		return p.executeToolResult(ctx, protocol.ToolCall{Name: name}, context.Background(), &execution.CaptureSender{}, nil).Text
	}

	assert.Equal(t, "from-owner-skill", run(build(true, true, true, true)), "owner Skill 优先于其余来源")
	assert.Equal(t, "from-system-skill", run(build(false, true, true, true)), "其次命中系统 Skill")
	assert.Equal(t, "from-real-command", run(build(false, false, true, true)), "再次是真实命令")
	assert.Equal(t, "from-plain-execute", run(build(false, false, false, true)), "最后回退到工具自身 Execute")
}

// TestCaptureSenderAttachmentsMerged 冻结命令输出的附件捕获（B6/GAP-8）：
// handler 通过 captureSender 发送的正文与附件都不丢，附件并入最终回复。
func TestCaptureSenderAttachmentsMerged(t *testing.T) {
	attachment := platform.Attachment{URL: "https://example.invalid/chart.png"}

	t.Run("capture keeps text and attachments", func(t *testing.T) {
		cs := &execution.CaptureSender{}
		_, err := cs.Send(context.Background(), platform.SendRequest{
			Message: platform.OutboundMessage{Text: "图表已生成", Attachments: []platform.Attachment{attachment}},
		})
		require.NoError(t, err)
		assert.Equal(t, "图表已生成", cs.CapturedText)
		require.Len(t, cs.CapturedAttachments, 1)
	})

	t.Run("real command output reaches result and attachments merge", func(t *testing.T) {
		p := &Plugin{
			reg:         toolkit.NewToolRegistry(),
			skillReg:    toolkit.NewSkillRegistry(),
			cfg:         &config.Config{},
			cmdPatterns: map[string]string{"chart": "/chart"},
		}
		p.reg.Register(toolkit.Tool{Name: "chart", Description: "chart", Execute: func(context.Context, map[string]any) (string, error) {
			return "placeholder", nil
		}})
		p.syncer = &fakeEventProcessor{onSync: func(sender platform.Sender) {
			_, _ = sender.Send(context.Background(), platform.SendRequest{
				Message: platform.OutboundMessage{Text: "图表已生成", Attachments: []platform.Attachment{attachment}},
			})
		}}

		evt := platform.NewSyntheticEvent("c2c", "test")
		ctx := eventctx.NewContextFromEvent(evt, nil)
		cs := &execution.CaptureSender{}
		got := p.executeToolResult(ctx, protocol.ToolCall{Name: "chart"}, context.Background(), cs, nil).Text

		assert.Equal(t, "图表已生成", got, "命令正文作为工具结果回填，不被附件挤掉")
		require.Len(t, cs.CapturedAttachments, 1)

		merged := runtime.MergeChatAttachments(cs.CapturedAttachments,
			[]platform.Attachment{{URL: "https://example.invalid/extra.png"}})
		assert.Len(t, merged, 2, "捕获附件与 Provider 附件必须合并而不是互相覆盖")
		assert.Equal(t, attachment.URL, merged[0].URL)
	})
}

// TestToolResultFailureIsTyped 冻结失败判定的类型化语义（B5；原"错误:"前缀
// 判定已随类型化改造退役）：正文逐字保留，成败只看 ActionResult.Err。
func TestToolResultFailureIsTyped(t *testing.T) {
	p := &Plugin{reg: toolkit.NewToolRegistry(), skillReg: toolkit.NewSkillRegistry()}
	p.reg.Register(toolkit.Tool{Name: "boom", Execute: func(context.Context, map[string]any) (string, error) {
		return "", errors.New("boom")
	}})

	evt := platform.NewSyntheticEvent("c2c", "test")
	ctx := eventctx.NewContextFromEvent(evt, nil)

	got := p.executeToolResult(ctx, protocol.ToolCall{Name: "boom"}, context.Background(), &execution.CaptureSender{}, nil)
	require.Error(t, got.Err)
	assert.Equal(t, "错误: 工具 \"boom\" 执行失败: boom", got.Text, "失败文案必须逐字保持")

	missing := p.executeToolResult(ctx, protocol.ToolCall{Name: "nope"}, context.Background(), &execution.CaptureSender{}, nil)
	require.Error(t, missing.Err)
	assert.Equal(t, "错误: 未找到工具 \"nope\"", missing.Text)
}

// TestSkillLoopNonStreaming 冻结 Skill 子代理（B8/F6）：走非流式 LLM 调用，
// 且可见工具 = 自有工具 + 其他系统 Skill（不注入自身）。
func TestSkillLoopNonStreaming(t *testing.T) {
	var streamCalled bool
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		cfg:      &config.Config{SkillMaxDepth: 2, SkillTimeout: time.Minute},
		prov: &mockProvider{
			chatFn: func(context.Context, *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				return &protocol.ChatResponse{Content: "skill-done"}, nil
			},
			chatStreamFn: func(context.Context, *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
				streamCalled = true
				return nil, errors.New("skill loop must not stream")
			},
		},
	}

	own := toolkit.Tool{Name: "own_tool", Description: "own", Execute: func(context.Context, map[string]any) (string, error) {
		return "own", nil
	}}
	p.skillReg.Register(toolkit.Skill{Name: "other_skill", OwnerID: toolkit.OwnerSystem, Prompt: "p", Enabled: true})

	self := toolkit.Skill{Name: "self_skill", OwnerID: toolkit.OwnerSystem, Prompt: "self-prompt", Tools: []toolkit.Tool{own}, Enabled: true}
	assert.ElementsMatch(t, []string{"own_tool", "other_skill"}, toolNamesOf(toolkit.ActionsOf(p.buildSkillTools(self))),
		"可见工具 = 自有工具 + 其他系统 Skill")

	out, err := p.executeSkill(context.Background(), self, map[string]any{"query": "x"})
	require.NoError(t, err)
	assert.Equal(t, "skill-done", out)
	assert.False(t, streamCalled, "Skill 循环必须走非流式路径")
}

// TestSkillLoopCountsUsage 冻结技能调用记账：每跑一次技能子循环，该技能按
// 自身 owner + name 归属的调用计数 +1；未注册技能无可增键，只执行不计数。
func TestSkillLoopCountsUsage(t *testing.T) {
	p := &Plugin{
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
		cfg:      &config.Config{SkillMaxDepth: 1, SkillTimeout: time.Minute},
		prov: &mockProvider{
			chatFn: func(context.Context, *protocol.ChatRequest) (*protocol.ChatResponse, error) {
				return &protocol.ChatResponse{Content: "done"}, nil
			},
		},
	}

	p.skillReg.Register(toolkit.Skill{Name: "counted", OwnerID: "u1", Prompt: "p", Enabled: true})
	skill, ok := p.skillReg.GetByOwner("u1", "counted")
	require.True(t, ok, "技能应已注册")

	_, err := p.executeSkill(context.Background(), skill, map[string]any{"query": "x"})
	require.NoError(t, err)
	after, _ := p.skillReg.GetByOwner("u1", "counted")
	assert.Equal(t, int64(1), after.UsageCount, "技能执行后调用计数应 +1")

	unregistered := toolkit.Skill{Name: "ghost", OwnerID: "u1", Prompt: "p", Enabled: true}
	_, err = p.executeSkill(context.Background(), unregistered, map[string]any{"query": "x"})
	require.NoError(t, err)
	unchanged, _ := p.skillReg.GetByOwner("u1", "counted")
	assert.Equal(t, int64(1), unchanged.UsageCount, "其他技能的执行不得串改本技能计数")
}
