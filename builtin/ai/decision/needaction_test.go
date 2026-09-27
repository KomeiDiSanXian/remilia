package decision

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNeedActionSocialOnly 冻结闸门的判否口径：只有“整条消息都是社交寒暄”
// 才判为不需要动作，允许剥离尾部语气词，但绝不因子串命中而误判。
func TestNeedActionSocialOnly(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"问候", "你好"},
		{"带标点与空白的问候", "  Hi，！ "},
		{"带语气词后缀", "谢谢啦"},
		{"带语气词后缀（应答）", "好的呢"},
		{"表情包裹", "早上好😊"},
		{"英文致谢（分隔符）", "Thank you!"},
		{"笑声", "哈哈哈哈哈"},
		{"短应答", "嗯嗯"},
		{"数字谐音告别", "88"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, NeedAction(tc.query, false), "纯社交消息不应要求动作: %q", tc.query)
		})
	}
}

// TestNeedActionNonSocial 冻结闸门的保守性：任何掺杂任务的文字都判为需要动作，
// 包括“社交用语 + 指令”的组合与无法判别的空消息。
func TestNeedActionNonSocial(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"任务请求", "帮我查下明天的天气"},
		{"社交用语夹带任务", "谢谢，帮我查下天气"},
		{"社交用语夹带指令", "行，那就删了吧"},
		{"空消息", ""},
		{"纯标点", "？？？"},
		{"纯表情", "😊😊"},
		{"知识问答", "HTTP 状态码 404 是什么意思"},
		{"未知短语", "随便问问"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, NeedAction(tc.query, false), "非纯社交消息必须要求动作: %q", tc.query)
		})
	}
}

// TestNeedActionPlanActive 冻结“进行中的计划”优先：计划未完成时即便消息是
// 纯社交寒暄，也必须继续挑动作推进。
func TestNeedActionPlanActive(t *testing.T) {
	assert.True(t, NeedAction("你好", true), "计划进行中必须要求动作")
	assert.True(t, NeedAction("帮我查下天气", true))
}
