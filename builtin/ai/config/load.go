// load.go — 配置读取：把 config.yaml 的 plugins.ai 节翻译成 [Config]。
//
// 这里按"配置项属于哪块能力"分组，每组一个函数，避免单个 400 行的读取过程
// （历史上它是 376 行、75 个 if 块、cyclomatic 113 的函数）。
//
// # 零值语义
//
// 配置项对"零值"有两种意图，必须显式区分，否则用户写 0 会被静默忽略：
//
//   - **0 有效**（关闭某项能力）：用 [present] 判断 key 是否存在，存在即取，
//     典型如 trigger_cmd: ""（禁用命令触发）、image_merge_window: 0s（关闭合并窗口）；
//   - **0 视为未配置**（回落到默认）：用 v > 0 / v != "" 判断，典型如 max_tokens。
//
// 前者在字段注释里标出"0 值语义"，后者是默认行为。历史上的 bug 都来自把
// 前者当后者写，因此每次新增"允许 0"的键都要走 [present]。
package config

import (
	"encoding/json"
	"strings"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Load 从插件配置中读取配置项，未配置时使用默认值。
func Load(ctx *plugin.SetupContext) *Config {
	cfg := DefaultConfig
	if ctx.Config == nil {
		return &cfg
	}
	loadProvider(ctx, &cfg)
	loadTuning(ctx, &cfg)
	loadSession(ctx, &cfg)
	loadTriggers(ctx, &cfg)
	loadGroupReplies(ctx, &cfg)
	loadSkills(ctx, &cfg)
	loadMultimodal(ctx, &cfg)
	loadExec(ctx, &cfg)
	loadWorkspace(ctx, &cfg)
	loadApproval(ctx, &cfg)
	loadToolSelection(ctx, &cfg)
	loadContext(ctx, &cfg)
	loadMemory(ctx, &cfg)
	loadPlanning(ctx, &cfg)
	loadVerification(ctx, &cfg)

	if cfg.TriggerCmd == "" && !cfg.AtBot && !cfg.GroupAutonomous && !cfg.PrivateChat {
		ctx.Log.Warn("No trigger method enabled: set trigger_cmd, at_bot, group_autonomous, or private_chat in config")
	}
	return &cfg
}

// loadProvider 读取提供商、模型与 API 端点。
func loadProvider(ctx *plugin.SetupContext, cfg *Config) {
	// provider 大小写不敏感：newProvider 只认小写名，写 "OpenAI" 原本会被判为
	// unknown provider。非法取值在这里就给出告警，而不是等到装配期才失败。
	if v := strings.ToLower(strings.TrimSpace(ctx.Config.GetString("provider", ""))); v != "" {
		if ValidProvider(v) {
			cfg.Provider = v
		} else {
			ctx.Log.Warnf("unknown provider %q, must be openai|anthropic, ignoring", v)
		}
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
	cfg.IncludeUsage = ctx.Config.GetBool("include_usage", cfg.IncludeUsage)
}

// loadTuning 读取采样参数、超时与重试。
func loadTuning(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetInt("max_tokens", 0); v > 0 {
		cfg.MaxTokens = v
	}
	if v, ok := intOption(ctx, "max_tokens_cap"); ok {
		if v >= 0 {
			cfg.MaxTokensCap = v
		} else {
			ctx.Log.Warnf("max_tokens_cap must not be negative, ignoring %d", v)
		}
	}
	if v := strings.ToLower(strings.TrimSpace(ctx.Config.GetString("reasoning_effort", ""))); v != "" {
		if ValidReasoningEffort(v) {
			cfg.ReasoningEffort = v
		} else {
			ctx.Log.Warnf("reasoning_effort must be one of none/minimal/low/medium/high, ignoring %q", v)
		}
	}
	if v, ok := floatOption(ctx, "temperature"); ok {
		if v >= 0 && v <= 2 {
			cfg.Temperature = v
		} else {
			ctx.Log.Warnf("temperature must be within [0, 2], ignoring %v", v)
		}
	}
	if v, ok := floatOption(ctx, "top_p"); ok {
		if v >= 0 && v <= 1 {
			cfg.TopP = v
		} else {
			ctx.Log.Warnf("top_p must be within [0, 1], ignoring %v", v)
		}
	}
	if v := ctx.Config.GetDuration("api_timeout", 0); v > 0 {
		cfg.APITimeout = v
	}
	if v, ok := intOption(ctx, "max_retries"); ok {
		if v >= 0 {
			cfg.MaxRetries = v
		} else {
			ctx.Log.Warnf("max_retries must not be negative, ignoring %d", v)
		}
	}
	if v := ctx.Config.GetInt("max_depth", 0); v > 0 {
		cfg.MaxDepth = v
	}
	if v := ctx.Config.GetDuration("tool_timeout", 0); v > 0 {
		cfg.ToolTimeout = v
	}
	if v := ctx.Config.GetInt("tool_retry_limit", 0); v > 0 {
		cfg.ToolRetryLimit = v
	}
	if v := ctx.Config.GetInt("tool_parallel", 0); v > 0 {
		cfg.ToolParallel = v
	}
	if v := ctx.Config.GetDuration("turn_timeout", 0); v > 0 {
		cfg.TurnTimeout = v
	}
}

// loadSession 读取会话缓存、TTL 与历史保留。
func loadSession(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetInt("max_history", 0); v > 0 {
		cfg.MaxHistory = v
	}
	if v := ctx.Config.GetDuration("session_ttl", 0); v > 0 {
		cfg.SessionTTL = v
	}
	// session_retention: 0s 表示"保留期不限"，因此必须按存在性判断。
	if present(ctx, "session_retention") {
		if v := ctx.Config.GetDuration("session_retention", cfg.SessionRetention); v >= 0 {
			cfg.SessionRetention = v
		}
	}
	if v := ctx.Config.GetInt("session_cache_size", 0); v > 0 {
		cfg.SessionCacheSize = v
	}
	if v, ok := intOption(ctx, "image_cache_max_mb"); ok {
		if v >= 0 {
			cfg.ImageCacheMaxMB = v
		} else {
			ctx.Log.Warnf("image_cache_max_mb must not be negative, ignoring %d", v)
		}
	}
}

// loadTriggers 读取触发方式与系统提示词。
func loadTriggers(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetString("system_prompt", ""); v != "" {
		cfg.SystemPrompt = v
	}
	// trigger_cmd: "" 表示禁用命令触发，必须按存在性判断（否则永远回退默认 /ai）。
	if present(ctx, "trigger_cmd") {
		cfg.TriggerCmd = ctx.Config.GetString("trigger_cmd", cfg.TriggerCmd)
	}
	cfg.AtBot = ctx.Config.GetBool("at_bot", cfg.AtBot)
	cfg.GroupAutonomous = ctx.Config.GetBool("group_autonomous", cfg.GroupAutonomous)
	cfg.PrivateChat = ctx.Config.GetBool("private_chat", cfg.PrivateChat)
	cfg.Fallback = ctx.Config.GetBool("fallback", cfg.Fallback)
	cfg.Markdown = ctx.Config.GetBool("markdown", cfg.Markdown)
	cfg.QQRegenButton = ctx.Config.GetBool("qq_regen_button", cfg.QQRegenButton)
	cfg.QQClearButton = ctx.Config.GetBool("qq_clear_button", cfg.QQClearButton)
	cfg.QQPlanButton = ctx.Config.GetBool("qq_plan_button", cfg.QQPlanButton)
}

// loadGroupReplies 读取群自主发言策略。
func loadGroupReplies(ctx *plugin.SetupContext, cfg *Config) {
	if v := strings.ToLower(strings.TrimSpace(ctx.Config.GetString("group_reply_policy", ""))); v != "" {
		if ValidGroupReplyPolicy(v) {
			cfg.GroupReplyPolicy = v
		} else {
			ctx.Log.Warnf("invalid group_reply_policy %q, must be all|smart|judge, ignoring", v)
		}
	}
	if v, ok := floatOption(ctx, "group_reply_chance"); ok {
		if v >= 0 && v <= 1 {
			cfg.GroupReplyChance = v
		} else {
			ctx.Log.Warnf("group_reply_chance must be within [0, 1], ignoring %v", v)
		}
	}
	if v := ctx.Config.GetDuration("group_reply_cooldown", 0); v > 0 {
		cfg.GroupReplyCooldown = v
	}
}

// loadSkills 读取自定义技能的数量与长度上限。
func loadSkills(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetDuration("skill_timeout", 0); v > 0 {
		cfg.SkillTimeout = v
	}
	if v := ctx.Config.GetInt("skill_max_depth", 0); v > 0 {
		cfg.SkillMaxDepth = v
	}
	if v := ctx.Config.GetInt("max_user_skills", 0); v > 0 {
		cfg.MaxUserSkills = v
	}
	if v := ctx.Config.GetInt("max_user_skill_prompt_len", 0); v > 0 {
		cfg.MaxUserSkillPromptLen = v
	}
}

// loadMultimodal 读取多模态输入的开关与限额。
func loadMultimodal(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetInt("max_attachment_size", 0); v > 0 {
		cfg.MaxAttachmentSize = int64(v)
	}
	cfg.VisionEnabled = ctx.Config.GetBool("vision_enabled", cfg.VisionEnabled)
	cfg.AudioEnabled = ctx.Config.GetBool("audio_enabled", cfg.AudioEnabled)

	// 以下三项的 0 值都有"关闭"语义，按存在性判断。
	if present(ctx, "image_merge_window") {
		cfg.ImageMergeWindow = ctx.Config.GetDuration("image_merge_window", cfg.ImageMergeWindow)
	}
	if present(ctx, "image_context_window") {
		cfg.ImageContextWindow = ctx.Config.GetDuration("image_context_window", cfg.ImageContextWindow)
	}
	if v, ok := intOption(ctx, "image_context_turns"); ok {
		if v < 0 {
			ctx.Log.Warnf("image_context_turns must not be negative, ignoring %d", v)
		} else {
			cfg.ImageContextTurns = v
		}
	}
	if v := ctx.Config.GetInt("max_images_per_message", 0); v > 0 {
		cfg.MaxImagesPerMessage = v
	}
	if v := ctx.Config.GetInt("max_images_per_request", 0); v > 0 {
		cfg.MaxImagesPerRequest = v
	}
}

// loadExec 读取受控进程执行与执行节点归属。
func loadExec(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetDuration("exec_timeout", 0); v > 0 {
		cfg.ExecTimeout = v
	}
	if v := ctx.Config.GetInt("exec_max_output_bytes", 0); v > 0 {
		cfg.ExecMaxOutputBytes = v
	}
	cfg.ExecRequireApproval = ctx.Config.GetBool("exec_require_approval", cfg.ExecRequireApproval)

	// 执行节点归属：默认为本机。
	if v := strings.ToLower(strings.TrimSpace(ctx.Config.GetString("exec_node", ""))); v != "" {
		cfg.ExecNode = v
	}
	if v := strings.TrimSpace(ctx.Config.GetString("exec_remote_url", "")); v != "" {
		cfg.ExecRemoteURL = v
	}
	if v := ctx.Config.GetString("exec_remote_token", ""); v != "" {
		cfg.ExecRemoteToken = v
	}
	if v := ctx.Config.GetDuration("exec_remote_timeout", 0); v > 0 {
		cfg.ExecRemoteTimeout = v
	}

	// 任务预算：0 或负数表示不限制（显式配置可关闭默认值）。
	if v, ok := intOption(ctx, "task_max_tool_calls"); ok {
		cfg.TaskMaxToolCalls = v
	}
	if v := ctx.Config.GetDuration("task_max_duration", 0); v != 0 {
		cfg.TaskMaxDuration = v
	}
	if v, ok := intOption(ctx, "max_sends_per_round"); ok {
		cfg.MaxSendsPerRound = v
	}
}

// loadWorkspace 读取工作区根目录与命令白名单。
func loadWorkspace(ctx *plugin.SetupContext, cfg *Config) {
	// 三处列表配置统一口径：去掉每项首尾空白并丢弃空项。历史上 tool_allowlist
	// 不 trim，导致 " ping" 与 "ping" 行为不同（前者永远匹配不上命令名）。
	cfg.ToolAllowlist = stringList(ctx, "tool_allowlist")
	cfg.WorkspaceRoots = stringList(ctx, "workspace_roots")
	cfg.WorkspaceAllowWrite = ctx.Config.GetBool("workspace_allow_write", cfg.WorkspaceAllowWrite)

	for key, dst := range map[string]*[]string{
		"exec_allowlist":            &cfg.ExecAllowlist,
		"exec_env_allowlist":        &cfg.ExecEnvAllowlist,
		"allow_private_media_hosts": &cfg.AllowPrivateMediaHosts,
	} {
		*dst = stringList(ctx, key)
	}
}

// loadApproval 读取工具审批模式与等待时长。
func loadApproval(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetString("tool_approval", ""); v != "" {
		switch v {
		case approvalOff, approvalRestricted, approvalAlways:
			cfg.ToolApproval = v
		default:
			ctx.Log.Warnf("invalid tool_approval %q, must be off|restricted|always, ignoring", v)
		}
	}
	if v := ctx.Config.GetDuration("approval_timeout", 0); v > 0 {
		cfg.ApprovalTimeout = v
	}
	if v := ctx.Config.GetString("need_action_gate", ""); v != "" {
		switch v {
		case NeedActionAuto, NeedActionAlways:
			cfg.NeedActionGate = v
		default:
			ctx.Log.Warnf("invalid need_action_gate %q, must be auto|always, ignoring", v)
		}
	}
}

// loadToolSelection 读取工具选择、稳定策略与嵌入模型。
func loadToolSelection(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetInt("tool_select_max", 0); v > 0 {
		cfg.ToolSelectMax = v
	}
	if v := ctx.Config.GetInt("tool_budget", 0); v > 0 {
		cfg.ToolBudget = v
	}
	cfg.ToolSetSticky = ctx.Config.GetBool("tool_set_sticky", cfg.ToolSetSticky)
	if v := ctx.Config.GetInt("tool_set_sticky_max", 0); v > 0 {
		cfg.ToolSetStickyMax = v
	}
	// 非零即生效：允许负值显式关闭闲置衰减（0 等同于未配置）。
	if v := ctx.Config.GetDuration("tool_set_ttl", 0); v != 0 {
		cfg.ToolSetTTL = v
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
}

// loadContext 读取动态上下文各节的开关与限额。
func loadContext(ctx *plugin.SetupContext, cfg *Config) {
	cfg.IncludeRuntimeContext = ctx.Config.GetBool("include_runtime_context", cfg.IncludeRuntimeContext)
	cfg.IncludeMentionInfo = ctx.Config.GetBool("include_mention_info", cfg.IncludeMentionInfo)
	cfg.IncludeReplyContext = ctx.Config.GetBool("include_reply_context", cfg.IncludeReplyContext)

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
	// context_group_messages: 0 = 关闭群消息注入，按存在性判断。
	if v, ok := intOption(ctx, "context_group_messages"); ok {
		if v < 0 {
			ctx.Log.Warnf("context_group_messages must not be negative, ignoring %d", v)
		} else {
			cfg.ContextGroupMessages = v
		}
	}
	cfg.ContextGroupIncludeBot = ctx.Config.GetBool("context_group_include_bot", cfg.ContextGroupIncludeBot)
	cfg.ContextFields = stringList(ctx, "context_fields")
}

// loadMemory 读取长期记忆。
func loadMemory(ctx *plugin.SetupContext, cfg *Config) {
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
}

// loadPlanning 读取计划与后台自动推进。
func loadPlanning(ctx *plugin.SetupContext, cfg *Config) {
	if v := ctx.Config.GetInt("plan_max_steps", 0); v > 0 {
		cfg.PlanMaxSteps = v
	}
	cfg.PlanAutoContinue = ctx.Config.GetBool("plan_auto_continue", cfg.PlanAutoContinue)
	if v := ctx.Config.GetDuration("plan_auto_interval", 0); v > 0 {
		cfg.PlanAutoInterval = v
	}
	if v := ctx.Config.GetInt("plan_auto_rounds", 0); v > 0 {
		cfg.PlanAutoRounds = v
	}
}

// loadVerification 读取回答校验（LLM-as-judge）与辅助调用模型。
func loadVerification(ctx *plugin.SetupContext, cfg *Config) {
	cfg.VerifyEnabled = ctx.Config.GetBool("verify_enabled", cfg.VerifyEnabled)
	if v := ctx.Config.GetInt("verify_max_retries", 0); v > 0 {
		cfg.VerifyMaxRetries = v
	}
	if v := ctx.Config.GetString("verify_model", ""); v != "" {
		cfg.VerifyModel = v
	}
	if v := ctx.Config.GetString("extract_model", ""); v != "" {
		cfg.ExtractModel = v
	}
}

// present 报告 key 是否在配置里显式出现（含空串、0、false）。
//
// 用于"0 值有语义"的配置项：这些键写 0 是意图表达，不能被当成未配置。
func present(ctx *plugin.SetupContext, key string) bool {
	return ctx.Config.Get(key) != nil
}

// stringList 读取字符串列表配置：去掉每项首尾空白并丢弃空项。
//
// 所有列表型配置共用这一个口径——历史上 workspace_roots 会 trim 而
// tool_allowlist 不会，同一份配置在不同键上表现不一致。
func stringList(ctx *plugin.SetupContext, key string) []string {
	v := ctx.Config.Get(key)
	if v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func floatOption(ctx *plugin.SetupContext, key string) (float64, bool) {
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

func intOption(ctx *plugin.SetupContext, key string) (int, bool) {
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
