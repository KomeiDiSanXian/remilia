package ai

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	infrastorage "github.com/KomeiDiSanXian/remilia/infra/storage"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// DefaultConfig AI 插件默认配置。
var DefaultConfig = Config{
	Provider:     "openai",
	Model:        "gpt-4o-mini",
	MaxTokens:    2048,
	MaxDepth:     5,
	MaxHistory:   20,
	SessionTTL:   24 * time.Hour,
	SystemPrompt: "你是一个有用的AI助手，运行在一个叫Remilia Bot的IM框架中。用户问你有什么工具时请列举可用的工具。",
	TriggerCmd:   "/ai",
	AtBot:        true,
	PrivateChat:  true,
	Markdown:     true,
	Fallback:     false,
}

// Plugin AI 对话插件的主结构体。
// 管理会话、工具注册、LLM 提供商调用。
type Plugin struct {
	cfg        *Config
	coord      engine.Reader
	eng        *engine.Engine
	sm         *SessionManager
	reg        *ToolRegistry
	prov       Provider
	triggerCmd string
	// cmdPatterns 工具名 → 完整命令模式（如 "ping" → "/ping"），
	// 用于 executeTool 时构造正确的命令文本
	cmdPatterns map[string]string
}

// New 创建 AI 对话插件的描述符。
//
// 该插件支持：
//   - 多 LLM 提供商（OpenAI 兼容 API、Anthropic）
//   - 流式输出（SSE 逐 token 推送）
//   - 工具调用（自动发现无权限命令 + ToolProvider 接口）
//   - 会话管理（LRU 缓存 + 可选 GORM 持久化）
//   - 多种触发方式（命令 / @机器人 / 私聊）
//
// # 安全说明
//
// ⚠️ 自动发现工具时**仅暴露不需要权限的命令**，防止通过 AI 绕过权限检查。
// 带有 Permissions 的敏感命令不会被 AI 自动发现。
// 需要 AI 可调用的权限命令应通过 [ToolProvider] 接口显式注册。.
func New(eng *engine.Engine) *plugin.Descriptor {
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
  /ai tools           — 列出可用工具
  /ai reset           — 清空当前对话历史
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
				eng:         eng,
				prov:        prov,
				reg:         NewToolRegistry(),
				sm:          NewSessionManager(1000, cfg.MaxHistory, cfg.SessionTTL, store),
				cmdPatterns: make(map[string]string),
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

	if cfg.TriggerCmd == "" && !cfg.AtBot && !cfg.PrivateChat {
		ctx.Log.Warn("No trigger method enabled: set trigger_cmd, at_bot, or private_chat in config")
	}

	return &cfg
}

// registerHandlers 注册 AI 对话的触发处理器。
//
// 支持三种触发方式（可组合）：
//   - 命令前缀（如 /ai）
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
		ctx.Reg.RegisterCommand("", trigger).Handle(p.handleAI)
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
// 对于需要 AI 调用的权限命令，插件应：
//   - 实现 [ToolProvider] 接口显式注册工具
//   - 或在工具 Execute 中手动校验调用者身份
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

// handleSubCommand 处理 /ai 的子命令（reset、tools、undo、summary 等），
// 不经过 LLM 常规对话流程。
// 返回 true 表示已处理，调用方应直接 return。
func (p *Plugin) handleSubCommand(ctx *eventctx.Context, content string) bool {
	cmd := strings.ToLower(strings.TrimSpace(content))

	sender := ctx.GetSenderInfo()
	chat := ctx.GetChatInfo()
	sessionID := makeSessionID(ctx.GetEventPlatform(), chat.ID, sender.ID)

	switch cmd {
	case "reset", "重置":
		p.sm.Delete(sessionID)
		ctx.ReplyText("✅ 对话历史已清空，开始全新的对话吧！")
		return true

	case "undo":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			ctx.ReplyText("没有可以撤销的对话")
			return true
		}
		// 从后往前找到最后一个 user 消息，删除它及其之后所有消息
		lastUserIdx := -1
		for i := len(session.Messages) - 1; i >= 0; i-- {
			if session.Messages[i].Role == RoleUser {
				lastUserIdx = i
				break
			}
		}
		if lastUserIdx <= 0 {
			ctx.ReplyText("没有可以撤销的对话")
			return true
		}
		session.Messages = session.Messages[:lastUserIdx]
		p.sm.Save(session)
		ctx.ReplyText("↩️ 已撤销上一条对话")
		return true

	case "summary", "总结":
		session := p.sm.GetOrCreate(sessionID, sender.ID, chat.ID)
		if session == nil || len(session.Messages) <= 1 {
			ctx.ReplyText("还没有任何对话内容可以总结")
			return true
		}
		go p.doSummary(ctx, session, sender, chat)
		ctx.ReplyText("⏳ 正在生成对话总结，请稍候...")
		return true

	case "tools", "工具", "help", "帮助":
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
		b.WriteString(fmt.Sprintf("\n其他子命令："))
		b.WriteString(fmt.Sprintf("\n  `%s reset` — 清空对话历史", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s undo` — 撤销上一条对话", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s summary` — 总结当前对话", p.cfg.TriggerCmd))
		b.WriteString(fmt.Sprintf("\n  `%s tools` — 列出可用工具", p.cfg.TriggerCmd))
		ctx.ReplyText(b.String())
		return true
	}
	return false
}

// doSummary 在后台调用 LLM 生成对话总结，通过原始 sender 发送结果。
func (p *Plugin) doSummary(origCtx *eventctx.Context, session *Session, sender platform.UserInfo, chat platform.ChatInfo) {
	msgs := make([]Message, 0, len(session.Messages)+1)
	for _, m := range session.Messages {
		if m.Role != RoleSystem {
			msgs = append(msgs, m)
		}
	}
	summaryPrompt := Message{
		Role:    RoleUser,
		Content: "请用简短的几句话总结以上对话的要点。",
	}
	msgs = append(msgs, summaryPrompt)

	req := &ChatRequest{
		Model:    p.cfg.Model,
		Messages: msgs,
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
//  1. 提取并清洗消息内容
//  2. 获取或创建会话（按 platform:chatID:userID 隔离）
//  3. 注入/更新系统提示词和工具描述
//  4. 追加用户消息到会话
//  5. 进入工具调用循环（processWithTools）
//  6. 发送最终回复
func (p *Plugin) handleAI(ctx *eventctx.Context) error {
	content := ctx.GetMessageContent()
	if content == "" {
		return nil
	}

	content = p.cleanMessage(content)

	// 排除命令消息：清洗后若仍是命令（带 "/"、"!" 等前缀），
	// AI 不应处理，避免与命令 handler 并发争抢共享 ctx 导致竞态。
	if content == "" || isCommandMessage(content) {
		return nil
	}

	// 子命令处理：/ai reset、/ai tools、/ai help 等不走 LLM
	if p.handleSubCommand(ctx, content) {
		return nil
	}

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

	// 构建系统提示词
	// 工具定义已通过 ChatRequest.Tools 参数传递给 LLM，无需额外写入 system prompt
	var systemPrompt = p.cfg.SystemPrompt

	// 更新/插入系统消息
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

	// 追加用户消息
	p.sm.AppendMessage(session, Message{Role: RoleUser, Content: content})

	// 进入工具调用循环
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

		req := &ChatRequest{
			Model:    p.cfg.Model,
			Messages: session.Messages,
			Tools:    p.reg.List(),
		}

		streamCh, err := p.prov.ChatStream(ctx.Context(), req)
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
			toolResult := p.executeTool(ctx, tc)
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
//   其他: 显示原始错误
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

// commandEvent 包装 platform.Event，用于在工具执行时向引擎注入伪造的命令事件。
//
// 覆盖 Content() 返回命令文本，ID() 返回唯一标识以避免被去重中间件拦截。
type commandEvent struct {
	platform.Event
	content string
	id      string
}

func (e *commandEvent) Content() string { return e.content }
func (e *commandEvent) ID() string {
	if e.id != "" {
		return e.id
	}
	return e.Event.ID()
}

// captureSender 包装 platform.Sender，拦截 Send 调用并记录消息文本内容，
// 但不转发给真实 Sender。命令 handler 的回复仅作为工具结果返回给 AI，
// 避免用户同时看到命令原始输出和 AI 总结两条消息。
type captureSender struct {
	platform.Sender
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
// 优先通过 Engine 触发真实命令执行并捕获其回复内容；
// 若捕获失败或工具无对应命令，回退到 tool.Execute 的占位结果。
func (p *Plugin) executeTool(ctx *eventctx.Context, tc ToolCall) string {
	tool, ok := p.reg.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("错误: 未找到工具 %q", tc.Name)
	}

	// 优先：通过 Engine 执行真实命令并捕获回复
	if p.eng != nil {
		if result := p.executeRealCommand(ctx, tc.Name); result != "" {
			return result
		}
	}

	// 回退：占位结果（工具无对应命令或捕获失败）
	result, err := tool.Execute(ctx.Context(), tc.Arguments)
	if err != nil {
		return fmt.Sprintf("错误: 工具 %q 执行失败: %v", tc.Name, err)
	}
	return result
}

// executeRealCommand 通过 Engine 执行工具对应的真实命令，返回命令 handler 的回复文本。
//
// 使用 captureSender 包装原始 Sender，命令 handler 的 ctx.Reply() 会被捕获，
// 同时消息仍会正常发送给用户。
func (p *Plugin) executeRealCommand(origCtx *eventctx.Context, toolName string) string {
	pattern, ok := p.cmdPatterns[toolName]
	if !ok {
		return ""
	}

	originalEvent := origCtx.GetPlatformEvent()
	if originalEvent == nil {
		return ""
	}

	wrapped := &commandEvent{
		Event:   originalEvent,
		content: pattern,
		id:      pattern + ":" + toolName + ":" + fmt.Sprint(time.Now().UnixNano()),
	}
	cs := &captureSender{Sender: origCtx.GetPlatformSender()}
	cmdCtx := eventctx.NewContextFromEvent(wrapped, cs)
	p.eng.ProcessEventSync(cmdCtx)
	return cs.captured
}

// cleanMessage 清洗消息内容，去除命令前缀和 @ 提及。
func (p *Plugin) cleanMessage(content string) string {
	content = strings.TrimSpace(content)

	if p.triggerCmd != "" && strings.HasPrefix(content, p.triggerCmd) {
		content = strings.TrimPrefix(content, p.triggerCmd)
		content = strings.TrimSpace(content)
	}

	content = strings.TrimLeft(content, "@")
	content = strings.TrimSpace(content)

	return content
}

// makeSessionID 生成会话唯一标识。
// 格式: "{platform}:{chatID}:{userID}"
// 不同平台、不同群组、不同用户的会话相互隔离。
func makeSessionID(platform, chatID, userID string) string {
	return fmt.Sprintf("%s:%s:%s", platform, chatID, userID)
}
