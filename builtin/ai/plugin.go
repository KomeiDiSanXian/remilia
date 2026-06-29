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
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
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
//   - fsmEngine: 内置 FSM 引擎，用于技能注册等两步对话流程
//   - lifecycleCtx: 插件生命周期上下文，插件关闭时取消，用于替代 context.Background()
//   - lifecycleCancel: 取消 lifecycleCtx 的函数
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

	fsmEngine       *fsm.Engine
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
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

			lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())

			p := &Plugin{
				cfg:             cfg,
				coord:           coord,
				syncer:          syncer,
				prov:            prov,
				reg:             NewToolRegistry(),
				sm:              NewSessionManager(1000, cfg.MaxHistory, cfg.SessionTTL, store),
				cmdPatterns:     make(map[string]string),
				skillReg:        NewSkillRegistry(),
				fsmEngine:       fsm.NewEngine(nil),
				lifecycleCtx:    lifecycleCtx,
				lifecycleCancel: lifecycleCancel,
			}

			p.registerSkillAddFSM()

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

			p.fsmEngine.StartCleanup(5*time.Minute, lifecycleCtx.Done())

			ctx.Spawn(func(runCtx context.Context) {
				defer p.lifecycleCancel()
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

// registerSkillAddFSM 注册技能添加两步对话的 FSM。
// 状态机管理用户从 /ai skill add <name> 到发送内容的流程。
func (p *Plugin) registerSkillAddFSM() {
	skillAddFSM := &fsm.FSM{
		Name:    "skill_add",
		Initial: "awaiting_content",
		Timeout: 10 * time.Minute,
		Events: []fsm.Event{
			{
				Name: "cancel",
				From: "*",
				To:   "",
				Match: func(ctx *eventctx.Context) bool {
					content := strings.TrimSpace(ctx.GetMessageContent())
					lower := strings.ToLower(content)
					return lower == "cancel" || lower == "取消" || lower == "c"
				},
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Reply(platform.TextMessage("❌ 技能注册已取消。"))
					return nil
				},
			},
			{
				Name: "submit",
				From: "awaiting_content",
				To:   "",
				Match: func(ctx *eventctx.Context) bool {
					content := strings.TrimSpace(ctx.GetMessageContent())
					if content == "" {
						return false
					}
					lower := strings.ToLower(content)
					return lower != "cancel" && lower != "取消" && lower != "c"
				},
				Action: func(fsmCtx *fsm.FSMContext) error {
					name, _ := fsmCtx.Data["name"].(string)
					ownerID, _ := fsmCtx.Data["ownerID"].(string)
					if name == "" || ownerID == "" {
						fsmCtx.Reply(platform.TextMessage("❌ 技能注册数据丢失，请重新开始。"))
						return nil
					}

					prompt := strings.TrimSpace(fsmCtx.GetMessageContent())
					for _, att := range fsmCtx.GetPlatformEvent().Attachments() {
						if strings.HasPrefix(att.MimeType, "text/") || strings.HasSuffix(att.URL, ".md") {
							if content := p.downloadTextAttachment(fsmCtx.Context.Context(), att); content != "" {
								prompt = content
								break
							}
						}
					}
					if prompt == "" {
						fsmCtx.Reply(platform.TextMessage("❌ 技能内容为空，注册已取消。"))
						return nil
					}

					desc := extractSkillDescription(prompt)
					skill := Skill{
						Name: name, Description: desc, Prompt: prompt, Enabled: true,
					}
					if err := p.RegisterUserSkill(skill, ownerID); err != nil {
						fsmCtx.Reply(platform.TextMessage("❌ " + err.Error()))
					} else {
						fsmCtx.Reply(platform.TextMessage(
							fmt.Sprintf("✅ 技能 `%s%s` 已注册！> %s", UserSkillPrefix, name, desc)))
					}
					return nil
				},
			},
		},
	}
	if err := p.fsmEngine.Register(skillAddFSM); err != nil {
		logger.Errorf("[AI] Failed to register skill_add FSM: %v", err)
	}
}

// makeSkillAddSessionID 生成技能添加 FSM 的会话 ID（按用户隔离）。
func makeSkillAddSessionID(ctx *eventctx.Context) string {
	return fmt.Sprintf("skill_add:%s:%s", ctx.GetEventPlatform(), ctx.GetSenderInfo().ID)
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
//
// 子命令支持嵌套（如 /ai skill add），帮助插件会自动展开显示。
func buildAIDefinition() *command.Definition {
	return command.NewDef("ai").
		SubCommand(command.NewDef("reset").Description("清空对话历史").Build()).
		SubCommand(command.NewDef("undo").Description("撤销上一条对话").Build()).
		SubCommand(command.NewDef("retry").Description("重新生成上一条回复").Build()).
		SubCommand(command.NewDef("summary").Description("总结当前对话").Build()).
		SubCommand(command.NewDef("status").Description("查看会话状态").Build()).
		SubCommand(command.NewDef("stats").Description("查看使用统计").Build()).
		SubCommand(command.NewDef("tools").Description("列出可用工具").Alias("help").Build()).
		SubCommand(
			command.NewDef("skill").Description("管理自定义技能").
				SubCommand(command.NewDef("add").Description("注册新技能，后接技能名称和 Markdown 内容，或发送 .md 附件").Build()).
				SubCommand(command.NewDef("list").Description("列出我的所有技能及状态").Build()).
				SubCommand(command.NewDef("remove").Description("删除指定技能").Build()).
				SubCommand(command.NewDef("enable").Description("启用指定技能").Build()).
				SubCommand(command.NewDef("disable").Description("禁用指定技能").Build()).
				SubCommand(command.NewDef("promote").Description("将用户技能提升为系统级（需管理员权限）").Build()).
				SubCommand(command.NewDef("info").Description("查看技能详情和 Prompt 预览").Build()).
				Build(),
		).
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
		if trigger != "" {
			// trigger_cmd 已设置：@机器人 时仅响应带触发前缀的消息
			ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage)).
				Where(eventctx.OnMentionedBot()).
				Where(eventctx.OnCommand(trigger)).
				Handle(p.handleAI)
		} else {
			// 无 trigger_cmd：@机器人 时响应任意消息（排除命令）
			ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage)).
				Where(eventctx.OnMentionedBot()).
				Where(func(c *eventctx.Context) bool {
					return !isCommandMessage(c.GetMessageContent())
				}).
				Handle(p.handleAI)
		}
	}

	if p.cfg.PrivateChat {
		ctx.Reg.RegisterMatcher(string(platform.EventKindPrivateMessage)).
			Where(func(c *eventctx.Context) bool {
				return !isCommandMessage(c.GetMessageContent())
			}).
			Handle(p.handleAI)
	}
}
