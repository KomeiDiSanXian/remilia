// Package ai config.go — 配置结构与加载逻辑。
//
// 本文件定义 AI 插件的全部配置项（Config）、默认值（DefaultConfig）
// 以及从 config.yaml 读取配置的加载函数（loadConfig）。
//
// 配置项覆盖 LLM 提供商选择、模型参数、超时控制、会话管理、触发方式、
// Markdown 回复、Skill 系统等。
package ai

import (
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

	// SystemPrompt 用户自定义指令，追加在 Framework Prompt 之后。
	// 用于设定 AI 的角色、个性或额外行为约束。
	SystemPrompt string `yaml:"system_prompt"`

	// TriggerCmd 触发 AI 对话的命令前缀。为空则不注册命令触发。
	TriggerCmd string `yaml:"trigger_cmd"`

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

	// SkillTimeout 每个 Skill 执行的最大时长，默认 60s。
	SkillTimeout time.Duration `yaml:"skill_timeout"`

	// SkillMaxDepth Skill 内部工具调用的最大递归深度，默认 3。
	SkillMaxDepth int `yaml:"skill_max_depth"`

	// VisionEnabled 是否启用图片识别。设为 false 时入站图片会被忽略。
	VisionEnabled bool `yaml:"vision_enabled"`

	// AudioEnabled 是否启用音频输入（仅 OpenAI GPT-4o 预览版，Anthropic 不支持）。
	AudioEnabled bool `yaml:"audio_enabled"`

	// MaxAttachmentSize 入站附件单文件大小上限（字节），超出则跳过。
	// 默认 20MB。
	MaxAttachmentSize int64 `yaml:"max_attachment_size"`
}

// DefaultConfig AI 插件默认配置。
var DefaultConfig = Config{
	Provider:          "openai",
	Model:             "gpt-4o-mini",
	MaxTokens:         2048,
	MaxDepth:          5,
	MaxHistory:        20,
	Temperature:       0.7,
	TopP:              1.0,
	APITimeout:        60 * time.Second,
	MaxRetries:        0,
	ToolTimeout:       30 * time.Second,
	SessionTTL:        24 * time.Hour,
	SystemPrompt:      "你是 Remilia Bot 的 AI 助手。",
	TriggerCmd:        "/ai",
	AtBot:             true,
	PrivateChat:       true,
	Markdown:          true,
	Fallback:          false,
	SkillTimeout:      60 * time.Second,
	SkillMaxDepth:     3,
	VisionEnabled:     true,
	AudioEnabled:      false,
	MaxAttachmentSize: 20 * 1024 * 1024,
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
	if v := ctx.Config.GetFloat64("temperature", 0); v > 0 {
		cfg.Temperature = v
	}
	if v := ctx.Config.GetFloat64("top_p", 0); v > 0 {
		cfg.TopP = v
	}
	if v := ctx.Config.GetDuration("api_timeout", 0); v > 0 {
		cfg.APITimeout = v
	}
	if v := ctx.Config.GetInt("max_retries", 0); v > 0 {
		cfg.MaxRetries = v
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

	if cfg.TriggerCmd == "" && !cfg.AtBot && !cfg.PrivateChat {
		ctx.Log.Warn("No trigger method enabled: set trigger_cmd, at_bot, or private_chat in config")
	}

	return &cfg
}
