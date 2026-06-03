// Package ai 提供 AI 对话能力，支持多 LLM 提供商和工具调用。
//
// # 支持的提供商
//
// 1. OpenAI 兼容 API（provider: "openai"）
//   - 可直接使用：OpenAI（GPT-4o、GPT-4o-mini 等）
//   - 也可使用任意 OpenAI 兼容接口的服务：
//     DeepSeek（https://api.deepseek.com）、
//     月之暗面 Kimi、零一万物 Yi、
//     Groq、Together AI、vLLM、
//     Ollama（本地）、任意 OpenAI 代理等
//
// 2. Anthropic（provider: "anthropic"）
//   - Claude Sonnet 4、Claude 3.5 Sonnet、Claude 3 Opus、Claude 3 Haiku 等
//
// # 工具调用
//
// AI 插件支持两种方式暴露工具给 LLM：
//
//  1. 自动发现：自动扫描已注册的**无权限命令**并包装为工具
//  2. 显式注册：插件实现 [ToolProvider] 接口，向 AI 显式注册自定义工具
//
// # 多轮对话
//
// AI 插件天然支持多轮对话。每次用户的输入都会追加到当前会话上下文中，
// LLM 可以看到完整的对话历史。会话按 platform:chatID:userID 维度隔离，
// 不同用户/群组之间互不干扰。
//
// 上下文窗口通过 max_history 控制，超出部分从中间裁剪（保留 system prompt 和最近的消息）。
//
// # ⚠️ 安全设计
//
// 自动发现工具时**仅暴露不需要权限的命令**（Permissions 为空）。
// 需要权限的敏感命令不会被 AI 自动发现，防止通过 AI 绕过权限检查。
//
// 如需让 AI 调用需要权限的命令，插件应：
//   - 实现 [ToolProvider] 接口显式注册工具
//   - 在工具 Execute 中自行校验调用者身份
//   - 使用 system_prompt 告知 AI 该工具的使用约束
//
// # 配置示例
//
//	plugins:
//	  ai:
//	    provider: "openai"
//	    model: "gpt-4o-mini"
//	    base_url: "https://api.openai.com/v1"
//	    api_key: "${AI_API_KEY}"
//	    system_prompt: "你是一个有用的AI助手"
//
// # 常用模型推荐
//
//   - OpenAI:     gpt-4o, gpt-4o-mini, gpt-4-turbo, o1-mini
//   - DeepSeek:   deepseek-chat, deepseek-reasoner
//   - Anthropic:  claude-sonnet-4-20250514, claude-3-5-sonnet-latest
//   - 本地部署:   Ollama（base_url 设置为本地地址）
package ai

import "time"

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

	// SystemPrompt 系统提示词，定义 AI 的行为和角色。
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
}
