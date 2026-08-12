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
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
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
	cmdMu       sync.RWMutex
	cmdPatterns map[string]string
	skillReg    *SkillRegistry

	// summaryMu / summaries 防止同一会话重复触发 /ai summary 产生无界后台 goroutine。
	summaryMu sync.Mutex
	summaries map[string]bool

	// history 消息历史提供者（messagelog），用于回复上下文与群聊最近消息窗口。
	// 默认 messagelog.Default()；messagelog 未启用时查询为空，相关功能自动降级为 no-op。
	history *messagelog.Logger

	fsmEngine       *fsm.Engine
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc

	// reminders 定时提醒管理器（/ai remind）。
	// 依赖 plugin.SessionNotifier 主动推送；进程内存储，重启后失效。
	reminders *reminderManager

	// approvals 命令执行审批管理器（tool_approval）。
	approvals *approvalManager

	// groupPolicies per-group 工具策略/提示词（/ai group）。
	// LevelDB 持久化（data/ai）；store 为 nil 时纯内存（测试场景）。
	groupPolicies *groupPolicyManager

	// emb 文本向量缓存（embedding_base_url 配置时启用）。
	// 工具选择与记忆检索共用；为 nil 时两者均退化为纯关键词打分。
	emb *textVectorCache

	// memory 长期事实记忆（memory_enabled 配置时启用）。
	// LevelDB 持久化（data/ai_memory）；store 为 nil 时功能关闭。
	memory *memoryStore

	// realCmdMu 并行工具执行时真实命令路径（syncer）的串行化互斥。
	realCmdMu sync.Mutex
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
  /ai trace           — 查看工具调用追踪（耗时/参数/错误）
  /ai tools           — 列出可用工具
  /ai remind 5分钟 去喝水 — 设置定时提醒（到期主动推送）
  /ai approve <ID>    — 批准工具执行（/ai deny <ID> 拒绝）
  /ai group           — 管理本群 AI 策略（提示词/工具白名单/审批/@触发）
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

			// 群策略存储：优先使用 data/ai 目录（LevelDB），失败时降级为纯内存。
			var groupPolicies *groupPolicyManager
			if !ctx.DryRun {
				gp, gpErr := OpenGroupPolicyStore("data")
				if gpErr != nil {
					ctx.Log.Warnf("Failed to open group policy store: %v, using in-memory only", gpErr)
					groupPolicies = newGroupPolicyManager(nil, "")
				} else {
					groupPolicies = gp
				}
			} else {
				groupPolicies = newGroupPolicyManager(nil, "")
			}

			// 语义检索基础设施（embedding_base_url 配置时启用）：
			// 文本向量按内容惰性嵌入并缓存，查询向量每轮一次；失败自动降级纯关键词。
			// 工具选择与记忆检索共用同一缓存实例（相同文本不重复嵌入）。
			var emb *textVectorCache
			if cfg.EmbeddingBaseURL != "" {
				embKey := cfg.EmbeddingAPIKey
				if embKey == "" {
					embKey = cfg.APIKey
				}
				if e := newOpenAIEmbedder(cfg.EmbeddingBaseURL, embKey, cfg.EmbeddingModel); e != nil {
					emb = newTextVectorCache(e)
					ctx.Log.Infof("AI semantic retrieval enabled (embedding model %s)", e.Model())
				} else {
					ctx.Log.Warn("Invalid embedding_base_url, semantic retrieval disabled")
				}
			}

			// 长期事实记忆（memory_enabled 配置时启用）：独立 LevelDB 目录，
			// 避免与群策略共用 data/ai 触发 LevelDB 锁冲突。
			var memory *memoryStore
			if cfg.MemoryEnabled && !ctx.DryRun {
				mem, memErr := OpenMemoryStore("data", cfg.MemoryMaxFacts, cfg.MemoryMinInterval)
				if memErr != nil {
					ctx.Log.Warnf("Failed to open memory store: %v, memory disabled", memErr)
				} else {
					memory = mem
					memory.SetEmbedder(emb)
					ctx.Log.Info("AI long-term memory enabled (auto-extract, data/ai_memory)")
				}
			}

			p := &Plugin{
				cfg:             cfg,
				coord:           coord,
				syncer:          syncer,
				prov:            prov,
				reg:             NewToolRegistry(),
				sm:              NewSessionManager(1000, cfg.MaxHistory, cfg.SessionTTL, store),
				cmdPatterns:     make(map[string]string),
				skillReg:        NewSkillRegistry(),
				summaries:       make(map[string]bool),
				history:         messagelog.Default(),
				fsmEngine:       fsm.NewEngine(nil),
				lifecycleCtx:    lifecycleCtx,
				lifecycleCancel: lifecycleCancel,
				reminders:       newReminderManager(),
				approvals:       newApprovalManager(),
				groupPolicies:   groupPolicies,
				emb:             emb,
				memory:          memory,
			}

			p.registerSkillAddFSM()

			// 规划层内置工具（create_plan / update_plan_step，general 类别恒被选中）。
			// 模型按需调用：简单任务不创建计划零开销；复杂任务先建计划再逐步执行。
			for _, t := range buildPlanTools(cfg.PlanMaxSteps) {
				p.reg.Register(t)
			}

			// 消息发送工具（send_message / send_to，默认启用）：
			// send_message 无需审批；send_to 强制审批 + ai.message.send 权限。
			for _, t := range p.buildSendTools() {
				p.reg.Register(t)
			}

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
				// 插件 Teardown：停止全部待触发的定时提醒
				defer p.reminders.stopAll()
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
		Teardown: func(tctx *plugin.TeardownContext) error {
			if p := tctx.API.(*Plugin); p != nil {
				if p.groupPolicies != nil {
					p.groupPolicies.Close()
				}
				if p.memory != nil {
					p.memory.Close()
				}
			}
			return nil
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
					for _, att := range platform.Attachments(fsmCtx.GetPlatformEvent()) {
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
		SubCommand(command.NewDef("trace").Description("查看工具调用追踪（最近 50 条）").Build()).
		SubCommand(command.NewDef("tools").Description("列出可用工具").Alias("help").Build()).
		SubCommand(command.NewDef("remind").Description("设置定时提醒（如：remind 5分钟 去喝水）").
			SubCommand(command.NewDef("list").Description("列出本会话的提醒").Build()).
			SubCommand(command.NewDef("cancel").Description("取消指定提醒，后接提醒 ID").Build()).
			Build()).
		SubCommand(command.NewDef("approve").Description("批准工具执行（后接审批 ID，如：approve A1）").Build()).
		SubCommand(command.NewDef("deny").Description("拒绝工具执行（后接审批 ID，如：deny A1）").Build()).
		SubCommand(
			command.NewDef("group").Description("管理本群 AI 策略（工具白名单/提示词/审批模式/@触发）").
				SubCommand(command.NewDef("status").Description("查看本群生效配置").Build()).
				SubCommand(command.NewDef("set").Description("设置本群配置").
					SubCommand(command.NewDef("prompt").Description("设置群提示词，后接文本").Build()).
					SubCommand(command.NewDef("tools").Description("设置群工具白名单：all|none|工具名,工具名").Build()).
					SubCommand(command.NewDef("approval").Description("设置群审批模式：off|restricted|always").Build()).
					SubCommand(command.NewDef("mention").Description("设置群 @ 触发要求：on|off").Build()).
					Build()).
				SubCommand(command.NewDef("reset").Description("重置群配置：reset [prompt|tools|approval|mention|all]").Build()).
				SubCommand(command.NewDef("global").Description("管理全局默认策略（需超级管理员）").
					SubCommand(command.NewDef("status").Description("查看全局配置").Build()).
					SubCommand(command.NewDef("reset").Description("重置全局配置").Build()).
					Build()).
				Build(),
		).
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
// 支持四种触发方式（可组合）：
//   - 命令前缀（如 /ai），通过 command.Definition 定义子命令
//   - @机器人 正则匹配
//   - 群聊自主发言（group_autonomous）：不 @ 也响应群内非命令消息
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

	// 审批按钮回调（EventKindInteraction）：处理 /ai approve|deny 按钮点击。
	// 按钮 ID 形如 "ai:approve:A1" / "ai:deny:A1"（见 approval.go）。
	ctx.Reg.RegisterMatcher(string(platform.EventKindInteraction)).Handle(p.handleApprovalButton)

	if p.cfg.GroupAutonomous {
		// 群聊自主发言：不 @ 机器人也响应群内非命令消息。
		// 等价官方 OpenClaw 插件的 requireMention=false。
		// 注意：此模式覆盖 @机器人 的群聊路径（@ 消息也命中），
		// 因此开启时不再单独注册 AtBot 群聊 matcher，避免重复处理。
		ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage)).
			Where(func(c *eventctx.Context) bool {
				return !isCommandMessage(c.GetMessageContent())
			}).
			Handle(p.handleAI)
	} else if p.cfg.AtBot {
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

	// per-group @ 触发要求（/ai group set mention off）：群策略显式允许自主发言时，
	// 即使全局 at_bot=true 也放行未 @ 的消息。@ 机器人时仍需 @（OnMentionedBot
	// 规则已覆盖），此处仅兜底"未 @ 但群策略允许自主"的路径。
	//
	// 全局 GroupAutonomous=true 时不注册此兜底：自主 matcher 已覆盖全部
	// 非命令消息，群策略 mention=on 的"必须 @"限制在 handleAI 内按群过滤。
	if !p.cfg.GroupAutonomous {
		ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage)).
			Where(func(c *eventctx.Context) bool {
				require, ok := p.groupRequireMention(c)
				// 群策略未配置或要求 @ 时，本兜底 matcher 不处理（由上方 matcher 路由）
				if !ok || require {
					return false
				}
				// 群策略允许自主发言：放行未 @ 且非命令的消息
				return !eventctx.OnMentionedBot()(c) && !isCommandMessage(c.GetMessageContent())
			}).
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
