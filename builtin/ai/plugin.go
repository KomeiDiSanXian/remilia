// Package ai plugin.go — 插件入口与消息路由注册。
//
// 本文件定义 AI 插件的主结构体 Plugin，包含其字段说明、
// 插件描述符 New 函数、命令定义（buildAIDefinition）
// 以及三种触发方式的 handler 注册逻辑（registerHandlers）。
package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/vevent"
	"github.com/KomeiDiSanXian/remilia/command"
)

// Plugin AI 对话插件的主结构体，管理整个 AI 插件的生命周期。
//
// 字段说明：
//   - cfg: 插件配置，加载自 config.yaml plugins.ai 节
//   - coord: 命令协调器（engine.Reader），用于启动时发现已注册的命令
//   - syncer: 事件处理器（vevent），用于 executeRealCommand 合成事件触发真实命令
//   - sm: 会话管理器，负责会话的 LRU 缓存、持久化、过期清理
//   - reg: 工具注册表，管理所有可供 LLM 调用的工具
//   - prov: LLM 提供商实例，当前支持 OpenAI 兼容 API 和 Anthropic
//   - triggerCmd: 触发命令前缀（如 "/ai"），用于 cleanMessage 时剥离
//   - cmdPatterns: 工具名到完整命令模式的映射，用于 executeRealCommand 构造合成事件
//   - skillReg: 技能注册表，管理所有已注册的 Skill
type Plugin struct {
	cfg         *Config
	coord       engine.Reader
	syncer      vevent.EventProcessor
	sm          *SessionManager
	reg         *ToolRegistry
	prov        Provider
	triggerCmd  string
	cmdPatterns map[string]string
	skillReg    *SkillRegistry
}

// New 创建 AI 对话插件的描述符。
//
// 该插件支持：
//   - 多 LLM 提供商（OpenAI 兼容 API、Anthropic）
//   - 流式输出（SSE 逐 token 推送）
//   - 工具调用（自动发现无权限命令 + RegisterToolProvider 接口）
//   - 会话管理（LRU 缓存 + 可选 GORM 持久化）
//   - 多种触发方式（命令 / @机器人 / 私聊）
//
// # 安全说明
//
// ⚠️ 自动发现工具时**仅暴露不需要权限的命令**，防止通过 AI 绕过权限检查。
// 带有 Permissions 的敏感命令不会被 AI 自动发现。
// 需要 AI 可调用的权限命令应通过 [RegisterToolProvider] 显式注册。
//
//	 示例：
//
//		if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
//		    aiSvc.RegisterToolProvider(myProvider)
//		}
func New(syncer vevent.EventProcessor) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:         "ai",
		Version:      "1.0.0",
		OptionalDeps: []string{"storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "AI 对话插件，支持多提供商和工具调用",
			Category:    "功能",
			Tags:        []string{"ai", "chat", "llm", "对话", "openai", "anthropic", "deepseek"},
			HelpText: `AI 对话插件 — 使用大语言模型进行智能对话，支持工具调用。

支持的提供商：
  - OpenAI 兼容 API：OpenAI、DeepSeek、Kimi、Yi、Groq、vLLM、Ollama 等
  - Anthropic：Claude Sonnet 4、Claude 3.5 Sonnet、Claude 3 Opus 等

用法：
  /ai <消息>          — 与 AI 对话
  /ai reset           — 清空对话历史
  /ai undo            — 撤销上一条对话
  /ai retry           — 重新生成上一条回复
  /ai summary         — 总结当前对话
  /ai status          — 查看会话状态
  /ai stats           — 查看使用统计
  /ai tools           — 列出可用工具
  @机器人 <消息>       — 在群聊中 @机器人 触发

配置示例（config.yaml plugins.ai 节）：
  ai:
    provider: "openai"                    # openai / anthropic
    model: "gpt-4o-mini"                  # 模型名称
    base_url: "https://api.openai.com/v1"  # API 地址
    api_key: "${AI_API_KEY}"              # API Key
    system_prompt: "你是一个有用的AI助手"   # 系统提示词`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			cfg := loadConfig(ctx)
			prov, err := NewProvider(cfg)
			if err != nil {
				return nil, fmt.Errorf("ai: create provider: %w", err)
			}

			var store SessionStore = &noopSessionStore{}
			if !ctx.DryRun {
				if storageSvc, ok := plugin.TryService[*infrastorage.Plugin](ctx, "storage"); ok {
					if s, err := NewGormSessionStore(storageSvc); err == nil {
						store = s
						ctx.Log.Info("Session persistence enabled via storage plugin")
					} else {
						ctx.Log.Warnf("Failed to init session store: %v, using in-memory only", err)
					}
				} else {
					ctx.Log.Info("Storage plugin not available, using in-memory session storage")
				}
			}

			coord := ctx.Info.Coordinator()

			p := &Plugin{
				cfg:         cfg,
				coord:       coord,
				syncer:      syncer,
				prov:        prov,
				reg:         NewToolRegistry(),
				sm:          NewSessionManager(1000, cfg.MaxHistory, cfg.SessionTTL, store),
				cmdPatterns: make(map[string]string),
				skillReg:    NewSkillRegistry(),
			}

			p.discoverTools()
			p.registerHandlers(ctx)

			// 多模态配置警告
			if cfg.VisionEnabled || cfg.AudioEnabled {
				nonVisionModels := []string{"deepseek-chat", "deepseek-reasoner", "gpt-3.5-turbo", "gpt-3.5-turbo-16k"}
				model := strings.ToLower(cfg.Model)
				for _, nm := range nonVisionModels {
					if strings.Contains(model, nm) {
						ctx.Log.Warnf("[AI] Model %q may not support multimodal input (vision=%v audio=%v), "+
							"set vision_enabled=false or audio_enabled=false if errors occur", cfg.Model, cfg.VisionEnabled, cfg.AudioEnabled)
						break
					}
				}
			}

			ctx.Spawn(func(runCtx context.Context) {
				ticker := time.NewTicker(10 * time.Minute)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						p.sm.CleanupExpired()
					case <-runCtx.Done():
						return
					}
				}
			})

			return p, nil
		},
	}
}

// HealthCheckers 返回 AI 提供商 API 端点可达性检查器，实现 health.CheckProvider。
func (p *Plugin) HealthCheckers() []health.Checker {
	probeName := "ai-" + p.cfg.Provider
	probeURL := buildHealthProbeURL(p.cfg)
	opts := []health.APIProbeOption{
		health.WithHeader("Authorization", "Bearer "+p.cfg.APIKey),
		health.WithAcceptStatus(func(code int) bool {
			return code >= 200 && code < 500
		}),
	}
	return []health.Checker{
		health.NewAPIProbe(probeName, probeURL, 5*time.Second, opts...),
	}
}

// buildHealthProbeURL 根据提供商类型构造健康检查用的探测 URL。
func buildHealthProbeURL(cfg *Config) string {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	switch cfg.Provider {
	case "anthropic":
		if strings.HasSuffix(baseURL, "/v1") {
			return baseURL + "/messages"
		}
		return baseURL + "/v1/messages"
	default:
		return baseURL + "/models"
	}
}

// buildAIDefinition 构建 AI 命令定义，包含子命令和可选的消息参数。
func buildAIDefinition() *command.Definition {
	return command.NewDef("ai").
		SubCommand(command.NewDef("reset").Build()).
		SubCommand(command.NewDef("undo").Build()).
		SubCommand(command.NewDef("retry").Build()).
		SubCommand(command.NewDef("summary").Build()).
		SubCommand(command.NewDef("status").Build()).
		SubCommand(command.NewDef("stats").Build()).
		SubCommand(command.NewDef("tools").Description("列出可用工具").Build()).
		Build()
}

// registerHandlers 注册 AI 对话的触发处理器。
//
// 支持三种触发方式（可组合）：
//   - 命令前缀（如 /ai），通过 command.Definition 定义子命令
//   - @机器人 正则匹配
//   - 私聊自动响应
//
// ⚠️ 私聊自动响应会过滤以 "/" 开头的命令消息，避免与现有命令的
// handler 并发争抢 ctx.SetStdContext（见 Timeout 中间件实现），
// 防止因竞态条件导致命令执行中 context 被意外取消。
func (p *Plugin) registerHandlers(ctx *plugin.SetupContext) {
	trigger := p.cfg.TriggerCmd
	if trigger != "" {
		p.triggerCmd = trigger
		def := buildAIDefinition()
		ctx.OnCommandDefWith("", trigger, def, p.handleAI)
	}

	if p.cfg.AtBot {
		ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage)).
			Where(eventctx.OnMentionedBotOrNoMentions()).
			Handle(p.handleAI)
	}

	if p.cfg.PrivateChat {
		ctx.Reg.RegisterMatcher(string(platform.EventKindPrivateMessage)).
			Where(func(c *eventctx.Context) bool {
				return !isCommandMessage(c.GetMessageContent())
			}).
			Handle(p.handleAI)
	}
}
