package engine

// index_regex.go — 正则匹配器索引（BandSlow）
//
// 第一个慢带索引：正则规则（OnRegex）是唯一"昂贵"的规则族
// （每 matcher 每事件一次 MatchString），其余规则均为廉价字符串操作。
//
// 慢带语义（docs/notes/25 Fast/Slow 契约）：
//   - 正则 matcher 仅在快带（permanent/command/temp）未阻断时参与执行
//   - 优先级应低于快带 matcher（文档化契约，非强制）
//   - fast 阶段被 block 短路时，本索引根本不会被查询——正则零成本跳过
//
// 候选生成在惰性阶段构建时执行一次正则预匹配（LRU 编译缓存共享），
// Match() 对 regexIndexed matcher 跳过 Rules[0]，正则不执行两次。
//
// 注意：候选过滤列表为每事件新建切片（慢带路径可接受——
// 正则执行成本远高于一次切片分配；快带路径保持零分配）。

import (
	"github.com/KomeiDiSanXian/remilia/core/context"
)

// regexIndex 从 COW state 的 regexIndex 检索 Regex() 注册的匹配器，
// 在候选生成阶段对消息内容逐条做正则预匹配，并携带捕获组
// （随候选 Meta 注入 Context，handler 通过 ctx.RegexResult() 读取，
// 无需重新执行正则）。
type regexIndex struct{}

// Band 返回 BandSlow。
func (regexIndex) Band() RoutingBand { return BandSlow }

// Candidates 消息内容为空时返回空候选；
// 否则对 specific/generic 两桶逐条预匹配，收集命中的匹配器及其捕获组。
// 返回列表按原桶序（已按优先级升序），过滤不改变相对顺序。
func (regexIndex) Candidates(env MatcherEnv, ctx *context.Context) MatcherCandidates {
	var c MatcherCandidates
	content := ctx.GetMessageContent()
	if content == "" {
		return c
	}
	c.AddMeta(matchRegexBucket(env.RegexCandidates(ctx.GetEventType()), content))
	c.AddMeta(matchRegexBucket(env.RegexCandidates(""), content))
	return c
}

// matchRegexBucket 对桶内 matcher 逐条预匹配，返回命中列表及其捕获组
// （与列表 1:1 对齐）。
func matchRegexBucket(bucket []*Matcher, content string) ([]*Matcher, []any) {
	if len(bucket) == 0 {
		return nil, nil
	}
	matched := make([]*Matcher, 0, len(bucket))
	metas := make([]any, 0, len(bucket))
	for _, m := range bucket {
		// 与 OnRegex 规则共享 LRU 编译缓存，模式只编译一次
		re := context.MustGetCachedRegexp(m.regexPattern)
		if groups := re.FindStringSubmatch(content); groups != nil {
			matched = append(matched, m)
			metas = append(metas, context.RegexMatch{Pattern: m.regexPattern, Groups: groups})
		}
	}
	return matched, metas
}
