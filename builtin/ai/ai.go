package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/vevent"
	"github.com/KomeiDiSanXian/remilia/command"
)

// DefaultConfig AI 插件默认配置。
var DefaultConfig = Config{
	Provider:      "openai",
	Model:         "gpt-4o-mini",
	MaxTokens:     2048,
	MaxDepth:      5,
	MaxHistory:    20,
	Temperature:   0.7,
	TopP:          1.0,
	APITimeout:    60 * time.Second,
	MaxRetries:    0,
	ToolTimeout:   30 * time.Second,
	SessionTTL:    24 * time.Hour,
	SystemPrompt:  "你是一个有用的AI助手，运行在一个叫Remilia Bot的IM框架中。用户问你有什么工具时请列举可用的工具。",
	TriggerCmd:    "/ai",
	AtBot:         true,
	PrivateChat:   true,
	Markdown:      true,
	Fallback:      false,
	SkillTimeout:  60 * time.Second,
	SkillMaxDepth: 3,
}

// Plugin AI 对话插件的主结构体。
// 管理会话、工具注册、LLM 提供商调用。
type Plugin struct {
	cfg        *Config
	coord      engine.Reader
	syncer     vevent.EventProcessor
	sm         *SessionManager
	reg        *ToolRegistry
	prov       Provider
	triggerCmd string
	// cmdPatterns 工具名 → 完整命令模式（如 "ping" → "/ping"），
	// 用于 executeTool 时构造正确的命令文本
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

			// 初始化会话持久化存储
			// 依赖 storage 插件（可选），无存储插件时使用内存存储
			var store SessionStore = &noopSessionStore{}
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

			// 自动扫描已注册命令并将其包装为工具
			p.discoverTools()
			// 注册消息处理器
			p.registerHandlers(ctx)

			// 后台 goroutine：定期清理过期会话
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

// loadConfig 从插件配置中读取配置项，未配置时使用默认值。
//
// 当前配置校验：
//   - 至少启用一种触发方式（trigger_cmd / at_bot / private_chat）
//   - provider、model、api_key 等由 Setup 阶段 NewProvider 校验
func loadConfig(ctx *plugin.SetupContext) *Config {
	cfg := DefaultConfig
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

	if cfg.TriggerCmd == "" && !cfg.AtBot && !cfg.PrivateChat {
		ctx.Log.Warn("No trigger method enabled: set trigger_cmd, at_bot, or private_chat in config")
	}

	return &cfg
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
		ctx.Reg.RegisterRegex("", `^\s*@.*?(\s+.*)?$`).Handle(p.handleAI)
	}

	if p.cfg.PrivateChat {
		ctx.Reg.RegisterMatcher(string(platform.EventKindPrivateMessage)).
			Where(func(c *eventctx.Context) bool {
				return !isCommandMessage(c.GetMessageContent())
			}).
			Handle(p.handleAI)
	}
}

// isCommandMessage 判断消息是否为命令消息。
//
// 使用 [context.SplitCommandPattern] 检测消息的首个单词是否带有
// 非字母数字前缀（如 "/"、"!"、"!!"、"$#" 等），有则视为命令消息。
//
// 这覆盖了所有自定义命令前缀场景，与框架的命令路由逻辑保持一致。
func isCommandMessage(msg string) bool {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return false
	}
	// 取第一个空白分隔的单词
	firstWord := trimmed
	if idx := strings.IndexFunc(trimmed, unicode.IsSpace); idx != -1 {
		firstWord = trimmed[:idx]
	}
	prefix, _ := eventctx.SplitCommandPattern(firstWord)
	return prefix != ""
}

// discoverTools 自动扫描已注册的**无权限**命令并包装为 LLM 工具。
//
// # 安全设计
//
// ⚠️ 仅自动发现不需要任何权限的命令（Permissions 为空）。
// 需要权限的命令不会被 AI 自动发现，防止通过 AI 绕过权限检查。
//
// 对于需要 AI 调用的权限命令，插件应在自己的 Setup 中调用
// [Plugin.RegisterToolProvider] 显式注册工具，并在 Execute 中自行校验身份。
//
// # 工作原理
//
//  1. 通过 engine.Reader.GetAllCommands() 获取所有命令列表
//  2. 跳过 AI 自身命令、隐藏命令、需要权限的命令
//  3. 每个安全命令生成一个 Tool 供 LLM 调用
//
// 限制：在 Setup 阶段调用，仅能发现已注册的无权限命令。
func (p *Plugin) discoverTools() {
	if p.coord == nil {
		return
	}

	allCmds := p.coord.GetAllCommands()
	for _, cmd := range allCmds {
		if cmd.Definition != nil && cmd.Definition.Hidden {
			continue
		}
		if !isCommandSafeForAI(cmd) {
			continue
		}
		tool := buildToolFromCommand(cmd)
		if tool != nil {
			p.cmdPatterns[tool.Name] = cmd.Command
			p.reg.Register(*tool)
		}
	}
}

// RegisterToolProvider 注册一个实现了 ToolProvider 接口的插件所提供的工具集。
//
// 其他插件可在自己的 Setup 中通过 plugin.TryService 获取 AI 插件的服务实例
// 后调用此方法注册自定义工具，尤其是需要权限校验的敏感命令。
//
// 使用示例：
//
//	if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
//	    aiSvc.RegisterToolProvider(myToolProvider)
//	}
func (p *Plugin) RegisterToolProvider(tp ToolProvider) {
	for _, t := range tp.ListTools() {
		p.reg.Register(t)
	}
}

// DiscoverToolProviders 扫描插件管理器中所有已注册的插件服务，
// 自动发现实现了 [ToolProvider] 接口的插件并注册其工具。
// 应在 [plugin.Manager.FreezeContainer] 之后调用。
func (p *Plugin) DiscoverToolProviders(mgr *plugin.Manager) {
	for _, name := range mgr.List() {
		svc, ok := mgr.GetContainer().Get(name)
		if !ok || svc == nil {
			continue
		}
		tp, ok := svc.(ToolProvider)
		if !ok {
			continue
		}
		p.RegisterToolProvider(tp)
	}
}

// RegisterSkill 注册一个 Skill。
// Skill 会自动包装为 Tool 供 LLM 发现和调用。
// 如果 Parameters 为空，自动使用 {"query": string} 作为默认参数。
func (p *Plugin) RegisterSkill(s Skill) {
	if len(s.Parameters.Properties) == 0 {
		s.Parameters = ToolParamSchema{
			Type: "object",
			Properties: map[string]ToolParamSchema{
				"query": {Type: "string", Description: "需要该技能处理的问题"},
			},
			Required: []string{"query"},
		}
	}
	p.skillReg.Register(s)
	skill := s
	p.reg.Register(Tool{
		Name:        skill.Name,
		Description: skill.Description,
		Parameters:  skill.Parameters,
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return p.executeSkill(ctx, skill, args)
		},
	})
}

// RegisterSkillProvider 注册一个实现了 SkillProvider 接口的插件所提供的技能集。
func (p *Plugin) RegisterSkillProvider(sp SkillProvider) {
	for _, s := range sp.ListSkills() {
		p.RegisterSkill(s)
	}
}

// DiscoverSkillProviders 扫描插件管理器中所有已注册的插件服务，
// 自动发现实现了 [SkillProvider] 接口的插件并注册其技能。
// 应在 [plugin.Manager.FreezeContainer] 之后调用。
func (p *Plugin) DiscoverSkillProviders(mgr *plugin.Manager) {
	for _, name := range mgr.List() {
		svc, ok := mgr.GetContainer().Get(name)
		if !ok || svc == nil {
			continue
		}
		if sp, ok := svc.(SkillProvider); ok {
			p.RegisterSkillProvider(sp)
		}
	}
}

// truncateText 截断文本到指定长度，超过时末尾添加 "..."

// isCommandSafeForAI 判断命令是否能安全地暴露给 AI。
//
// 安全条件（全部满足）：
//  1. 命令无权限要求（Permissions 为空）
//  2. 命令定义中也无权限要求（Definition.Permissions 为空）
//  3. 不是 AI 自身命令
//
// 不满足任一条件 → AI 不可调用该命令。
func isCommandSafeForAI(cmd engine.CommandInfo) bool {
	name := strings.TrimLeft(cmd.Command, "/!$#")
	if name == "" || name == "ai" {
		return false
	}
	if len(cmd.Permissions) > 0 {
		return false
	}
	if cmd.Definition != nil && len(cmd.Definition.Permissions) > 0 {
		return false
	}
	return true
}

// buildToolFromCommand 将命令信息转换为 LLM 工具。
//
// 注意：调用方应确保已通过 isCommandSafeForAI 前置检查。
func buildToolFromCommand(cmd engine.CommandInfo) *Tool {
	name := strings.TrimLeft(cmd.Command, "/!$#")
	if name == "" {
		return nil
	}
	name = strings.ReplaceAll(name, " ", "_")

	desc := cmd.Description
	if desc == "" {
		desc = fmt.Sprintf("执行命令 %s", cmd.Command)
	}

	return &Tool{
		Name:        name,
		Description: desc,
		Parameters: ToolParamSchema{
			Type:       "object",
			Properties: make(map[string]ToolParamSchema),
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("[命令 %s 已触发]", cmd.Command), nil
		},
	}
}

// execSubCommand 根据子命令名称执行对应的操作。
// 用于 /ai 命令路径（通过 GetParsedCommand 获取子命令名）。
func (p *Plugin) execSubCommand(ctx *eventctx.Context, subCmd string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	sessionID := makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)

	switch subCmd {
	case "reset":
		p.sm.Delete(sessionID)
		return ctx.ReplyText("✅ 对话历史已清空，开始全新的对话吧！")

	case "undo":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("没有可以撤销的对话")
		}
		lastUserIdx := -1
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if session.Messages[i].Role == RoleUser {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx <= 0 {
			return ctx.ReplyText("没有可以撤销的对话")
		}
		session.Messages = session.Messages[:lastUserIdx]
		p.sm.Save(session)
		return ctx.ReplyText("↩️ 已撤销上一条对话")

	case "retry":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("没有可以重试的对话")
		}
		lastAssistantIdx := -1
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if session.Messages[i].Role == RoleAssistant {
				lastAssistantIdx = i
				break
			}
		}
		if lastAssistantIdx < 0 {
			return ctx.ReplyText("没有可以重试的对话")
		}
		session.Messages = session.Messages[:lastAssistantIdx]
		p.sm.Save(session)

		reply, err := p.processWithTools(ctx, session)
		if err != nil {
			return ctx.ReplyText(formatAIError(err))
		}
		if reply != "" {
			if p.cfg.Markdown && ctx.GetPlatformCapabilities().Markdown {
				_, err := ctx.Reply(platform.MarkdownMessage(reply))
				return err
			}
			_, err := ctx.Reply(platform.TextMessage(reply))
			return err
		}
		return nil

	case "summary":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("还没有任何对话内容可以总结")
		}
		msgsSnapshot := make([]Message, len(session.Messages))
		copy(msgsSnapshot, session.Messages)
		go p.doSummary(ctx, msgsSnapshot)
		return ctx.ReplyText("⏳ 正在生成对话总结，请稍候...")

	case "status":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("当前没有活跃的对话")
		}
		msgCount := len(session.Messages)
		sysCount := 0
		for _, m := range session.Messages {
			if m.Role == RoleSystem {
				sysCount++
			}
		}
		duration := time.Since(session.CreatedAt)
		var b strings.Builder
		b.WriteString("📊 **对话状态**\n\n")
		b.WriteString(fmt.Sprintf("  - 提供商：`%s`\n", p.cfg.Provider))
		b.WriteString(fmt.Sprintf("  - 模型：`%s`\n", p.cfg.Model))
		b.WriteString(fmt.Sprintf("  - 消息数：`%d`（含 %d 条系统提示）\n", msgCount, sysCount))
		b.WriteString(fmt.Sprintf("  - 对话时长：`%s`\n", formatDuration(duration)))
		b.WriteString(fmt.Sprintf("  - 会话 ID：`%s`\n", sessionID))
		return ctx.ReplyText(b.String())

	case "stats":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			return ctx.ReplyText("当前没有活跃的对话")
		}
		var b strings.Builder
		b.WriteString("📈 **使用统计**\n\n")
		b.WriteString(fmt.Sprintf("  - LLM 调用次数：`%d`\n", session.CallCount))
		b.WriteString(fmt.Sprintf("  - 工具调用次数：`%d`\n", session.ToolCount))
		return ctx.ReplyText(b.String())

	case "tools", "help":
		var b strings.Builder
		b.WriteString("我可以使用以下工具：\n\n")
		tools := p.reg.List()
		if len(tools) == 0 {
			b.WriteString("（当前没有可用工具）")
		} else {
			for _, t := range tools {
				b.WriteString(fmt.Sprintf("  - **%s**：%s\n", t.Name, t.Description))
			}
		}
		b.WriteString(fmt.Sprintf("\n在对话中直接告诉我你想使用哪个工具即可。\n"))
		b.WriteString(fmt.Sprintf("\n**子命令：**"))
		b.WriteString(fmt.Sprintf("\n  `%s reset` — 清空对话历史", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s undo` — 撤销上一条对话", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s retry` — 重新生成上一条回复", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s summary` — 总结当前对话", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s status` — 查看会话状态", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s stats` — 查看使用统计", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s tools` — 列出可用工具", p.cfg.TriggerCmd))
		return ctx.ReplyText(b.String())
	}
	return nil
}

// handleSubCommand 处理 @bot/私聊路径的子命令，通过内容字符串匹配。
// 返回 true 表示已处理。
func (p *Plugin) handleSubCommand(ctx *eventctx.Context, content string) bool {
	cmd := strings.ToLower(strings.TrimSpace(content))
	switch cmd {
	case "reset", "重置":
		p.execSubCommand(ctx, "reset")
		return true
	case "undo":
		p.execSubCommand(ctx, "undo")
		return true
	case "retry", "重试":
		p.execSubCommand(ctx, "retry")
		return true
	case "summary", "总结":
		p.execSubCommand(ctx, "summary")
		return true
	case "status":
		p.execSubCommand(ctx, "status")
		return true
	case "stats":
		p.execSubCommand(ctx, "stats")
		return true
	case "tools", "工具", "help", "帮助":
		p.execSubCommand(ctx, "tools")
		return true
	}
	return false
}

// formatDuration 将 time.Duration 格式化为人类可读的字符串。
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// doSummary 在后台调用 LLM 生成对话总结，通过原始 sender 发送结果。
// msgs 是调用方已复制的消息快照，不会与 session 管理器产生 data race。
func (p *Plugin) doSummary(origCtx *eventctx.Context, msgs []Message) {
	filtered := make([]Message, 0, len(msgs)+1)
	for _, m := range msgs {
		if m.Role != RoleSystem {
			filtered = append(filtered, m)
		}
	}
	filtered = append(filtered, Message{
		Role:    RoleUser,
		Content: "请用简短的几句话总结以上对话的要点。",
	})

	req := &ChatRequest{
		Model:       p.cfg.Model,
		Messages:    filtered,
		Temperature: p.cfg.Temperature,
		TopP:        p.cfg.TopP,
	}

	resp, err := p.prov.Chat(context.Background(), req)
	if err != nil {
		newCtx := eventctx.NewContextFromEvent(origCtx.GetPlatformEvent(), origCtx.GetPlatformSender())
		newCtx.ReplyText("❌ 生成总结失败: " + formatAIError(err))
		return
	}

	if resp.Content != "" {
		newCtx := eventctx.NewContextFromEvent(origCtx.GetPlatformEvent(), origCtx.GetPlatformSender())
		newCtx.ReplyText("📋 **对话总结**\n\n" + resp.Content)
	}
}

// handleAI AI 消息处理器的入口。
//
// 处理流程：
//  1. 若通过 /ai 命令触发，使用 GetParsedCommand 检测子命令
//  2. 若通过 @bot 或私聊触发，使用 cleanMessage 清洗后检测子命令
//  3. 均非子命令时进入 AI 对话流程
//  4. 注入/更新系统提示词
//  5. 追加用户消息到会话
//  6. 进入工具调用循环（processWithTools）
//  7. 发送最终回复
func (p *Plugin) handleAI(ctx *eventctx.Context) error {
	parsed := ctx.GetParsedCommand()

	// /ai 命令路径：通过 command 包解析子命令
	if parsed != nil {
		if len(parsed.CommandPath) > 1 {
			return p.execSubCommand(ctx, parsed.CommandPath[1])
		}
		// 无子命令时，从原始消息中提取 AI 对话内容
		return p.handleAIChat(ctx, p.cleanMessage(ctx.GetMessageContent()))
	}

	// @bot / 私聊路径：手动清洗后检测子命令
	content := ctx.GetMessageContent()
	if content == "" {
		return nil
	}
	content = p.cleanMessage(content)
	if content == "" {
		return nil
	}
	if p.handleSubCommand(ctx, content) {
		return nil
	}
	if isCommandMessage(content) {
		return nil
	}

	return p.handleAIChat(ctx, content)
}

// handleAIChat 执行 AI 对话流程：获取/创建会话、注入系统提示、追加用户消息、调用 LLM。
func (p *Plugin) handleAIChat(ctx *eventctx.Context, content string) error {
	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()

	sessionID := makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)
	session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
	if session == nil {
		return ctx.ReplyError("创建会话失败")
	}

	if session.Messages == nil {
		session.Messages = make([]Message, 0)
	}

	var systemPrompt = p.cfg.SystemPrompt

	var foundSystem bool
	for i, m := range session.Messages {
		if m.Role == RoleSystem {
			session.Messages[i].Content = systemPrompt
			foundSystem = true
			break
		}
	}
	if !foundSystem {
		session.Messages = append([]Message{{Role: RoleSystem, Content: systemPrompt}}, session.Messages...)
	}

	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: content})

	reply, err := p.processWithTools(ctx, session)
	if err != nil {
		return ctx.ReplyError(formatAIError(err))
	}

	if reply != "" {
		if p.cfg.Markdown && ctx.GetPlatformCapabilities().Markdown {
			_, err := ctx.Reply(platform.MarkdownMessage(reply))
			return err
		}
		_, err := ctx.Reply(platform.TextMessage(reply))
		return err
	}
	return nil
}

// singleRoundResult 单轮非流式 LLM 调用的结果。
type singleRoundResult struct {
	Text      string
	ToolCalls []ToolCall
}

// runSingleRound 执行单轮非流式 LLM 调用，返回文本回复和工具调用。
// 不涉及 session 管理，纯函数式，供 executeSkill 内部循环使用。
func (p *Plugin) runSingleRound(ctx context.Context, messages []Message, tools []Tool) (*singleRoundResult, error) {
	req := &ChatRequest{
		Model:       p.cfg.Model,
		Messages:    messages,
		Tools:       tools,
		Temperature: p.cfg.Temperature,
		TopP:        p.cfg.TopP,
		MaxTokens:   p.cfg.MaxTokens,
	}

	resp, err := p.prov.Chat(ctx, req)
	if err != nil {
		return nil, err
	}

	for i := range resp.ToolCalls {
		if resp.ToolCalls[i].ID == "" {
			resp.ToolCalls[i].ID = fmt.Sprintf("call_%s_%d", resp.ToolCalls[i].Name, i)
		}
	}

	return &singleRoundResult{
		Text:      resp.Content,
		ToolCalls: resp.ToolCalls,
	}, nil
}

// processWithTools 执行 AI 对话的工具调用循环。
//
// 循环逻辑：
//  1. 发送会话消息 + 工具列表到 LLM
//  2. LLM 返回文本回复和/或工具调用请求
//  3. 有工具调用时：
//     a. 追加 assistant 消息（含 tool_calls）
//     b. 逐个执行工具，结果追加为 tool 消息
//     c. 回到步骤 1（递归深度 +1）
//  4. 无工具调用时返回最终文本
//
// maxDepth 防止无限循环，默认最多 5 轮。
func (p *Plugin) processWithTools(ctx *eventctx.Context, session *Session) (string, error) {
	currentDepth := 0
	maxDepth := p.cfg.MaxDepth
	partialContent := ""

	for currentDepth < maxDepth {
		currentDepth++

		session.CallCount++

		req := &ChatRequest{
			Model:       p.cfg.Model,
			Messages:    session.Messages,
			Tools:       p.reg.List(),
			Temperature: p.cfg.Temperature,
			TopP:        p.cfg.TopP,
		}

		streamCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.APITimeout)
		defer cancel()
		streamCh, err := p.prov.ChatStream(streamCtx, req)
		if err != nil {
			return partialContent, fmt.Errorf("chat stream: %w", err)
		}

		var fullResponse strings.Builder
		var toolCalls []ToolCall

		for event := range streamCh {
			switch event.Type {
			case StreamEventText:
				fullResponse.WriteString(event.Content)
			case StreamEventToolCall:
				if event.ToolCall != nil && event.ToolCall.Name != "" {
					toolCalls = append(toolCalls, *event.ToolCall)
				}
			case StreamEventError:
				return partialContent, event.Err
			case StreamEventDone:
			}
		}

		responseText := fullResponse.String()

		// 为 ID 为空的工具调用生成稳定 ID，确保 assistant 的 tool_calls
		// 和后续 tool 消息的 tool_call_id 一致。
		for i := range toolCalls {
			if toolCalls[i].ID == "" {
				toolCalls[i].ID = fmt.Sprintf("call_%s_%d", toolCalls[i].Name, i)
			}
		}

		// 无工具调用：返回最终文本
		if len(toolCalls) == 0 {
			if responseText != "" {
				p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: responseText})
			}
			return responseText, nil
		}

		// 有工具调用：追加 assistant 消息并执行工具
		p.sm.AppendMessage(session, Message{Role: RoleAssistant, Content: responseText, ToolCalls: toolCalls})

		for _, tc := range toolCalls {
			session.ToolCount++
			toolCtx, cancel := context.WithTimeout(ctx.Context(), p.cfg.ToolTimeout)
			defer cancel()
			toolResult := p.executeTool(ctx, tc, toolCtx)
			p.sm.AppendMessage(session, Message{
				Role:       RoleTool,
				Content:    toolResult,
				ToolCallID: tc.ID,
			})
		}

		partialContent = responseText
	}

	return partialContent, fmt.Errorf("超过最大工具调用深度 (%d)", maxDepth)
}

// formatAIError 将 provider 返回的错误转换为用户友好的提示。
//
// 常见错误映射：
//   - 401: API Key 无效或未配置
//   - 404: 模型名称错误或 API 地址不对
//   - 429: 速率限制
//   - timeout / context deadline: 请求超时（可检查网络或增大 timeout）
//     其他: 显示原始错误
func formatAIError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401"):
		return "API 认证失败，请检查 api_key 配置"
	case strings.Contains(msg, "404"):
		return "API 地址或模型名称错误，请检查 base_url 和 model 配置"
	case strings.Contains(msg, "429"):
		return "请求过于频繁，请稍后再试"
	case strings.Contains(msg, "context deadline exceeded"):
		return "请求超时，请检查网络连接或增大超时配置"
	case strings.Contains(msg, "connection refused"):
		return "无法连接 API 服务器，请检查 base_url 配置"
	case strings.Contains(msg, "no such host"):
		return "API 域名解析失败，请检查 base_url 配置"
	default:
		return fmt.Sprintf("AI 处理出错: %v", err)
	}
}

// captureSender 实现 platform.Sender，拦截 Send 调用并记录消息文本内容，
// 不转发给真实用户。命令 handler 的回复仅作为工具结果返回给 AI，
// 避免用户同时看到命令原始输出和 AI 总结两条消息。
type captureSender struct {
	platform.NoopSender
	captured string
}

func (s *captureSender) Send(_ context.Context, req platform.SendRequest) (platform.SendResult, error) {
	text := req.Message.Text
	if text == "" {
		text = req.Message.Markdown
	}
	if text != "" {
		s.captured = text
	}
	return platform.SendResult{}, nil
}

// executeTool 执行一个工具调用并返回结果字符串。
//
// toolCtx 是调用方传入的超时 context，用于限制工具执行的最长时间。
// 优先通过 vevent 触发真实命令执行并捕获其回复内容；
// 若捕获失败或工具无对应命令，回退到 tool.Execute 的占位结果。
func (p *Plugin) executeTool(ctx *eventctx.Context, tc ToolCall, toolCtx context.Context) string {
	if skill, ok := p.skillReg.Get(tc.Name); ok {
		result, err := p.executeSkill(toolCtx, skill, tc.Arguments)
		if err != nil {
			return fmt.Sprintf("错误: 技能 %q 执行失败: %v", tc.Name, err)
		}
		return result
	}

	tool, ok := p.reg.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("错误: 未找到工具 %q", tc.Name)
	}

	// 优先：通过合成事件触发真实命令执行并捕获回复
	if p.syncer != nil {
		if result := p.executeRealCommand(ctx, tc.Name); result != "" {
			return result
		}
	}

	// 回退：占位结果（工具无对应命令或捕获失败）
	callerCtx := WithCallerInfo(toolCtx, ctx.GetSenderInfo())
	done := make(chan struct{}, 1)
	var result string
	var execErr error
	go func() {
		result, execErr = tool.Execute(callerCtx, tc.Arguments)
		close(done)
	}()
	select {
	case <-done:
		if execErr != nil {
			return fmt.Sprintf("错误: 工具 %q 执行失败: %v", tc.Name, execErr)
		}
		return result
	case <-toolCtx.Done():
		return fmt.Sprintf("错误: 工具 %q 执行超时", tc.Name)
	}
}

// executeRealCommand 通过 vevent 注入合成事件执行工具对应的真实命令，
// 返回命令 handler 的回复文本。
//
// 使用 captureSender 捕获 handler 的 ctx.Reply() 输出，不转发给真实用户。
// AI 会自行总结工具执行结果后回复用户，避免用户看到两条消息。
func (p *Plugin) executeRealCommand(origCtx *eventctx.Context, toolName string) string {
	pattern, ok := p.cmdPatterns[toolName]
	if !ok {
		return ""
	}

	originalEvent := origCtx.GetPlatformEvent()
	if originalEvent == nil {
		return ""
	}

	evt := platform.NewSyntheticEvent(
		originalEvent.Kind(),
		pattern,
		platform.WithSyntheticSender(originalEvent.Sender()),
		platform.WithSyntheticChat(originalEvent.Chat()),
	)
	cs := &captureSender{}
	p.syncer.ProcessPlatformEventSync(evt, cs)
	return cs.captured
}

// executeSkill 执行一个 Skill 的内部工具调用循环。
//
// 使用自己的 Prompt 和 Tools 做最多 SkillMaxDepth 轮的非流式 LLM 调用。
// 不持久化到 session，纯函数式。
func (p *Plugin) executeSkill(ctx context.Context, skill Skill, args map[string]any) (string, error) {
	argsJSON, _ := json.MarshalIndent(args, "", "  ")
	msgs := []Message{
		{Role: RoleSystem, Content: skill.Prompt},
		{Role: RoleUser, Content: string(argsJSON)},
	}
	tools := p.buildSkillTools(skill)

	skillCtx, cancel := context.WithTimeout(ctx, p.cfg.SkillTimeout)
	defer cancel()

	for depth := 0; depth < p.cfg.SkillMaxDepth; depth++ {
		resp, err := p.runSingleRound(skillCtx, msgs, tools)
		if err != nil {
			return "", err
		}

		if len(resp.ToolCalls) == 0 {
			return resp.Text, nil
		}

		msgs = append(msgs, Message{Role: RoleAssistant, Content: resp.Text, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			result := p.executeSkillTool(skillCtx, tc, tools)
			msgs = append(msgs, Message{Role: RoleTool, Content: result, ToolCallID: tc.ID})
		}
	}

	return "", fmt.Errorf("技能 %q 超过最大调用深度 (%d)", skill.Name, p.cfg.SkillMaxDepth)
}

// buildSkillTools 构建 Skill 可见的工具列表 = 自己的 Tools + 其他已注册的 Skill。
// 其他 Skill 按其自带的 Parameters 注入，无参数时使用默认 {"query": string}。
func (p *Plugin) buildSkillTools(skill Skill) []Tool {
	tools := make([]Tool, 0, len(skill.Tools)+len(p.skillReg.List()))
	tools = append(tools, skill.Tools...)

	for _, s := range p.skillReg.List() {
		if s.Name == skill.Name {
			continue
		}
		other := s
		params := other.Parameters
		if len(params.Properties) == 0 {
			params = ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"query": {Type: "string", Description: "需要该技能处理的问题"},
				},
				Required: []string{"query"},
			}
		}
		tools = append(tools, Tool{
			Name:        other.Name,
			Description: other.Description,
			Parameters:  params,
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				return p.executeSkill(ctx, other, args)
			},
		})
	}

	return tools
}

// executeSkillTool 执行 Skill 内部的工具调用。
// 不走 syncer/real command，直接调用工具自身的 Execute 回调。
func (p *Plugin) executeSkillTool(ctx context.Context, tc ToolCall, tools []Tool) string {
	for _, t := range tools {
		if t.Name == tc.Name {
			result, err := t.Execute(ctx, tc.Arguments)
			if err != nil {
				return fmt.Sprintf("错误: 工具 %q 执行失败: %v", tc.Name, err)
			}
			return result
		}
	}
	return fmt.Sprintf("错误: 未找到工具 %q", tc.Name)
}

// cleanMessage 清洗消息内容，去除 @ 提及和触发命令前缀。
func (p *Plugin) cleanMessage(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimLeft(content, "@")
	content = strings.TrimSpace(content)
	if p.triggerCmd != "" {
		content = strings.TrimPrefix(content, p.triggerCmd)
	}
	content = strings.TrimSpace(content)
	return content
}

// makeSessionID 生成会话唯一标识。
// 格式: "{platform}:{chatID}:{userID}"
// 不同平台、不同群组、不同用户的会话相互隔离。
func makeSessionID(platform, chatID, userID string) string {
	return fmt.Sprintf("%s:%s:%s", platform, chatID, userID)
}
