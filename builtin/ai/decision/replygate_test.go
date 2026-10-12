// replygate_test.go — 群自主发言接话决策（纯算法层）测试。
package decision

import "testing"

func TestShouldReply_StrongSignalsAlwaysReply(t *testing.T) {
	in := ReplyInput{Query: "666", Strong: true}
	if got := ShouldReply(in); got != TierReply {
		t.Errorf("strong signal must reply, got %d", got)
	}
	// 强信号优先于复读/社交/嫌弃判定。
	if got := ShouldReply(ReplyInput{Query: "闭嘴", Strong: true, Duplicate: true}); got != TierReply {
		t.Errorf("strong signal overrides all filters, got %d", got)
	}
}

func TestShouldReply_Rejects(t *testing.T) {
	tests := []struct {
		name string
		in   ReplyInput
	}{
		{"淡然复读刷屏", ReplyInput{Query: "大家好", Duplicate: true}},
		{"嫌弃表达", ReplyInput{Query: "闭嘴，别刷了"}},
		{"确切嫌弃+被回复", ReplyInput{Query: "滚", ReplyToBot: true}},
		{"纯打招呼", ReplyInput{Query: "大家好"}},
		{"纯谢谢", ReplyInput{Query: "谢谢啦"}},
		{"纯笑声", ReplyInput{Query: "哈哈哈哈"}},
		{"干扰因子", ReplyInput{Query: "666666"}},
		{"混入干扰段", ReplyInput{Query: "太逗了这集"}},
		{"超短应答", ReplyInput{Query: "哦"}},
		{"个人称呼打扰", ReplyInput{Query: "是吧"}},
		{"空串", ReplyInput{}},
	}
	for _, tc := range tests {
		if got := ShouldReply(tc.in); got != TierReject {
			t.Errorf("%s: got %d, want TierReject", tc.name, got)
		}
	}
}

func TestShouldReply_TalksToBot(t *testing.T) {
	// 回复 bot + 第二人称/疑问 → 必回。
	cases := []ReplyInput{
		{Query: "你能帮我算下吗", ReplyToBot: true},
		{Query: "你说的这个靠谱吗？", ReplyToBot: true},
		{Query: "帮我查一下快递", ReplyToBot: true},
		{Query: "那是什么呀", ReplyToBot: true},
		{Query: "我大意就这句话不是这么说的", ReplyToBot: true}, // 足够长
		{Query: "帮我看看", ReplyToBot: false, MentionsBot: true},
		{Query: "诸葛喵直接帮我的", ReplyToBot: false, MentionsBot: true},
	}
	for _, in := range cases {
		if got := ShouldReply(in); got != TierReply {
			t.Errorf("ShouldReply(%q, reply=%v, mention=%v) = %d, want TierReply", in.Query, in.ReplyToBot, in.MentionsBot, got)
		}
	}
}

func TestShouldReply_ReplyToBotButChattingPublicly(t *testing.T) {
	// 引用 bot 的消息但当众聊别的：有实质内容 → 允许按概率抽签（TierIdle）。
	if got := ShouldReply(ReplyInput{Query: "你们看这广告词", ReplyToBot: true}); got != TierIdle {
		t.Errorf("reply-bot + public chatter = %d, want TierIdle", got)
	}
	// 引用 bot 但内容无信息 → 不回。
	if got := ShouldReply(ReplyInput{Query: "？？", ReplyToBot: true}); got != TierReject {
		t.Errorf("reply-bot + short chatter = %d, want TierReject", got)
	}
}

func TestShouldReply_IdleTierForRegularChat(t *testing.T) {
	// 有信息的普通闲聊 → TierIdle（走概率抽签）。
	cases := []string{
		"总体方案看起来不太可行",
		"今天又是摸鱼的一天",
		"刚刚那家店好像关门了",
	}
	for _, q := range cases {
		if got := ShouldReply(ReplyInput{Query: q}); got != TierIdle {
			t.Errorf("ShouldReply(%q) = %d, want TierIdle", q, got)
		}
	}
}

func TestShouldReply_HasContentLiftsShortLimit(t *testing.T) {
	// 带图片的"看一下"合法诉求不应被超短拦截。
	if got := ShouldReply(ReplyInput{Query: "看下", HasContent: true}); got != TierIdle {
		t.Errorf("image+short text = %d, want TierIdle", got)
	}
	// 带图片的求助问句 → 必回。
	if got := ShouldReply(ReplyInput{Query: "看下", HasContent: true, MentionsBot: true}); got != TierReply {
		t.Errorf("image+mentioned = %d, want TierReply", got)
	}
}

func TestTalksToBotBoundary(t *testing.T) {
	yes := []string{"你再想想题目？", "请问这是什么", "帮我", "帮我看看！"}
	for _, q := range yes {
		if !talksToBot(q) {
			t.Errorf("talksToBot(%q) should be true", q)
		}
	}
	no := []string{"", "哦哦哦"}
	for _, q := range no {
		if talksToBot(q) {
			t.Errorf("talksToBot(%q) should be false", q)
		}
	}
}

func TestSubstantiveLenIgnoresPlaceholders(t *testing.T) {
	if got := substantiveLen("[图片][表情]"); got != 0 {
		t.Errorf("placeholder-only message substantiveLen = %d, want 0", got)
	}
	if got := substantiveLen("[图片] 帮帮我老天！"); got < 5 {
		t.Errorf("text+image substantiveLen = %d, expect >= 5", got)
	}
}
