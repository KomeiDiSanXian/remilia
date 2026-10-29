// Package config 定义 AI 插件的配置结构与加载逻辑。
//
// 本文件定义 AI 插件的全部配置项（Config）与默认值（DefaultConfig）；
// 从 config.yaml 读取配置的过程见 load.go（按能力分组、区分零值语义）。
//
// 配置项覆盖 LLM 提供商选择、模型参数、超时控制、会话管理、触发方式、
// Markdown 回复、Skill 系统等。
package config

import (
	"time"
)

// Config AI 插件配置。
// 通过 config.yaml 的 plugins.ai 节读取。
type Config struct {
	// Provider LLM 提供商名称。
	// 支持: "openai"（默认，兼容 DeepSeek 等）、"anthropic"
	Provider string `yaml:"provider"`
	// Model 模型名称。
	// OpenAI: gpt-4o, gpt-4o-mini, gpt-4-turbo, o1-mini
	// DeepSeek: deepseek-chat, deepseek-reasoner,
	//           deepseek-v4-flash-vision-exp（图片理解）
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
	// ToolApproval 命令执行审批模式（2026-08 新增，对齐官方 OpenClaw 插件的
	// Command Execution Approval 能力）：
	//   - "off"（默认）: 不审批，工具直接执行
	//   - "restricted": 仅审批标记 RequiresApproval=true 的工具
	//   - "always":    审批所有工具调用
	// 审批交互双通道：平台支持回调按钮时发送"允许/拒绝"按钮；
	// 任何平台都可用 /ai approve <ID> /ai deny <ID> 文本命令。
	// 可被 per-group 策略覆盖（/ai group set approval <mode>）。
	ToolApproval string `yaml:"tool_approval"`
	// EmbeddingBaseURL Embedding API 地址（OpenAI 兼容 /embeddings 端点）。
	// 非空时启用语义检索加权：工具选择叠加 embedding 余弦相似度，
	// 未配置或请求失败时自动降级为纯关键词打分。
	// 注意：Anthropic 不提供 embedding API，可配置第三方兼容服务或本地 Ollama。
	EmbeddingBaseURL string `yaml:"embedding_base_url"`
	// EmbeddingAPIKey Embedding API 密钥。为空时回退使用 api_key。
	EmbeddingAPIKey string `yaml:"embedding_api_key"`
	// EmbeddingModel Embedding 模型名称。为空时默认 text-embedding-3-small。
	EmbeddingModel string `yaml:"embedding_model"`
	// VerifyModel 回答校验器使用的模型（默认空 = 跟随主模型）。
	// 可配置便宜的轻量模型降低校验成本。
	VerifyModel string `yaml:"verify_model"`
	// ExtractModel 记忆抽取使用的模型（默认空 = 跟随主模型）。
	ExtractModel string `yaml:"extract_model"`
	// ExecNode 执行能力的运行节点："local"（默认，本机工作区 + exec_allowlist）
	// 或 "remote"（文件与进程能力整体转发到 exec_remote_url 指定的执行节点）。
	// 远程节点的边界（根目录与命令白名单）由节点进程自己持有并强制。
	ExecNode string `yaml:"exec_node"`
	// ExecRemoteURL 远程执行节点地址（如 http://10.0.0.19:9020）。
	ExecRemoteURL string `yaml:"exec_remote_url"`
	// ExecRemoteToken 远程执行节点访问令牌。
	ExecRemoteToken string `yaml:"exec_remote_token"`
	// ReasoningEffort 思考程度（OpenAI 兼容的 reasoning_effort；空 = 不发送
	// 该字段，跟随端点默认行为）。
	//
	// 取值 none / minimal / low / medium / high。"none" 让思考型模型不产出
	// 推理内容、直接作答：延迟与输出 token 都显著下降，也彻底避开"推理耗尽
	// max_tokens、正文为空"的截断。实测（DeepSeek 兼容端点）：none 生效、
	// minimal 被忽略、low/medium/high 只是提示——同一问题推理长度在
	// 2.2k～4.1k token 之间波动，并不会稳定变短；因此遇到截断优先调大
	// max_tokens / max_tokens_cap，或直接设 none。
	// Anthropic 的思考由 thinking 参数控制，本配置对 anthropic 提供商无效。
	ReasoningEffort string `yaml:"reasoning_effort"`
	// GroupReplyPolicy 群自主发言的回复决策（GroupAutonomous=true 时的回复粒度）。
	//
	//   - "all"（默认）：现行行为，群里每条消息都回复（向后兼容）。
	//   - "smart"：分层决策——@/触发词/计划/被回复且在跟 bot 说话必回；
	//     纯社交（"哈哈""666"）、刷屏复读、超短无信息消息、对 bot 的嫌弃
	//     表达不回；其余弱信号消息按 group_reply_chance 概率抽签，且受
	//     group_reply_cooldown 插话冷静期约束（bot 刚说过话、无强信号时
	//     保持安静，像人一样"不无脑接话"）。
	//   - "judge"：在 smart 的本地预筛之上，把"普通闲聊是否值得插话"
	//     交给一次低成本的 LLM 判定（结合最近群聊上下文）。强信号与
	//     本地规则可确定的垃圾消息不产生判定调用；判定超时/失败时
	//     回退 group_reply_chance 概率抽签；同一时刻至多一个判定在跑。
	//
	// 群策略可用 /ai group set reply all|smart|judge [<概率>] 按群覆盖。
	GroupReplyPolicy string `yaml:"group_reply_policy"`
	// NeedActionGate“本轮是否需要主动动作”闸门的开关（默认 auto）。
	//
	// auto：纯社交寒暄轮判定为无需动作——不挑选可选动作、跳过检索与嵌入调用，
	// 工具集只保留默认保留级别与会话已用（见 decision.NeedAction）；
	// always：闸门恒开，不做判定，行为与收紧前一致。
	NeedActionGate string `yaml:"need_action_gate"`
	// ToolAllowlist 显式允许自动暴露为工具的命令名称列表。
	// 为空时自动发现所有无权限命令（旧行为）；非空时仅将列表中的命令暴露为工具。
	// 推荐用法：配置为 [] 启用旧行为，或列出具体命令名精确控制。
	ToolAllowlist []string `yaml:"tool_allowlist"`
	// AllowPrivateMediaHosts 补充受信媒体主机名单（默认空 = 严格公网 https）。
	// 附件地址可能来自平台侧而非用户输入：平台适配器会先声明自己托管的媒体
	// 源（如 OneBot 的 media_base_url），这里用于补充适配器不知道的来源
	// （内网图床、自建图片服务等）。写法 "host"（不限端口）或 "host:port"
	// （精确匹配），命中后允许 http/https 与内网地址；其余附件仍要求 https 公网。
	// 只填运维自己可信的媒体服务，不要填公网第三方站点。
	AllowPrivateMediaHosts []string `yaml:"allow_private_media_hosts"`
	// WorkspaceRoots 允许 AI 文件能力访问的根目录列表。
	// 为空表示禁用文件能力（默认）；非空时每个根目录必须已存在（构造时校验）。
	// 文件动作只读，且要求 RBAC 权限 ai.file.read。
	WorkspaceRoots []string `yaml:"workspace_roots"`
	// ExecAllowlist 允许执行的命令白名单（裸命令名，默认空 = 禁用进程执行）。
	// 非空且已配置 workspace_roots 时注册 process_run 动作；具体限制见
	// builtin/ai/procexec（命令与参数分开传入、不经过 Shell、环境变量清洗、
	// 输出上限与超时后整树回收）。
	ExecAllowlist []string `yaml:"exec_allowlist"`
	// ExecEnvAllowlist 在基础白名单之外额外允许子进程继承的环境变量名。
	ExecEnvAllowlist []string `yaml:"exec_env_allowlist"`
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
	// ExecTimeout 单次进程执行超时（默认 120s）。
	ExecTimeout time.Duration `yaml:"exec_timeout"`
	// ExecMaxOutputBytes 标准输出与标准错误各自的捕获上限（默认 32 KiB）。
	ExecMaxOutputBytes int `yaml:"exec_max_output_bytes"`
	// TaskMaxToolCalls 单个任务允许的工具调用总次数上限（默认 200；<=0 不限制）。
	// 由运行时强制执行：超限时任务置为失败并取消计划，防止失控循环。
	TaskMaxToolCalls int `yaml:"task_max_tool_calls"`
	// TaskMaxDuration 单个任务的执行时长上限（默认 2h；<=0 不限制）。
	// 由运行时强制执行：超限时任务置为失败并取消计划。
	TaskMaxDuration time.Duration `yaml:"task_max_duration"`
	// ExecRemoteTimeout 远程节点单次请求超时（<=0 时取客户端默认 120s）。
	ExecRemoteTimeout time.Duration `yaml:"exec_remote_timeout"`
	// MaxTokens 每次请求的最大输出 token 数（默认 4096）。
	//
	// 思考型模型（DeepSeek reasoning 系列等）把推理过程也算进这个预算：
	// 设得太小会出现"思考还没结束、预算就用完、一个字的正文都没输出"
	// （finish_reason=length）。插件会用 max_tokens_cap 对这类截断做一次
	// 提升预算的重试兜底，但正常回合就该给推理留出余量。
	MaxTokens int `yaml:"max_tokens"`
	// MaxTokensCap 输出预算截断重试的上限（默认 8192）。
	//
	// 思考型模型（DeepSeek reasoning 系列等）把推理过程也算进 max_tokens：
	// 预算被思考耗尽时模型不输出正文，直接以 finish_reason=length 结束，
	// 用户侧表现为"提问后没有任何回复"。这种截断可恢复——插件会按
	// max_tokens×2 提升预算重试一次，上限即本值。设为 <= max_tokens
	// 可关闭该重试（此时截断直接报错）。
	MaxTokensCap int `yaml:"max_tokens_cap"`
	// MaxDepth 工具调用的最大递归深度。
	// 防止工具循环调用过深，默认 5。
	MaxDepth int `yaml:"max_depth"`
	// MaxHistory 保留的最大历史消息条数（不是"轮数"：一条 user 或一条
	// assistant 各算一条）。超出部分从最早处裁剪，system 消息优先保留，
	// 且为稳定前缀缓存会一次多让出部分余量（见 session.TrimLowWaterDivisor）。
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
	// SessionTTL 会话在内存缓存中的闲置存活时长。
	//
	// 只影响内存驻留：闲置超过此值的会话被移出缓存，但库里的历史仍然保留，
	// 下次有人说话时重新加载。会话行的保留期见 SessionRetention。
	SessionTTL time.Duration `yaml:"session_ttl"`
	// SessionRetention 持久化会话历史的保留时长（默认 720h = 30 天）。
	//
	// 到点即永久删除库中的会话行（含内存中该会话的副本）。与 SessionTTL 解耦：
	// 早前两者共用 session_ttl，等价于"内存淘汰 = 删用户历史"，默认 24h 下
	// 用户一天没聊天对话就没了。显式配 0（session_retention: 0s）表示保留期
	// 不限，只有 /ai reset 这类显式操作才会删行。
	SessionRetention time.Duration `yaml:"session_retention"`
	// SessionCacheSize 内存中最多驻留的会话数（LRU 上限，默认 1000，<=0 用默认）。
	//
	// 内存占用约为 会话数 × max_history × 单条消息大小：工具结果可达
	// 8K 字符（MaxToolResultLen），中文 UTF-8 下约 24KB，所以 1000 × 20 条
	// 的极端上限接近 500MB。大群场景可下调到 200~500 换取更小的常驻内存，
	// 代价是被淘汰的会话下次要重新从库里加载。
	SessionCacheSize int `yaml:"session_cache_size"`
	// ImageCacheMaxMB 单会话附件（图片/音频）内存缓存的字节预算，单位 MB（默认 64）。
	//
	// 只按条数限制不够：单条附件上限由 max_attachment_size 决定（默认 20MB），
	// 32 条就是 640MB/会话。超出预算时按最早过期淘汰（刚写入的那条保留）。
	// 0 表示用默认值。
	ImageCacheMaxMB int `yaml:"image_cache_max_mb"`
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
	// ToolSetStickyMax 稳定集合中"补充工具"的数量上限（默认 8）。
	//
	// 补充工具 = 当前集合中不在本轮候选里的部分（来自此前话题，或此前已
	// 确认需要的工具）。上限用于防止"只增不减"无限膨胀到失去检索意义：
	// 超出时按最近使用时间淘汰最陈旧的补充项。
	ToolSetStickyMax int `yaml:"tool_set_sticky_max"`
	// ToolSetTTL 补充工具的空闲存活时长（默认 20 分钟）。
	//
	// 补充项连续超过该时长未被候选命中即批量移除，让长期不再相关的话题
	// 工具自然退场；负值（如 -1s）表示关闭衰减（仅受 ToolSetStickyMax 约束）。
	ToolSetTTL time.Duration `yaml:"tool_set_ttl"`
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
	// ToolParallel 工具调用的并行度（默认 4，最小 1）。
	// 模型一次返回多个工具调用时并发执行，缩短多查询回合延迟。
	ToolParallel int `yaml:"tool_parallel"`
	// PlanAutoInterval 计划后台推进的间隔（默认 15s）。
	PlanAutoInterval time.Duration `yaml:"plan_auto_interval"`
	// PlanAutoRounds 单个计划的后台自动推进轮次上限（默认 3）。
	PlanAutoRounds int `yaml:"plan_auto_rounds"`
	// MaxSendsPerRound 一次对话处理内 AI 消息发送工具（send_message /
	// send_to）的总发送次数上限（默认 5；<=0 表示不限）。
	// 防止模型滥用发送工具刷屏，并行执行下同样生效。
	MaxSendsPerRound int `yaml:"max_sends_per_round"`
	// TurnTimeout AI 一次完整处理（processWithTools 一次调用，含多轮 LLM
	// 调用与工具执行）的总时间预算。默认 0 = 自动推导：
	// api_timeout × max(2, min(max_depth, 5))。
	// 全局 Timeout 中间件（cmd/bot 默认 30s）会给事件上下文注入单次
	// deadline，多轮工具任务（如 send_message 分步执行、计划推进）会在
	// 该 deadline 处被整段切断——本配置以独立预算替换之。
	TurnTimeout time.Duration `yaml:"turn_timeout"`
	// ImageMergeWindow 群聊"先发图再发字"的合并窗口（默认 30s）。
	// 窗口内未 @/未引用的纯图片消息不回复，仅记录待后续文字合并为一条
	// 多模态消息；窗口超时仍无文字则静默丢弃（表情包防误触发）。
	// 0 = 关闭合并（纯图片消息按旧行为处理）。
	ImageMergeWindow time.Duration `yaml:"image_merge_window"`
	// ImageContextTurns 历史图片保留条数（默认 5）：最近 N 条 user 消息中的
	// 图片二进制随请求发送给模型，支持"继续追问图片细节"；更早的降级为
	// 文本占位。0 = 仅保留当前轮（旧行为）。
	ImageContextTurns int `yaml:"image_context_turns"`
	// ImageContextWindow 历史图片保留时间窗（默认 10 分钟，与附件缓存
	// contentCache TTL 对齐）。超过窗口的旧图不再随请求上传；0 = 仅按条数。
	ImageContextWindow time.Duration `yaml:"image_context_window"`
	// MaxImagesPerMessage 单条 user 消息最大图片数（默认 4）。超限时拒绝
	// 本次请求并提示用户重新编辑（不调用 LLM）。合并窗口累加后同样生效。
	MaxImagesPerMessage int `yaml:"max_images_per_message"`
	// MaxImagesPerRequest 单次 LLM 请求的最大图片总数（默认 8）。超限时
	// 从最近开始保留并提示用户（本次对话图片较多），继续处理。
	MaxImagesPerRequest int `yaml:"max_images_per_request"`
	// GroupReplyChance smart 策略下弱信号普通消息的回复概率（0~1，默认 0.2）。
	// 0 = smart 档几乎只回应强信号；1 ≈ 等价 all（其他判定仍会拦截垃圾消息）。
	GroupReplyChance float64 `yaml:"group_reply_chance"`
	// GroupReplyCooldown 插话冷静期（smart 策略）：bot 发言后多久内不再主动
	// 接话（默认 2m）。强信号（@/触发词/计划推进）不受此限。
	GroupReplyCooldown time.Duration `yaml:"group_reply_cooldown"`
	// WorkspaceAllowWrite 是否允许 AI 修改工作区文件（默认 false）。
	// 开启后注册 file_write / file_edit：两者要求 RBAC 权限 ai.file.write，
	// 且在任何审批模式下都强制人工审批。
	WorkspaceAllowWrite bool `yaml:"workspace_allow_write"`
	// ExecRequireApproval 进程执行是否在任何审批模式下都强制人工审批
	// （默认 true）。设为 false 时仍需 tool_approval=restricted 才审批。
	ExecRequireApproval bool `yaml:"exec_require_approval"`
	// IncludeUsage 流式请求是否携带 stream_options.include_usage（OpenAI 兼容 API）。
	// 用于 token 用量统计；极少数不兼容 stream_options 的网关可设为 false。
	IncludeUsage bool `yaml:"include_usage"`
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
	// QQRegenButton 是否在 QQ 单聊/群聊（频道除外）的 AI Markdown 回复上附加
	// "重新生成"键盘按钮（默认 true）。按钮为 QQ 指令按钮（action.type=2），
	// 点击后把 /ai retry 填入输入框——单聊手机端（8983+）自动发送、其余
	// （桌面端/群聊）手动发送，等价输入文本命令 /ai retry；纯文本/富媒体/
	// 频道回复不附加。
	QQRegenButton bool `yaml:"qq_regen_button"`
	// QQClearButton 是否在 QQ 单聊/群聊（频道除外）的 AI Markdown 回复上附加
	// "清空会话"键盘按钮（默认 true）。按钮为 QQ 指令按钮（action.type=2），
	// 点击后把 /ai reset 填入输入框，由用户手动发送（不自动发送，避免误触），
	// 等价输入文本命令 /ai reset：删除本插件会话历史；QQ 客户端原生"清空会话"
	// 入口（type=14 清空会话事件）不依赖此开关，始终会清空本插件会话。
	// 纯文本/富媒体/频道回复不附加。
	QQClearButton bool `yaml:"qq_clear_button"`
	// QQPlanButton 是否在 QQ 单聊/群聊（频道除外）的 Markdown 计划消息上附加
	// "查看计划/停止生成"键盘按钮（默认 true）：AI 创建任务计划时推送的
	// "计划已创建"消息与 /ai plan 状态回复会附带指令按钮（action.type=2）：
	// "查看计划"填入 /ai plan（单聊手机端自动发送），"停止生成"填入 /ai stop
	// （不自动发送；仅回合进行中时附带，触发时同时取消进行中计划）。
	// markdown=false 或非 Markdown 消息/频道不附加。
	QQPlanButton bool `yaml:"qq_plan_button"`
	// Fallback 当没有命令匹配时，是否由 AI 兜底回复。
	// 开启后等价于对群聊与私聊的**全部非命令消息**应答（不看是否 @ 机器人），
	// 即 group_autonomous 与 private_chat 的并集；群策略要求 @ 时仍由
	// handleAI 按群过滤。它是这些入口的超集，开启时不再单独注册 at_bot /
	// group_autonomous / private_chat 的 matcher，避免同一条消息重复派发。
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
	// ToolSetSticky 是否启用会话级工具集稳定策略（默认 true）。
	//
	// tools 是请求的顶级字段，在 LLM 的提示词序列里排在 system 之前，等价于
	// 一个"全局前缀开关"：工具集合一变，其后的稳定 System Prompt 与整段会话
	// 历史全部失去前缀缓存。工具检索按当前 query 打分，话题一抖动就会换掉
	// 集合里的补充项，于是历史反复 miss。开启后对检索结果叠加"滞回 + 单调
	// 并集 + 空闲衰减"：话题重叠或回访时集合不变，只在真正需要新工具时增长。
	// 设为 false 时恢复"每轮按 query 重新决定集合"（便于 A/B 对比缓存收益）。
	ToolSetSticky bool `yaml:"tool_set_sticky"`
	// MemoryEnabled 是否启用长期事实记忆（自动抽取，默认 false，隐私考虑）。
	// 开启后每轮对话回复完成后异步调用一次 LLM，从最近一轮对话提取
	// 稳定长期事实（偏好/习惯/约定），按用户/群作用域存入 LevelDB
	// （data/ai_memory），并在后续对话中按关键词检索注入系统提示。
	MemoryEnabled bool `yaml:"memory_enabled"`
	// VerifyEnabled 是否开启回答质量校验（LLM-as-judge，默认 false）。
	// 开启后每次对话多一次评审 LLM 调用：最终回答发送前评审是否
	// 回答了用户问题、是否捏造信息；不通过则注入修正指令重新生成。
	VerifyEnabled bool `yaml:"verify_enabled"`
	// PlanAutoContinue 计划后台自动推进（默认 false）。
	// 开启后创建计划后机器人按 plan_auto_interval 自动继续执行未完成步骤
	// 并主动汇报，用户无需逐条消息推动；用户发消息时重置推进预算。
	PlanAutoContinue bool `yaml:"plan_auto_continue"`
}

// provider 取值。
const (
	// ProviderOpenAI OpenAI 兼容端点（含 DeepSeek、Kimi、vLLM、Ollama 等）。
	ProviderOpenAI = "openai"
	// ProviderAnthropic Anthropic Messages API。
	ProviderAnthropic = "anthropic"
)

// ValidProvider 判断 provider 取值是否合法。
func ValidProvider(v string) bool {
	return v == ProviderOpenAI || v == ProviderAnthropic
}

// tool_approval 取值。与 builtin/ai 的 approvalOff/approvalRestricted/
// approvalAlways 同值，配置层只做取值校验，不引入插件的内部枚举类型。
const (
	// approvalOff 不审批，工具直接执行（默认）。
	approvalOff = "off"
	// approvalRestricted 仅审批标记 RequiresApproval 的工具。
	approvalRestricted = "restricted"
	// approvalAlways 审批所有工具调用。
	approvalAlways = "always"
)

// NeedActionGate 取值。
const (
	// NeedActionAuto 按本轮消息判定是否需要动作（默认）。
	NeedActionAuto = "auto"
	// NeedActionAlways 闸门恒开，始终按需要动作处理。
	NeedActionAlways = "always"
)

// reasoning_effort 的合法取值（OpenAI 兼容端点；空串 = 不发送该字段）。
const (
	// ReasoningEffortNone 关闭思考，思考型模型直接作答。
	ReasoningEffortNone = "none"
	// ReasoningEffortMinimal 最小思考（部分端点不认识该值，会被忽略）。
	ReasoningEffortMinimal = "minimal"
	// ReasoningEffortLow 低思考程度（提示性，端点不保证严格遵守）。
	ReasoningEffortLow = "low"
	// ReasoningEffortMedium 中等思考程度。
	ReasoningEffortMedium = "medium"
	// ReasoningEffortHigh 高思考程度。
	ReasoningEffortHigh = "high"
)

// ValidReasoningEffort 判断 reasoning_effort 取值是否合法。
//
// 导出与 [ValidProvider] / [ValidGroupReplyPolicy] 保持一致：这三个都是配置
// 取值的合法性判据，运维侧做配置校验时应当能用同一套判定，而不是自己再抄一份
// 允许值列表。
func ValidReasoningEffort(v string) bool {
	switch v {
	case ReasoningEffortNone, ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh:
		return true
	}
	return false
}

// GroupReplyPolicy 取值。
const (
	// GroupReplyAll 现行行为：每条消息都回复（默认，向后兼容）。
	GroupReplyAll = "all"
	// GroupReplySmart 分层决策：强信号必回，垃圾消息不回，
	// 其余按概率与冷静期抽签（见 GroupReplyPolicy 文档）。
	GroupReplySmart = "smart"
	// GroupReplyJudge 在 smart 的本地预筛之上把"模糊地带"交给 LLM 终审：
	// 强信号必回、本地规则确定的垃圾消息直接拒（不浪费判定调用），
	// 其余普通消息由一个小 LLM 调用结合最近群聊上下文决定"是否值得插话"。
	// 判定失败时回退到概率抽签；单飞并发上限 1，防止刷屏引发判定洪水。
	GroupReplyJudge = "judge"
)

// validGroupReplyPolicy 判断 group_reply_policy 取值是否合法。
func ValidGroupReplyPolicy(v string) bool {
	return v == GroupReplyAll || v == GroupReplySmart || v == GroupReplyJudge
}

// DefaultConfig AI 插件默认配置。
var DefaultConfig = Config{
	Provider:               "openai",
	Model:                  "gpt-4o-mini",
	MaxTokens:              4096,
	MaxTokensCap:           8192,
	IncludeUsage:           true,
	MaxDepth:               5,
	MaxHistory:             20,
	Temperature:            0.7,
	TopP:                   1.0,
	APITimeout:             60 * time.Second,
	MaxRetries:             0,
	ToolTimeout:            30 * time.Second,
	ToolRetryLimit:         2,
	SessionTTL:             24 * time.Hour,
	SessionRetention:       720 * time.Hour,
	SessionCacheSize:       1000,
	ImageCacheMaxMB:        64,
	ExecTimeout:            120 * time.Second,
	ExecMaxOutputBytes:     32 << 10,
	ExecRequireApproval:    true,
	TaskMaxToolCalls:       200,
	TaskMaxDuration:        2 * time.Hour,
	ExecNode:               "local",
	SystemPrompt:           "你是 Remilia Bot 的 AI 助手。",
	TriggerCmd:             "/ai",
	AtBot:                  true,
	PrivateChat:            true,
	Markdown:               true,
	QQRegenButton:          true,
	QQClearButton:          true,
	QQPlanButton:           true,
	Fallback:               false,
	SkillTimeout:           60 * time.Second,
	SkillMaxDepth:          3,
	VisionEnabled:          true,
	AudioEnabled:           false,
	MaxAttachmentSize:      20 * 1024 * 1024,
	ImageMergeWindow:       30 * time.Second,
	ImageContextTurns:      5,
	ImageContextWindow:     10 * time.Minute,
	MaxImagesPerMessage:    4,
	MaxImagesPerRequest:    8,
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
	NeedActionGate:         NeedActionAuto,
	ToolSetSticky:          true,
	ToolSetStickyMax:       8,
	ToolSetTTL:             20 * time.Minute,
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
	GroupReplyPolicy:       GroupReplyAll,
	GroupReplyChance:       0.2,
	GroupReplyCooldown:     2 * time.Minute,
}
