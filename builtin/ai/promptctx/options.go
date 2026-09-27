// options.go — 上下文各节的输入参数（由消费方声明）。
//
// 本包不依赖插件配置包（builtin/ai/config）：装配侧把与本包相关的开关显式
// 映射成 [ContextOptions]，签名因此直接暴露本层真正读到的参数。
package promptctx

// 选项默认值（配置值非法时生效）。
const (
	// DefaultRAGDays 历史检索的时间窗天数。
	DefaultRAGDays = 7
	// DefaultRAGCandidates 历史检索的候选条数上限。
	DefaultRAGCandidates = 500
)

// ContextOptions 动态上下文各节（运行时 / 群聊窗口 / 记忆 / 历史）的输入参数。
type ContextOptions struct {
	// RuntimeFields 运行时上下文的字段白名单（context_fields；空表示注入全部字段）。
	RuntimeFields []string
	// GroupIncludeBot 群聊窗口是否包含机器人自身回复（context_group_include_bot）。
	GroupIncludeBot bool
	// GroupMessages 群聊窗口条数，同时用于历史检索与窗口的去重（context_group_messages）。
	GroupMessages int
	// RAGDays 历史检索的时间窗天数（context_rag_days；<=0 用 DefaultRAGDays）。
	RAGDays int
	// RAGCandidates 历史检索的候选条数上限（context_rag_candidates；<=0 用 DefaultRAGCandidates）。
	RAGCandidates int
	// RAGInjectMax 历史检索的注入条数上限（context_rag_inject_max；<=0 表示不额外限制）。
	RAGInjectMax int
}

// ragDays 返回生效的历史检索时间窗天数。
func (o ContextOptions) ragDays() int {
	if o.RAGDays <= 0 {
		return DefaultRAGDays
	}
	return o.RAGDays
}

// ragCandidatesLimit 返回生效的历史检索候选条数上限。
func (o ContextOptions) ragCandidatesLimit() int {
	if o.RAGCandidates <= 0 {
		return DefaultRAGCandidates
	}
	return o.RAGCandidates
}
