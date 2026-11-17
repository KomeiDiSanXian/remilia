// facts_test.go — 框架事实的取值与渲染契约。
//
// 关键不变量只有一条：**默认配置下的 Prose 输出必须与改造前逐字节一致**。
// 新增的 weekday / timezone / location / language 只有在 context_fields
// 显式列出时才出现，否则升级本身就会让所有会话的前缀缓存整体失效。
package promptctx

import (
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixFactsClock 把事实时钟固定在给定时刻，返回恢复函数。
func fixFactsClock(t *testing.T, at time.Time) {
	t.Helper()
	prev := factsClock
	factsClock = func() time.Time { return at }
	t.Cleanup(func() { factsClock = prev })
}

// factsTestContext 构造一个群聊事件上下文。
func factsTestContext() *eventctx.Context {
	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "hi",
		platform.WithSyntheticSender(platform.UserInfo{ID: "u1", DisplayName: "小明", GroupRole: platform.GroupRoleAdmin}),
		platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", Name: "测试群", ParentID: "srv", IsGroup: true}),
	)
	return eventctx.NewContextFromEvent(evt, nil)
}

// TestRenderEnvProseMatchesLegacyOutput 冻结默认字段下的逐字节输出。
//
// 期望值就是改造前 BuildRuntimeContext 用同样的输入会产出的文本：
// 字段顺序 time → platform → bot_id → bot_name → user_name → user_id →
// user_is_bot → chat_type → chat_id → chat_name → parent_id → group_role。
func TestRenderEnvProseMatchesLegacyOutput(t *testing.T) {
	at := time.Date(2026, 11, 17, 9, 30, 5, 0, time.UTC)
	fixFactsClock(t, at)

	ctx := factsTestContext()
	ctx.SetBotID("bot-1")
	ctx.SetBotName("蕾米莉亚")

	got := RenderEnvProse(ContextOptions{}, FactsFromEvent(ContextOptions{}, ctx))

	wantLines := []string{
		"当前时间: 2026-11-17 09:30:05",
		"平台: synthetic",
		"机器人 ID: bot-1",
		"机器人名称: 蕾米莉亚",
		"用户昵称: 小明",
		"用户 ID: u1",
		"发送者是否为机器人: 否",
		"聊天类型: 群聊",
		"群 ID: g1",
		"群名称: 测试群",
		"所属服务器 ID: srv",
		"发送者群角色: 管理员",
	}
	lines := strings.Split(got, "\n")
	if len(lines) != len(wantLines) {
		t.Fatalf("field count changed: got %d lines, want %d\n%s", len(lines), len(wantLines), got)
	}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("line %d: got %q, want %q", i, lines[i], want)
		}
	}
	// 新增字段默认不得出现。
	for _, marker := range []string{"星期:", "时区:", "所在地:", "用户语言:"} {
		if strings.Contains(got, marker) {
			t.Errorf("default config must not emit %q, got:\n%s", marker, got)
		}
	}
}

// TestRenderEnvProseNewFieldsOptIn 验证新增字段必须显式开启才输出。
func TestRenderEnvProseNewFieldsOptIn(t *testing.T) {
	at := time.Date(2026, 11, 17, 9, 30, 5, 0, time.UTC)
	fixFactsClock(t, at)

	opts := ContextOptions{
		RuntimeFields: []string{"time", "weekday", "timezone", "location", "language"},
		Timezone:      "Asia/Shanghai",
		Language:      "中文",
		Location:      "新加坡",
	}
	got := RenderEnvProse(opts, FactsFromEvent(opts, factsTestContext()))

	for _, want := range []string{
		"当前时间: 2026-11-17 17:30:05", // 上海 +08:00
		"星期: 星期二",
		"时区: Asia/Shanghai (UTC+08:00)",
		"所在地: 新加坡",
		"用户语言: 中文",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestFactsTimezoneAndWeekday 验证时区换算与星期名称（中文，与 language 一致）。
func TestFactsTimezoneAndWeekday(t *testing.T) {
	f := EnvironmentFacts{Now: time.Date(2026, 11, 15, 23, 30, 0, 0, time.UTC)}
	if got := f.Date(); got != "2026-11-15" {
		t.Errorf("Date() = %q", got)
	}
	if got := f.Weekday(); got != "星期日" {
		t.Errorf("Weekday() = %q, want 星期日", got)
	}
	if got := f.Offset(); got != "+00:00" {
		t.Errorf("Offset() = %q", got)
	}

	// 同一时刻在东八区已经是下一天（跨日）。
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	shanghai := EnvironmentFacts{Now: time.Date(2026, 11, 15, 23, 30, 0, 0, time.UTC).In(loc)}
	if got := shanghai.Date(); got != "2026-11-16" {
		t.Errorf("Date() in Asia/Shanghai = %q, want 2026-11-16", got)
	}
	if got := shanghai.Weekday(); got != "星期一" {
		t.Errorf("Weekday() in Asia/Shanghai = %q, want 星期一", got)
	}
	if got := shanghai.Offset(); got != "+08:00" {
		t.Errorf("Offset() in Asia/Shanghai = %q", got)
	}
}

// TestFactsPayload 验证事实到持久化载荷的映射，以及载荷的三种渲染形态。
func TestFactsPayload(t *testing.T) {
	f := EnvironmentFacts{
		Now:      time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC),
		Platform: "qq",
		Language: "中文",
	}
	p := f.Payload()
	assert.Equal(t, "2026-11-09", p.Date)
	assert.Equal(t, "星期一", p.Weekday)
	assert.Equal(t, "qq", p.Platform)
	assert.Equal(t, "中文", p.Language)

	// 序列化往返必须稳定：同一份事实任何时候都得到同一串字节。
	encoded := protocol.MarshalEnvFacts(p)
	back, ok := protocol.ParseEnvFacts(encoded)
	require.True(t, ok)
	assert.Equal(t, p, back)
	assert.Equal(t, encoded, protocol.MarshalEnvFacts(back), "重编码必须逐字节一致")
}

// TestFactsReminderLine 冻结 DeepSeek latest_reminder 的字段格式：
// date,weekday,location,platform,language，逗号分隔、字段内不允许残留逗号。
func TestFactsReminderLine(t *testing.T) {
	f := EnvironmentFacts{
		Now:      time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC),
		Platform: "qq",
		Language: "中文",
	}
	p := f.Payload()
	// 2026-11-09 是星期一。
	if got, want := p.ReminderLine(), "2026-11-09,星期一,,qq,中文"; got != want {
		t.Errorf("ReminderLine() = %q, want %q", got, want)
	}

	// 字段内的逗号会破坏分隔：必须被替换掉，且字段数保持 5。
	p.Location = "新加坡, 樟宜"
	line := p.ReminderLine()
	if strings.Count(line, ",") != 4 {
		t.Errorf("reminder must keep exactly 5 fields, got %q", line)
	}
	if strings.Contains(line, "新加坡, 樟宜") {
		t.Errorf("comma inside a field must be sanitized, got %q", line)
	}
}

// TestConversationKindOrder 冻结会话类型判定顺序（群聊优先于频道私信）。
func TestConversationKindOrder(t *testing.T) {
	cases := []struct {
		name string
		chat platform.ChatInfo
		want ConversationKind
	}{
		{"群聊", platform.ChatInfo{IsGroup: true}, ConversationGroup},
		{"频道且群聊标志也真", platform.ChatInfo{IsGroup: true, IsDM: true}, ConversationGroup},
		{"频道私信", platform.ChatInfo{IsDM: true}, ConversationChannelDM},
		{"两个标志都假", platform.ChatInfo{ID: "c1"}, ConversationPrivate},
		{"零值", platform.ChatInfo{}, ConversationPrivate},
	}
	for _, c := range cases {
		if got := ConversationKindOf(c.chat); got != c.want {
			t.Errorf("%s: ConversationKindOf = %v, want %v", c.name, got, c.want)
		}
	}
	if got := ConversationGroup.String(); got != "群聊" {
		t.Errorf("ConversationGroup.String() = %q", got)
	}
	if got := ConversationChannelDM.String(); got != "频道私信" {
		t.Errorf("ConversationChannelDM.String() = %q", got)
	}
	if got := ConversationPrivate.String(); got != "私聊" {
		t.Errorf("ConversationPrivate.String() = %q", got)
	}
}
