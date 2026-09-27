// needaction.go — “本轮是否需要主动动作”闸门。
//
// 闸门是一个纯判定：输入是本轮用户消息与“是否存在进行中的计划”，输出是“是否
// 需要为新的外部观测或副作用寻找并执行动作”。它不读注册表、不调用嵌入、不碰
// 会话可变状态，因此可独立测试，也能被装配侧以任意输入复用。
//
// 判定口径（白名单式，只收窄到“确定不需要动作”）：
//
//	有进行中的计划          → true（计划推进本身就要动作）
//	消息为空或全是标点/表情 → true（没有依据，保持旧行为）
//	消息是纯社交寒暄        → false（问候/致谢/告别/应答/笑声）
//	其余                    → true
//
// 为什么只判“纯社交”：这是本地唯一能可靠识别的非动作意图。误判为 false 的代价
// 是“一次真实请求少拿到可选动作”，因此白名单只认“整条消息完全由社交用语构成”，
// 任何掺杂其它文字（哪怕只是“谢谢，帮我查下天气”）都判 true。“纯知识问答”
// 无法在本地与“需要检索的动作请求”区分，故不在判定范围内（见 L8）。
package decision

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// NeedAction 报告本轮是否需要为新的外部观测或副作用寻找并执行动作。
//
// planActive 表示会话存在进行中的计划（见 session.Plan.HasPending）；计划未完成
// 时必须继续挑动作推进，因此直接判真。
func NeedAction(query string, planActive bool) bool {
	if planActive {
		return true
	}
	return !isSocialOnly(query)
}

// socialPhrases 纯社交用语白名单。
//
// 比较前会去掉空白、标点与符号/表情；命中要求“整条消息 == 白名单短语之一”，
// 允许再剥离最多两个尾部语气词（“谢谢啦”“好的呢”）。不做子串匹配，避免把
// “行，那就删了吧”这类夹带指令的句子误判为非动作。
var socialPhrases = map[string]struct{}{
	// 问候
	"你好": {}, "您好": {}, "哈喽": {}, "嗨": {}, "hi": {}, "hello": {}, "hey": {},
	"在吗": {}, "在么": {}, "早": {}, "早安": {}, "早上好": {}, "中午好": {}, "下午好": {},
	"晚上好": {}, "晚安": {}, "morning": {}, "evening": {},
	// 致谢
	"谢谢": {}, "谢谢你": {}, "谢谢您": {}, "多谢": {}, "感谢": {}, "辛苦了": {},
	"thx": {}, "thanks": {}, "thankyou": {}, "3q": {},
	// 告别
	"再见": {}, "拜拜": {}, "bye": {}, "byebye": {}, "88": {}, "下次聊": {}, "晚点聊": {},
	// 应答
	"收到": {}, "好的": {}, "好": {}, "行": {}, "嗯": {}, "嗯嗯": {}, "哦": {}, "哦哦": {},
	"知道了": {}, "明白": {}, "明白了": {}, "了解": {}, "ok": {}, "okay": {}, "sure": {},
	"yes": {}, "no": {}, "是的": {}, "对的": {},
	// 笑声
	"哈哈": {}, "哈哈哈": {}, "嘿嘿": {}, "嘻嘻": {}, "呵呵": {}, "笑死": {},
	"lol": {}, "haha": {}, "233": {}, "2333": {},
}

// socialTailRunes 允许从短语末尾剥离的语气助词（最多剥离两个）。
const socialTailRunes = "啦呀啊哦喔噢咯呢吧嘛么哈呵了嗯"

// isSocialOnly 判断消息是否“整条都是社交寒暄”。
func isSocialOnly(query string) bool {
	norm := normalizeSocial(query)
	if norm == "" {
		// 空消息或纯标点/表情：没有可判别的文字，保守判为需要动作。
		return false
	}
	for range 3 { // 先按原样匹配，再各剥离一个语气词后匹配（共 3 次）
		if _, ok := socialPhrases[norm]; ok {
			return true
		}
		r, size := utf8.DecodeLastRuneInString(norm)
		if size == 0 || !strings.ContainsRune(socialTailRunes, r) {
			return false
		}
		norm = norm[:len(norm)-size]
	}
	return false
}

// normalizeSocial 去掉空白、标点与符号（含表情），并统一为小写。
func normalizeSocial(query string) string {
	var b strings.Builder
	b.Grow(len(query))
	for _, r := range strings.ToLower(query) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
