package ai

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handleAIChat 的准入是一**有序链**：每一步都可能提前 return，顺序本身即语义。
// 重构前只有 commandguard_test 间接覆盖了 handleAI 的命令样式判断，准入链本身
// 没有任何测试固定。这里用"是否真的调用了 LLM"作为唯一可观测信号，钉住每个
// 关卡；重构若打乱顺序或漏掉某一步，必有断言失败。

// admissionFixture 一次准入链测试的环境。
type admissionFixture struct {
	plugin *Plugin
	calls  *atomic.Int32
	// lastReq 保存最后一次请求，用于断言"进入 LLM 时消息里有什么"。
	lastReq atomic.Pointer[protocol.ChatRequest]
}

// newAdmissionFixture 构造一个"LLM 恒返回 ok"的最小插件。
func newAdmissionFixture(t *testing.T, cfg *config.Config) *admissionFixture {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.TriggerCmd == "" {
		cfg.TriggerCmd = "/ai"
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = 2
	}
	if cfg.APITimeout == 0 {
		cfg.APITimeout = 5 * time.Second
	}
	if cfg.ToolTimeout == 0 {
		cfg.ToolTimeout = 3 * time.Second
	}
	if cfg.MaxHistory == 0 {
		cfg.MaxHistory = 20
	}

	f := &admissionFixture{calls: new(atomic.Int32)}
	f.plugin = &Plugin{
		cfg:      cfg,
		sm:       session.NewSessionManager(100, cfg.MaxHistory, time.Hour, nil),
		reg:      toolkit.NewToolRegistry(),
		skillReg: toolkit.NewSkillRegistry(),
	}
	f.plugin.prov = &mockProvider{chatStreamFn: func(_ context.Context, req *protocol.ChatRequest) (<-chan protocol.StreamEvent, error) {
		f.calls.Add(1)
		f.lastReq.Store(req)
		ch := make(chan protocol.StreamEvent, 2)
		ch <- protocol.StreamEvent{Type: protocol.StreamEventText, Content: "ok"}
		ch <- protocol.StreamEvent{Type: protocol.StreamEventDone}
		close(ch)
		return ch, nil
	}}
	return f
}

// sessionKey 复算私聊会话键（平台名取自 platform.NewSyntheticEvent 的默认值）。
func sessionKey() string { return "synthetic:c1:u1" }

// chatCtx 构造一条私聊上下文。
func chatCtx(t *testing.T, content string) *eventctx.Context {
	t.Helper()
	evt := platform.NewSyntheticEvent(platform.EventKindPrivateMessage, content,
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "U1"}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "c1"}),
	)
	return eventctx.NewContextFromEvent(evt, &approvalCtxSender{})
}

// TestHandleAIChatEmptyMessageDoesNotReachLLM 空消息在落库前就被丢弃。
func TestHandleAIChatEmptyMessageDoesNotReachLLM(t *testing.T) {
	f := newAdmissionFixture(t, nil)
	require.NoError(t, f.plugin.handleAIChat(chatCtx(t, ""), ""))

	assert.Zero(t, f.calls.Load(), "空消息不得调用 LLM")
	assert.Zero(t, countUserMessages(f.plugin, sessionKey()), "空消息不得进入历史")
}

// countUserMessages 统计会话里 user 角色的消息数。
//
// 用计数而不是"历史为空"：回合开始时会写入稳定系统提示词（前缀缓存的起点），
// 这与准入是否放行无关，不能作为"消息是否被接收"的判据。
func countUserMessages(p *Plugin, sessionID string) int {
	sess := p.sm.Peek(sessionID)
	if sess == nil {
		return 0
	}
	n := 0
	for _, m := range sess.SnapshotMessages() {
		if m.Role == protocol.RoleUser {
			n++
		}
	}
	return n
}

// TestHandleAIChatNormalMessageReachesLLM 正常文本进入 LLM，且用户消息已落库。
func TestHandleAIChatNormalMessageReachesLLM(t *testing.T) {
	f := newAdmissionFixture(t, nil)
	require.NoError(t, f.plugin.handleAIChat(chatCtx(t, "你好"), "你好"))

	require.EqualValues(t, 1, f.calls.Load(), "正常消息必须调用一次 LLM")

	sess := f.plugin.sm.Peek(sessionKey())
	require.NotNil(t, sess)
	msgs := sess.SnapshotMessages()
	require.NotEmpty(t, msgs)
	// 首条是稳定系统提示词（前缀缓存的可复用起点）；用户消息已落库，
	// 其后紧跟本轮的 assistant 回复。
	assert.Equal(t, protocol.RoleSystem, msgs[0].Role)

	var lastUser *protocol.Message
	for i := range msgs {
		if msgs[i].Role == protocol.RoleUser {
			lastUser = &msgs[i]
		}
	}
	require.NotNil(t, lastUser, "用户消息必须已进入会话历史")
	assert.Equal(t, "你好", lastUser.Content)
}

// TestHandleAIChatRejectsOversizedImageBeforeAnySideEffect 守卫"准入先于副作用"。
//
// 单条消息图片超过 max_images_per_message 时，必须在落库与调用 LLM 之前被拒：
// 被拒的消息不留下任何痕迹（历史、计划、LLM 调用）。这条不变量不依赖具体关卡
// 顺序，但它固定了"所有准入检查都在副作用之前"这一契约。
func TestHandleAIChatRejectsOversizedImageBeforeAnySideEffect(t *testing.T) {
	cfg := &config.Config{MaxImagesPerMessage: 1, VisionEnabled: true}
	f := newAdmissionFixture(t, cfg)

	ctx := chatCtx(t, "看图")
	sess := f.plugin.sm.GetOrCreate(sessionKey(), "u1", "c1")
	msg := protocol.Message{Role: protocol.RoleUser, Content: "看图", ContentParts: []protocol.ContentPart{
		{Type: protocol.ContentPartImage, SourceURL: "https://example.invalid/1.png"},
		{Type: protocol.ContentPartImage, SourceURL: "https://example.invalid/2.png"},
	}}
	admitted, err := f.plugin.admitMessage(ctx, sess, &msg)
	require.NoError(t, err)
	require.False(t, admitted, "图片超限必须被拒绝")

	assert.Zero(t, f.calls.Load(), "图片超限的消息不得调用 LLM")
	assert.Zero(t, countUserMessages(f.plugin, sessionKey()), "被拒消息不得进入会话历史")
}

// TestImageBudgetSeesMergedImages 守卫"预算检查必须在合并之后"。
//
// 这是准入链顺序的**唯一可观测后果**：合并会把之前队列里的图片加进本条，
// 若先检查预算再合并，一条自带 1 张图、队列里还有 1 张（上限为 1）的消息
// 会被放行——实际却会带着 2 张图进入请求。
func TestImageBudgetSeesMergedImages(t *testing.T) {
	cfg := &config.Config{
		MaxImagesPerMessage: 1,
		VisionEnabled:       true,
		ImageMergeWindow:    time.Minute,
	}
	f := newAdmissionFixture(t, cfg)

	ctx := chatCtx(t, "看图")
	sess := f.plugin.sm.GetOrCreate(sessionKey(), "u1", "c1")

	// 队列里先放一张待合并图（模拟"先发图、再发字"）。
	sess.ExtendPendingImage([]session.PendingImageRef{{
		ChatID: "c1", PlatformMsgID: "m1", URL: "https://example.invalid/queued.png",
	}}, time.Now(), time.Minute, 4)

	// 本条再自带一张：合计 2 > 上限 1，必须被拒。
	msg := protocol.Message{Role: protocol.RoleUser, Content: "看图", ContentParts: []protocol.ContentPart{
		{Type: protocol.ContentPartImage, SourceURL: "https://example.invalid/self.png"},
	}}
	admitted, err := f.plugin.admitMessage(ctx, sess, &msg)
	require.NoError(t, err)
	assert.False(t, admitted, "合并后超限的消息必须被拒（顺序：先合并、后判预算）")
}

// TestImageBudgetBoundary 守卫图片预算的边界判定（等于上限时放行）。
func TestImageBudgetBoundary(t *testing.T) {
	cfg := &config.Config{MaxImagesPerMessage: 2, VisionEnabled: true}
	p := &Plugin{cfg: cfg}

	one := []protocol.ContentPart{{Type: protocol.ContentPartImage}, {Type: protocol.ContentPartText}}
	two := []protocol.ContentPart{
		{Type: protocol.ContentPartImage}, {Type: protocol.ContentPartImage},
	}
	three := []protocol.ContentPart{
		{Type: protocol.ContentPartImage}, {Type: protocol.ContentPartImage}, {Type: protocol.ContentPartImage},
	}

	assert.True(t, p.imageBudgetAllows(one, 0))
	assert.True(t, p.imageBudgetAllows(two, 0), "等于上限应放行")
	assert.False(t, p.imageBudgetAllows(three, 0))

	// 关闭多模态时图片不进入请求，不参与上限判定。
	p.cfg.VisionEnabled = false
	assert.True(t, p.imageBudgetAllows(three, 0), "视觉禁用时图片上限不适用")
}
