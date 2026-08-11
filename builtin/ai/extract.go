// Package ai extract.go — 事实记忆的自动抽取。
//
// 本文件实现对话结束后的异步事实抽取：
//   - maybeExtractMemory：节流检查 + 异步抽取入口（不阻塞对话回复）
//   - extractFacts：调用 LLM 从最近一轮 user+assistant 对话中提取事实（JSON 数组）
//   - parseExtractedFacts：鲁棒解析抽取结果（坏 JSON / 空数组 / 非法作用域兜底）
//
// 抽取语义：
//   - 输入：会话最近一条用户消息 + 最近一条助手回复（不含系统消息与工具轮次）
//   - 输出：[{"text": "...", "scope": "user"|"group"}]；scope 非法时归入 user
//   - 只提取稳定长期事实（偏好/习惯/约定/个人属性），忽略一次性事件与时间敏感信息
//   - 节流：同一作用域距上次抽取 >= memory_min_interval 才再次抽取（默认 10 分钟）
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// memoryExtractPrompt 事实抽取的系统提示词。
const memoryExtractPrompt = `你是记忆提取助手。从下面的对话中提取值得长期记住的事实（用户偏好、习惯、重要约定、个人属性等）。
要求：
- 只提取稳定的、长期的、对未来对话有帮助的事实；忽略一次性事件、时间敏感信息、命令、寒暄
- "user" 作用域：与单个用户相关的个人事实（如"用户喜欢喝咖啡"）
- "group" 作用域：群里共同确认的公共事实（如"本群成员都是 FGO 玩家"）；私聊只提取 user
- 事实表述要简洁完整、不含人称代词指代（如把"我喜欢"写成"用户喜欢"）
- 严格输出 JSON 数组，不要其他内容：[{"text": "...", "scope": "user|group"}]；没有可提取内容时输出 []

对话如下：`

// extractedFact LLM 抽取结果。
type extractedFact struct {
	Text  string `json:"text"`
	Scope string `json:"scope"`
}

// maybeExtractMemory 在对话回复完成后异步抽取记忆（受 memory_enabled 与节流控制）。
// 使用插件生命周期 context 执行，不依赖事件 context（事件可能在回复后很快超时）。
func (p *Plugin) maybeExtractMemory(ctx *eventctx.Context, session *Session) {
	if p.memory == nil || !p.memory.Enabled() {
		return
	}
	sender := ctx.GetSenderInfo()
	if sender.ID == "" {
		return
	}
	chat := ctx.GetChatInfo()

	scopes := []string{userScope(sender.ID)}
	if chat.IsGroup && chat.ID != "" {
		scopes = append(scopes, groupScope(chat.ID))
	}
	for _, scope := range scopes {
		if !p.memory.CanExtract(scope) {
			continue
		}
		p.memory.MarkExtracted(scope)
		scope := scope
		p.lifecycleSpawn(func() {
			if err := p.extractAndStore(scope, sender.ID, chat, session); err != nil {
				logger.Debugf("[AI] Memory extract failed: %v", err)
			}
		})
	}
}

// lifecycleSpawn 在插件生命周期上下文上启动后台任务。
func (p *Plugin) lifecycleSpawn(fn func()) {
	if p.lifecycleCtx == nil {
		go fn()
		return
	}
	go func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			fn()
		}()
		select {
		case <-p.lifecycleCtx.Done():
		case <-done:
		}
	}()
}

// extractAndStore 执行一轮抽取并写入存储。
func (p *Plugin) extractAndStore(scope, userID string, chat platform.ChatInfo, session *Session) error {
	conv := lastRoundForMemory(session)
	if conv == "" {
		return nil
	}

	base := p.lifecycleCtx
	if base == nil {
		base = context.Background()
	}
	extractCtx, cancel := context.WithTimeout(base, 30*time.Second)
	defer cancel()

	model := p.cfg.ExtractModel
	if model == "" {
		model = p.cfg.Model
	}
	result, err := p.runSingleRoundModel(extractCtx, model, []Message{
		{Role: RoleSystem, Content: memoryExtractPrompt},
		{Role: RoleUser, Content: conv},
	}, nil)
	if err != nil {
		return fmt.Errorf("extract llm call: %w", err)
	}

	facts := parseExtractedFacts(result.Text)
	stored := 0
	for _, f := range facts {
		if f.Scope == "group" && chat.IsGroup {
			p.memory.Add(groupScope(chat.ID), f.Text)
			stored++
			continue
		}
		p.memory.Add(userScope(userID), f.Text)
		stored++
	}
	logger.Debugf("[AI] Memory extract: %d facts for %s", stored, scope)
	return nil
}

// lastRoundForMemory 提取最近一轮 用户消息 + 助手回复 作为抽取输入。
// 跳过系统消息与工具轮次；无有效对话返回空串。
func lastRoundForMemory(session *Session) string {
	var lastUser, lastAssistant string
	for _, m := range session.SnapshotMessages() {
		switch m.Role {
		case RoleUser:
			if text := memoryMessageText(m); text != "" {
				lastUser = text
			}
		case RoleAssistant:
			if text := memoryMessageText(m); text != "" {
				lastAssistant = text
			}
		}
	}
	if lastUser == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("用户: " + lastUser)
	if lastAssistant != "" {
		b.WriteString("\n助手: " + lastAssistant)
	}
	return b.String()
}

// memoryMessageText 提取用于记忆的消息文本（含多模态文本片段，忽略反射指令等系统注入）。
func memoryMessageText(m Message) string {
	if len(m.ContentParts) == 0 {
		return strings.TrimSpace(m.Content)
	}
	var parts []string
	for _, cp := range m.ContentParts {
		if cp.Type == ContentPartText && strings.TrimSpace(cp.Text) != "" {
			parts = append(parts, strings.TrimSpace(cp.Text))
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// parseExtractedFacts 鲁棒解析 LLM 输出的事实数组。
// 容忍前后缀文本（截取首个 '[' 到末尾 ']'）、坏 JSON（返回空）、空数组。
func parseExtractedFacts(text string) []extractedFact {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '[')
	end := strings.LastIndexByte(text, ']')
	if start < 0 || end <= start {
		return nil
	}
	var facts []extractedFact
	if err := json.Unmarshal([]byte(text[start:end+1]), &facts); err != nil {
		logger.Debugf("[AI] Memory extract parse failed: %v", err)
		return nil
	}
	out := facts[:0]
	for _, f := range facts {
		f.Text = strings.TrimSpace(f.Text)
		f.Scope = strings.TrimSpace(f.Scope)
		if f.Text == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}
