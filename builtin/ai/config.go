// Package ai config.go — 配置结构与加载逻辑。
//
// 本文件定义 AI 插件的全部配置项（Config）、默认值（DefaultConfig）
// 以及从 config.yaml 读取配置的加载函数（loadConfig）。
//
// 配置项覆盖 LLM 提供商选择、模型参数、超时控制、会话管理、触发方式、
// Markdown 回复、Skill 系统等。
package ai

import (
	"encoding/json"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Config AI 插件配置。
// 通过 config.yaml 的 plugins.ai 节读取。
type Config struct {
	// Provider LLM 提供商名称。
	// 支持: "openai"（默认，兼容 DeepSeek 等）、"anthropic"
	Provider string `yaml:"provider"`
	// Model 模型名称。
	// OpenAI: gpt-4o, gpt-4o-mini, gpt-4-turbo, o1-mini
	// DeepSeek: deepseek-chat, deepseek-reasoner
	// Anthropic: claude-sonnet-4-20250514, claude-3-5-sonnet-latest
	Model string `yaml:"model"`
	// BaseURL API 地址。
	// OpenAI 默认: https://api.openai.com/v1
	// DeepSeek:     https://api.deepseek.com
	// Anthropic:    https://api.anthropic.com
	// 本地 Ollama:  http://localhost:11434/v1
	BaseURL string `yaml:"base_url"`
	// APIKey API 密钥。推荐通过环境变量引用如 "${AI_API_KEY}"。
	APIKey string `yaml:"api_key"`
	// SystemPrompt 用户自定义指令，追加在 Framework Prompt 之后。
	// 用于设定 AI 的角色、个性或额外行为约束。
	SystemPrompt string `yaml:"system_prompt"`
	// TriggerCmd 触发 AI 对话的命令前缀。为空则不注册命令触发。
	TriggerCmd string `yaml:"trigger_cmd"`
	// ToolAllowlist 显式允许自动暴露为工具的命令名称列表。
	// 为空时自动发现所有无权限命令（旧行为）；非空时仅将列表中的命令暴露为工具。
	// 推荐用法：配置为 [] 启用旧行为，或列出具体命令名精确控制。
	ToolAllowlist []string `yaml:"tool_allowlist"`
	// ContextFields 运行时上下文中要注入的字段白名单。
	// 为空表示注入全部字段（默认行为）；非空时仅注入列出的字段。
	// 可选字段：
	//   - time        当前时间
	//   - platform    平台
	//   - bot_id      机器人 ID
	//   - bot_name    机器人名称
	//   - user_name   用户昵称
	//   - user_id     用户 ID
	//   - user_is_bot 发送者是否为机器人
	//   - chat_type   聊天类型（私聊/群聊/频道私信）
	//   - chat_id     群 ID / 会话 ID
	//   - chat_name   群名称 / 会话名称
	//   - parent_id   所属服务器 ID（频道类平台）
	//   - group_role  发送者群角色（群主/管理员/普通成员）
	ContextFields []string `yaml:"context_fields"`
	// MaxTokens 每次请求的最大输出 token 数。
	MaxTokens int `yaml:"max_tokens"`
	// MaxDepth 工具调用的最大递归深度。
	// 防止工具循环调用过深，默认 5。
	MaxDepth int `yaml:"max_depth"`
	// MaxHistory 保留的最大消息轮数（一对 user+assistant 算一轮）。
	// 超出部分从中间裁剪，保留首尾。
	MaxHistory int `yaml:"max_history"`
	// Temperature 采样温度，0-2，默认 0.7。越高输出越随机。
	Temperature float64 `yaml:"temperature"`
	// TopP 核采样参数，0-1，默认 1.0。替代 temperature 使用。
	TopP float64 `yaml:"top_p"`
	// APITimeout API 请求超时时间，默认 60s。
	APITimeout time.Duration `yaml:"api_timeout"`
	// MaxRetries 请求失败时的最大重试次数，默认 0（不重试）。
	MaxRetries int `yaml:"max_retries"`
	// ToolTimeout 每个工具调用的最长执行时间，默认 30s。
	// 超时后工具结果返回超时错误，对话继续。
	ToolTimeout time.Duration `yaml:"tool_timeout"`
	// ToolRetryLimit 同一工具连续失败的允许次数（默认 2）。
	// 每次失败的结果都会回填给 LLM 供其调整策略；从第 2 次连续失败起，
	// 额外注入一条"反思指令"（要求模型分析原因并采用不同策略）；
	// 连续失败达到 limit+1 次时优雅中止本轮回合并向用户说明，
	// 替代原先撞 max_depth 的裸错误。
	ToolRetryLimit int `yaml:"tool_retry_limit"`
	// SessionTTL 会话过期时间。超过此时间未活跃的会话会被清理。
	SessionTTL time.Duration `yaml:"session_ttl"`
	// SkillTimeout 每个 Skill 执行的最大时长，默认 60s。
	SkillTimeout time.Duration `yaml:"skill_timeout"`
	// SkillMaxDepth Skill 内部工具调用的最大递归深度，默认 3。
	SkillMaxDepth int `yaml:"skill_max_depth"`
	// MaxAttachmentSize 入站附件单文件大小上限（字节），超出则跳过。
	// 默认 20MB。
	MaxAttachmentSize int64 `yaml:"max_attachment_size"`
	// MaxUserSkills 每个用户最多可注册的技能数，默认 10。
	MaxUserSkills int `yaml:"max_user_skills"`
	// MaxUserSkillPromptLen 用户技能 Prompt 的最大字符数，默认 2000。
	MaxUserSkillPromptLen int `yaml:"max_user_skill_prompt_len"`
	// ContextGroupMessages 注入系统提示的最近群消息条数（0 = 关闭，默认 10）。
	// 开启后 AI 能感知同群其他成员的发言，融入多人对话。
	// 依赖 messagelog 插件；只注入入站消息（不含机器人自己的回复）。
	ContextGroupMessages int `yaml:"context_group_messages"`
	// ContextGroupIncludeBot 群聊消息窗口是否包含机器人的出站回复
	// （AI 自己的回复与其他插件的回复）。
	// 默认 false。开启后出站消息以机器人自身名称标注（未注入名称时
	// 兜底"机器人"），窗口顶部附加说明行提示这些消息由本账号发出；
	// 与 AI 会话历史重合的回复（当前会话内 AI 已说过的内容）会被
	// 自动去重，不会在窗口与对话历史中重复出现。
	ContextGroupIncludeBot bool `yaml:"context_group_include_bot"`
	// AtBot 是否响应 @机器人的消息。
	AtBot bool `yaml:"at_bot"`
	// GroupAutonomous 群聊自主发言：不 @ 机器人也响应群内消息（仅 QQ 群全量消息场景）。
	//
	// 官方 OpenClaw 插件的 requireMention=false 等价能力。开启后：
	//   - 群内所有非命令消息都会触发 AI（无需 @，需平台授予群全量消息权限）
	//   - 命令消息（以 / 开头）仍被排除，避免与命令 handler 并发争抢
	//   - @机器人 + 触发命令（trigger_cmd）的组合仍生效
	// 默认 false（仅 @ 触发）。
	GroupAutonomous bool `yaml:"group_autonomous"`
	// PrivateChat 是否自动响应私聊消息。
	//
	// 注意：开启后 AI 会响应所有非命令消息（不以 "/" 开头）。
	// 命令消息会被自动跳过，避免与命令 handler 发生并发 context 竞态。
	PrivateChat bool `yaml:"private_chat"`
	// Markdown 是否使用 Markdown 格式发送回复。
	// true  = 平台支持 MD 时用 MarkdownMessage，否则回退纯文本
	// false = 始终使用纯文本（即使平台支持 MD）
	Markdown bool `yaml:"markdown"`
	// Fallback 当没有命令匹配时，是否由 AI 兜底回复。
	// 当前预留字段，需要配合 Router 低优先级 RouteRule 使用。
	Fallback bool `yaml:"fallback"`
	// VisionEnabled 是否启用图片识别。设为 false 时入站图片会被忽略。
	VisionEnabled bool `yaml:"vision_enabled"`
	// AudioEnabled 是否启用音频输入（仅 OpenAI GPT-4o 预览版，Anthropic 不支持）。
	AudioEnabled bool `yaml:"audio_enabled"`
	// IncludeRuntimeContext 是否将运行时上下文（用户/群聊/时间等）注入系统提示。
	// 这些信息会随每次请求发送给第三方 LLM，可能涉及隐私。
	// 默认 true（保持旧行为）；设为 false 时完全不注入运行时上下文。
	IncludeRuntimeContext bool `yaml:"include_runtime_context"`
	// IncludeMentionInfo 是否将本条消息 @ 提及的其他用户以昵称形式注入用户消息。
	// 涉及第三方用户隐私，默认 true（保持旧行为）。
	IncludeMentionInfo bool `yaml:"include_mention_info"`
	// IncludeReplyContext 是否将"被回复的消息"内容前置到用户消息。
	// 群聊中"回复某条消息再 @ 机器人"时，让 AI 知道回复针对的内容。
	// 依赖 messagelog 插件记录消息历史（机器人自己的对话回复也会被记录）。
	// 默认 true。设为 false 时不注入、也不记录出站回复。
	IncludeReplyContext bool `yaml:"include_reply_context"`
	// ToolApproval 命令执行审批模式（2026-08 新增，对齐官方 OpenClaw 插件的
	// Command Execution Approval 能力）：
	//   - "off"（默认）: 不审批，工具直接执行
	//   - "restricted": 仅审批标记 RequiresApproval=true 的工具
	//   - "always":    审批所有工具调用
	// 审批交互双通道：平台支持回调按钮时发送"允许/拒绝"按钮；
	// 任何平台都可用 /ai approve <ID> /ai deny <ID> 文本命令。
	// 可被 per-group 策略覆盖（/ai group set approval <mode>）。
	ToolApproval string `yaml:"tool_approval"`
	// ApprovalTimeout 审批等待超时（默认 60s）。超时按拒绝处理。
	ApprovalTimeout time.Duration `yaml:"approval_timeout"`
	// ToolSelectMax 每轮实际发送给 LLM 的最大工具数（默认 20）。
	// 工具总数超过此值时按用户消息内容本地检索打分，取相关度最高的子集，
	// 替代旧的"LLM 单分类路由"（本地计算，零额外 LLM 调用）。
	ToolSelectMax int `yaml:"tool_select_max"`
	// ToolBudget 工具 schema 的 token 预算（默认 8000）。
	// 选择工具时按 schema 大小估算逐条累加，超过预算即停止追加。
	// 防止大量工具的描述与参数撑爆上下文窗口。
	ToolBudget int `yaml:"tool_budget"`
	// EmbeddingBaseURL Embedding API 地址（OpenAI 兼容 /embeddings 端点）。
	// 非空时启用语义检索加权：工具选择叠加 embedding 余弦相似度，
	// 未配置或请求失败时自动降级为纯关键词打分。
	// 注意：Anthropic 不提供 embedding API，可配置第三方兼容服务或本地 Ollama。
	EmbeddingBaseURL string `yaml:"embedding_base_url"`
	// EmbeddingAPIKey Embedding API 密钥。为空时回退使用 api_key。
	EmbeddingAPIKey string `yaml:"embedding_api_key"`
	// EmbeddingModel Embedding 模型名称。为空时默认 text-embedding-3-small。
	EmbeddingModel string `yaml:"embedding_model"`
	// MemoryEnabled 是否启用长期事实记忆（自动抽取，默认 false，隐私考虑）。
	// 开启后每轮对话回复完成后异步调用一次 LLM，从最近一轮对话提取
	// 稳定长期事实（偏好/习惯/约定），按用户/群作用域存入 LevelDB
	// （data/ai_memory），并在后续对话中按关键词检索注入系统提示。
	MemoryEnabled bool `yaml:"memory_enabled"`
	// MemoryMinInterval 同一作用域（用户/群）两次自动抽取的最小间隔（默认 10 分钟）。
	// 防止高频对话造成 LLM 调用成本失控。
	MemoryMinInterval time.Duration `yaml:"memory_min_interval"`
	// MemoryMaxFacts 每个作用域最多保留的事实条数（默认 50），
	// 超出后按出现次数最少、最旧的优先淘汰。
	MemoryMaxFacts int `yaml:"memory_max_facts"`
	// MemoryInjectMax 每次注入系统提示的长期记忆条数上限（默认 8），
	// 按用户消息关键词相关度取 Top-N。
	MemoryInjectMax int `yaml:"memory_inject_max"`
	// PlanMaxSteps 单个任务计划的最大步骤数（默认 8）。
	// create_plan 超过此上限会报错并提示模型合并步骤。
	PlanMaxSteps int `yaml:"plan_max_steps"`
	// VerifyEnabled 是否开启回答质量校验（LLM-as-judge，默认 false）。
	// 开启后每次对话多一次评审 LLM 调用：最终回答发送前评审是否
	// 回答了用户问题、是否捏造信息；不通过则注入修正指令重新生成。
	VerifyEnabled bool `yaml:"verify_enabled"`
	// VerifyMaxRetries 校验不通过后的最大重新生成次数（默认 1）。
	VerifyMaxRetries int `yaml:"verify_max_retries"`
	// ContextRAGMessages 相关历史消息检索（消息级 RAG，默认 0 = 关闭）。
	// 开启后按用户消息在 messagelog 历史（默认最近 7 天）中检索相关消息注入
	// 系统提示，覆盖"上周讨论的方案""上次谁说的"这类时间线/细节查询
	// （最近的 context_group_messages 窗口与长期事实记忆均覆盖不到）。
	// 两阶段检索：本地关键词预筛（零成本，无命中不花 embedding）→
	// 对候选集做 embedding 语义精排（复用 embedding_base_url）。
	ContextRAGMessages int `yaml:"context_rag_messages"`
	// ContextRAGDays 历史检索的时间窗口（天，默认 7）。
	ContextRAGDays int `yaml:"context_rag_days"`
	// ContextRAGCandidates 关键词预筛的候选消息上限（默认 500）。
	ContextRAGCandidates int `yaml:"context_rag_candidates"`
	// ContextRAGInjectMax 每次注入的相关历史消息条数上限（默认 3）。
	ContextRAGInjectMax int `yaml:"context_rag_inject_max"`
	// ContextWindow 系统提示的全局 token 预算（默认 0 = 不限，维持现状）。
	// 开启后按模型上下文窗口控制注入总量：超出预算时按优先级
	// （群聊窗口 > 长期记忆 > 相关历史 > 运行时上下文）动态缩减各节，
	// 防止小上下文模型被注入内容撑爆。
	ContextWindow int `yaml:"context_window"`
	// VerifyModel 回答校验器使用的模型（默认空 = 跟随主模型）。
	// 可配置便宜的轻量模型降低校验成本。
	VerifyModel string `yaml:"verify_model"`
	// ExtractModel 记忆抽取使用的模型（默认空 = 跟随主模型）。
	ExtractModel string `yaml:"extract_model"`
	// ToolParallel 工具调用的并行度（默认 4，最小 1）。
	// 模型一次返回多个工具调用时并发执行，缩短多查询回合延迟。
	ToolParallel int `yaml:"tool_parallel"`
	// PlanAutoContinue 计划后台自动推进（默认 false）。
	// 开启后创建计划后机器人按 plan_auto_interval 自动继续执行未完成步骤
	// 并主动汇报，用户无需逐条消息推动；用户发消息时重置推进预算。
	PlanAutoContinue bool `yaml:"plan_auto_continue"`
	// PlanAutoInterval 计划后台推进的间隔（默认 15s）。
	PlanAutoInterval time.Duration `yaml:"plan_auto_interval"`
	// PlanAutoRounds 单个计划的后台自动推进轮次上限（默认 3）。
	PlanAutoRounds int `yaml:"plan_auto_rounds"`
	// MaxSendsPerRound 一次对话处理内 AI 消息发送工具（send_message /
	// send_to）的总发送次数上限（默认 5；<=0 表示不限）。
	// 防止模型滥用发送工具刷屏，并行执行下同样生效。
	MaxSendsPerRound int `yaml:"max_sends_per_round"`
}

// DefaultConfig AI 插件默认配置。
var DefaultConfig = Config{
	Provider:               "openai",
	Model:                  "gpt-4o-mini",
	MaxTokens:              2048,
	MaxDepth:               5,
	MaxHistory:             20,
	Temperature:            0.7,
	TopP:                   1.0,
	APITimeout:             60 * time.Second,
	MaxRetries:             0,
	ToolTimeout:            30 * time.Second,
	ToolRetryLimit:         2,
	SessionTTL:             24 * time.Hour,
	SystemPrompt:           "你是 Remilia Bot 的 AI 助手。",
	TriggerCmd:             "/ai",
	AtBot:                  true,
	PrivateChat:            true,
	Markdown:               true,
	Fallback:               false,
	SkillTimeout:           60 * time.Second,
	SkillMaxDepth:          3,
	VisionEnabled:          true,
	AudioEnabled:           false,
	MaxAttachmentSize:      20 * 1024 * 1024,
	MaxUserSkills:          10,
	MaxUserSkillPromptLen:  2000,
	IncludeRuntimeContext:  true,
	IncludeMentionInfo:     true,
	IncludeReplyContext:    true,
	ContextGroupMessages:   10,
	ContextGroupIncludeBot: false,
	ToolApproval:           "off",
	ApprovalTimeout:        60 * time.Second,
	ToolSelectMax:          20,
	ToolBudget:             8000,
	EmbeddingModel:         "text-embedding-3-small",
	MemoryMinInterval:      10 * time.Minute,
	MemoryMaxFacts:         50,
	MemoryInjectMax:        8,
	PlanMaxSteps:           8,
	VerifyMaxRetries:       1,
	ContextRAGDays:         7,
	ContextRAGCandidates:   500,
	ContextRAGInjectMax:    3,
	ToolParallel:           4,
	PlanAutoInterval:       15 * time.Second,
	PlanAutoRounds:         3,
	MaxSendsPerRound:       5,
}

// loadConfig 从插件配置中读取配置项，未配置时使用默认值。
//
// 当前配置校验：
//   - 至少启用一种触发方式（trigger_cmd / at_bot / private_chat）
//   - provider、model、api_key 等由 Setup 阶段 NewProvider 校验
func loadConfig(ctx *plugin.SetupContext) *Config {
	cfg := DefaultConfig
	if ctx.Config == nil {
		return &cfg
	}
	if v := ctx.Config.GetString("provider", ""); v != "" {
		cfg.Provider = v
	}
	if v := ctx.Config.GetString("model", ""); v != "" {
		cfg.Model = v
	}
	if v := ctx.Config.GetString("base_url", ""); v != "" {
		cfg.BaseURL = v
	}
	if v := ctx.Config.GetString("api_key", ""); v != "" {
		cfg.APIKey = v
	}
	if v := ctx.Config.GetInt("max_tokens", 0); v > 0 {
		cfg.MaxTokens = v
	}
	if v := ctx.Config.GetInt("max_depth", 0); v > 0 {
		cfg.MaxDepth = v
	}
	if v := ctx.Config.GetInt("max_history", 0); v > 0 {
		cfg.MaxHistory = v
	}
	if v, ok := configFloat(ctx, "temperature"); ok {
		if v >= 0 && v <= 2 {
			cfg.Temperature = v
		} else {
			ctx.Log.Warnf("temperature must be within [0, 2], ignoring %v", v)
		}
	}
	if v, ok := configFloat(ctx, "top_p"); ok {
		if v >= 0 && v <= 1 {
			cfg.TopP = v
		} else {
			ctx.Log.Warnf("top_p must be within [0, 1], ignoring %v", v)
		}
	}
	if v := ctx.Config.GetDuration("api_timeout", 0); v > 0 {
		cfg.APITimeout = v
	}
	if v, ok := configInt(ctx, "max_retries"); ok {
		if v >= 0 {
			cfg.MaxRetries = v
		} else {
			ctx.Log.Warnf("max_retries must not be negative, ignoring %d", v)
		}
	}
	if v := ctx.Config.GetDuration("tool_timeout", 0); v > 0 {
		cfg.ToolTimeout = v
	}
	if v := ctx.Config.GetInt("tool_retry_limit", 0); v > 0 {
		cfg.ToolRetryLimit = v
	}
	if v := ctx.Config.GetDuration("session_ttl", 0); v > 0 {
		cfg.SessionTTL = v
	}
	if v := ctx.Config.GetString("system_prompt", ""); v != "" {
		cfg.SystemPrompt = v
	}
	if v := ctx.Config.GetString("trigger_cmd", ""); v != "" {
		cfg.TriggerCmd = v
	}
	cfg.AtBot = ctx.Config.GetBool("at_bot", cfg.AtBot)
	cfg.GroupAutonomous = ctx.Config.GetBool("group_autonomous", cfg.GroupAutonomous)
	cfg.PrivateChat = ctx.Config.GetBool("private_chat", cfg.PrivateChat)
	cfg.Markdown = ctx.Config.GetBool("markdown", cfg.Markdown)
	cfg.Fallback = ctx.Config.GetBool("fallback", cfg.Fallback)

	if v := ctx.Config.GetDuration("skill_timeout", 0); v > 0 {
		cfg.SkillTimeout = v
	}
	if v := ctx.Config.GetInt("skill_max_depth", 0); v > 0 {
		cfg.SkillMaxDepth = v
	}
	if v := ctx.Config.GetInt("max_attachment_size", 0); v > 0 {
		cfg.MaxAttachmentSize = int64(v)
	}
	cfg.VisionEnabled = ctx.Config.GetBool("vision_enabled", cfg.VisionEnabled)
	cfg.AudioEnabled = ctx.Config.GetBool("audio_enabled", cfg.AudioEnabled)

	if v := ctx.Config.GetInt("max_user_skills", 0); v > 0 {
		cfg.MaxUserSkills = v
	}
	if v := ctx.Config.GetInt("max_user_skill_prompt_len", 0); v > 0 {
		cfg.MaxUserSkillPromptLen = v
	}

	if v := ctx.Config.Get("tool_allowlist"); v != nil {
		if list, ok := v.([]any); ok {
			cfg.ToolAllowlist = make([]string, 0, len(list))
			for _, item := range list {
				if s, ok := item.(string); ok && s != "" {
					cfg.ToolAllowlist = append(cfg.ToolAllowlist, s)
				}
			}
		}
	}

	cfg.IncludeRuntimeContext = ctx.Config.GetBool("include_runtime_context", cfg.IncludeRuntimeContext)
	cfg.IncludeMentionInfo = ctx.Config.GetBool("include_mention_info", cfg.IncludeMentionInfo)
	cfg.IncludeReplyContext = ctx.Config.GetBool("include_reply_context", cfg.IncludeReplyContext)

	if v := ctx.Config.GetString("tool_approval", ""); v != "" {
		switch v {
		case "off", "restricted", "always":
			cfg.ToolApproval = v
		default:
			ctx.Log.Warnf("invalid tool_approval %q, must be off|restricted|always, ignoring", v)
		}
	}
	if v := ctx.Config.GetDuration("approval_timeout", 0); v > 0 {
		cfg.ApprovalTimeout = v
	}

	if v := ctx.Config.GetInt("tool_select_max", 0); v > 0 {
		cfg.ToolSelectMax = v
	}
	if v := ctx.Config.GetInt("tool_budget", 0); v > 0 {
		cfg.ToolBudget = v
	}
	if v := ctx.Config.GetString("embedding_base_url", ""); v != "" {
		cfg.EmbeddingBaseURL = v
	}
	if v := ctx.Config.GetString("embedding_api_key", ""); v != "" {
		cfg.EmbeddingAPIKey = v
	}
	if v := ctx.Config.GetString("embedding_model", ""); v != "" {
		cfg.EmbeddingModel = v
	}
	cfg.MemoryEnabled = ctx.Config.GetBool("memory_enabled", cfg.MemoryEnabled)
	if v := ctx.Config.GetDuration("memory_min_interval", 0); v > 0 {
		cfg.MemoryMinInterval = v
	}
	if v := ctx.Config.GetInt("memory_max_facts", 0); v > 0 {
		cfg.MemoryMaxFacts = v
	}
	if v := ctx.Config.GetInt("memory_inject_max", 0); v > 0 {
		cfg.MemoryInjectMax = v
	}
	if v := ctx.Config.GetInt("plan_max_steps", 0); v > 0 {
		cfg.PlanMaxSteps = v
	}
	cfg.VerifyEnabled = ctx.Config.GetBool("verify_enabled", cfg.VerifyEnabled)
	if v := ctx.Config.GetInt("verify_max_retries", 0); v > 0 {
		cfg.VerifyMaxRetries = v
	}
	if v := ctx.Config.GetInt("context_rag_messages", 0); v > 0 {
		cfg.ContextRAGMessages = v
	}
	if v := ctx.Config.GetInt("context_rag_days", 0); v > 0 {
		cfg.ContextRAGDays = v
	}
	if v := ctx.Config.GetInt("context_rag_candidates", 0); v > 0 {
		cfg.ContextRAGCandidates = v
	}
	if v := ctx.Config.GetInt("context_rag_inject_max", 0); v > 0 {
		cfg.ContextRAGInjectMax = v
	}
	if v := ctx.Config.GetInt("context_window", 0); v > 0 {
		cfg.ContextWindow = v
	}
	if v := ctx.Config.GetString("verify_model", ""); v != "" {
		cfg.VerifyModel = v
	}
	if v := ctx.Config.GetString("extract_model", ""); v != "" {
		cfg.ExtractModel = v
	}
	if v := ctx.Config.GetInt("tool_parallel", 0); v > 0 {
		cfg.ToolParallel = v
	}
	cfg.PlanAutoContinue = ctx.Config.GetBool("plan_auto_continue", cfg.PlanAutoContinue)
	if v := ctx.Config.GetDuration("plan_auto_interval", 0); v > 0 {
		cfg.PlanAutoInterval = v
	}
	if v := ctx.Config.GetInt("plan_auto_rounds", 0); v > 0 {
		cfg.PlanAutoRounds = v
	}
	if v := ctx.Config.GetInt("max_sends_per_round", 0); v > 0 {
		cfg.MaxSendsPerRound = v
	}

	if v := ctx.Config.GetInt("context_group_messages", 0); v > 0 {
		cfg.ContextGroupMessages = v
	}
	cfg.ContextGroupIncludeBot = ctx.Config.GetBool("context_group_include_bot", cfg.ContextGroupIncludeBot)

	if v := ctx.Config.Get("context_fields"); v != nil {
		if list, ok := v.([]any); ok {
			cfg.ContextFields = make([]string, 0, len(list))
			for _, item := range list {
				if s, ok := item.(string); ok && s != "" {
					cfg.ContextFields = append(cfg.ContextFields, s)
				}
			}
		}
	}

	if cfg.TriggerCmd == "" && !cfg.AtBot && !cfg.GroupAutonomous && !cfg.PrivateChat {
		ctx.Log.Warn("No trigger method enabled: set trigger_cmd, at_bot, group_autonomous, or private_chat in config")
	}

	return &cfg
}

func configFloat(ctx *plugin.SetupContext, key string) (float64, bool) {
	switch v := ctx.Config.Get(key).(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func configInt(ctx *plugin.SetupContext, key string) (int, bool) {
	switch v := ctx.Config.Get(key).(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}
