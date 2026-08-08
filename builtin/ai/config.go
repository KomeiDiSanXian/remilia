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

	if cfg.TriggerCmd == "" && !cfg.AtBot && !cfg.PrivateChat {
		ctx.Log.Warn("No trigger method enabled: set trigger_cmd, at_bot, or private_chat in config")
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
