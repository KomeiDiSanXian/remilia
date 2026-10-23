// callkind.go — 一次 LLM 调用的用途（kind）与会话范围（scope）标注。
//
// 这两项只用于观测，不进入请求线格式：Provider 实现不读取，装饰器
// （builtin/ai 的 metricsProvider）读取后写进指标标签与单次调用日志。
// 挂在 context 上而不是 ChatRequest 上，是因为请求结构体描述的是发给
// 端点的载荷，观测维度不属于载荷；同时新增字段还要改所有构造点。
//
// 为什么需要 kind：对话回合、插话判定、回答校验、事实抽取、总结、技能
// 子循环、对外 Complete 共用同一套 p.prov，混在一个 token 计数器里时，
// 命中率会被"天然无前缀可复用"的辅助调用稀释（生产观测中事实抽取约占
// 调用数的四分之一，命中率接近 0）。按 kind 拆分后，主对话的真实命中率
// 才可与辅助调用分开评估。
//
// kind 与 scope 合并存放在同一个 context 值里：两者总是成对出现（同一
// 次调用的用途与会话），合并在减少 context 槽位的同时也避免两次查找。
package protocol

import "context"

// CallKind 一次 LLM 调用的用途分类（指标标签 kind 的取值）。
type CallKind string

const (
	// CallKindChat 对话回合（主工具循环，含工具轮回填后的再次调用）。
	CallKindChat CallKind = "chat"
	// CallKindJudge 群聊插话判定（decision 的 judge 档）。
	CallKindJudge CallKind = "judge"
	// CallKindVerify 回答校验（LLM-as-judge 评审）。
	CallKindVerify CallKind = "verify"
	// CallKindExtract 事实记忆抽取。
	CallKindExtract CallKind = "extract"
	// CallKindSummary 对话总结（/ai summary）。
	CallKindSummary CallKind = "summary"
	// CallKindSkill 用户 Skill 的子循环调用。
	CallKindSkill CallKind = "skill"
	// CallKindService 通过 LLMService.Complete 发起的外部一次性问答。
	CallKindService CallKind = "service"
	// CallKindOther 未标注的调用（默认值；出现即说明有调用点漏标）。
	CallKindOther CallKind = "other"
)

// callMeta 是 context 中该调用的观测标注（kind + scope 共用一个槽位）。
type callMeta struct {
	kind  CallKind
	scope string
}

// callMetaKey 是私有空结构体键，避免与其他包的 context 键冲突。
type callMetaKey struct{}

// WithCallKind 返回带调用用途标注的 ctx。nil ctx 按 Background 处理，
// 便于后台任务（记忆抽取、总结）在无父上下文时也能标注。
func WithCallKind(ctx context.Context, kind CallKind) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if kind == "" {
		return ctx
	}
	meta, _ := ctx.Value(callMetaKey{}).(callMeta)
	meta.kind = kind
	return context.WithValue(ctx, callMetaKey{}, meta)
}

// CallKindOf 读取调用用途标注；未标注返回 CallKindOther。
func CallKindOf(ctx context.Context) CallKind {
	if ctx != nil {
		if meta, ok := ctx.Value(callMetaKey{}).(callMeta); ok && meta.kind != "" {
			return meta.kind
		}
	}
	return CallKindOther
}

// WithCallScope 返回带会话范围标注的 ctx（对话/判定传会话 ID，仅用于
// 日志关联，不写进指标标签——会话数量无上界，做标签会让基数失控）。
func WithCallScope(ctx context.Context, scope string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope == "" {
		return ctx
	}
	meta, _ := ctx.Value(callMetaKey{}).(callMeta)
	meta.scope = scope
	return context.WithValue(ctx, callMetaKey{}, meta)
}

// CallScopeOf 读取会话范围标注；未标注返回空串。
func CallScopeOf(ctx context.Context) string {
	if ctx != nil {
		if meta, ok := ctx.Value(callMetaKey{}).(callMeta); ok {
			return meta.scope
		}
	}
	return ""
}
