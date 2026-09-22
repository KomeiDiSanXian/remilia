// extract.go — 事实记忆的自动抽取。
//
// 对话结束后的异步事实抽取：
//   - Extractor.Extract：调用 LLM 从最近一轮 user+assistant 对话中提取事实（JSON 数组）
//   - ParseExtractedFacts：鲁棒解析抽取结果（坏 JSON / 空数组 / 非法作用域兜底）
//   - LastRoundForMemory：渲染抽取输入（最近一条用户消息 + 最近一条助手回复）
//
// 抽取语义：
//   - 输入：会话最近一条用户消息 + 最近一条助手回复（不含系统消息与工具轮次）
//   - 输出：[{"text": "...", "scope": "user"|"group"}]；scope 非法时归入 user
//   - 只提取稳定长期事实（偏好/习惯/约定/个人属性），忽略一次性事件与时间敏感信息
//
// 节流（memory_min_interval）与异步调度依赖插件生命周期，留在装配侧；
// 本包只负责"跑一轮抽取并写入"。
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// MemoryExtractPrompt 事实抽取的系统提示词。
const MemoryExtractPrompt = `你是记忆提取助手。从下面的对话中提取值得长期记住的事实（用户偏好、习惯、重要约定、个人属性等）。
要求：
- 只提取稳定的、长期的、对未来对话有帮助的事实；忽略一次性事件、时间敏感信息、命令、寒暄
- "user" 作用域：与单个用户相关的个人事实（如"用户喜欢喝咖啡"）
- "group" 作用域：群里共同确认的公共事实（如"本群成员都是 FGO 玩家"）；私聊只提取 user
- 事实表述要简洁完整、不含人称代词指代（如把"我喜欢"写成"用户喜欢"）
- 严格输出 JSON 数组，不要其他内容：[{"text": "...", "scope": "user|group"}]；没有可提取内容时输出 []

对话如下：`

// ExtractedFact LLM 抽取结果。
type ExtractedFact struct {
	Text  string `json:"text"`
	Scope string `json:"scope"`
}

// MemoryWriter 长期记忆的写入端口（未启用时字段为 nil）。
// 方法与具体存储（builtin/ai 的 memoryStore）一致，装配侧直接注入。
type MemoryWriter interface {
	Enabled() bool
	CanExtract(scope string) bool
	MarkExtracted(scope string)
	Add(scope, text string)
}

// ScopeKeys 记忆作用域键的构造（键格式由装配侧唯一决定）。
type ScopeKeys interface {
	UserScope(userID string) string
	GroupScope(chatID string) string
}

// Extractor 执行一轮事实抽取并写入长期记忆。
type Extractor struct {
	// Client 单轮非流式 LLM 调用。
	Client Client
	// Memory 长期记忆写入端口。
	Memory MemoryWriter
	// Scopes 作用域键构造。
	Scopes ScopeKeys
	// LifecycleCtx 插件生命周期上下文（nil 时用 context.Background）。
	LifecycleCtx context.Context
}

// Extract 执行一轮抽取并写入存储。
// scope 为本次抽取的节流作用域（仅用于日志），userID 与 chat 决定事实写入位置。
func (e Extractor) Extract(scope, userID string, chat platform.ChatInfo, sess *session.Session) error {
	conv := LastRoundForMemory(sess)
	if conv == "" {
		return nil
	}

	base := e.LifecycleCtx
	if base == nil {
		base = context.Background()
	}
	extractCtx, cancel := context.WithTimeout(base, 30*time.Second)
	defer cancel()

	model := e.Client.Cfg.ExtractModel
	if model == "" {
		model = e.Client.Cfg.Model
	}
	result, err := e.Client.SingleRound(extractCtx, model, []protocol.Message{
		{Role: protocol.RoleSystem, Content: MemoryExtractPrompt},
		{Role: protocol.RoleUser, Content: conv},
	}, nil)
	if err != nil {
		return fmt.Errorf("extract llm call: %w", err)
	}

	facts := ParseExtractedFacts(result.Text)
	stored := 0
	for _, f := range facts {
		if f.Scope == "group" && chat.IsGroup {
			e.Memory.Add(e.Scopes.GroupScope(chat.ID), f.Text)
			stored++
			continue
		}
		e.Memory.Add(e.Scopes.UserScope(userID), f.Text)
		stored++
	}
	logger.Debugf("[AI] Memory extract: %d facts for %s", stored, scope)
	return nil
}

// LastRoundForMemory 提取最近一轮 用户消息 + 助手回复 作为抽取输入。
// 跳过系统消息与工具轮次；无有效对话返回空串。
func LastRoundForMemory(sess *session.Session) string {
	var lastUser, lastAssistant string
	for _, m := range sess.SnapshotMessages() {
		switch m.Role {
		case protocol.RoleUser:
			if text := MemoryMessageText(m); text != "" {
				lastUser = text
			}
		case protocol.RoleAssistant:
			if text := MemoryMessageText(m); text != "" {
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

// MemoryMessageText 提取用于记忆的消息文本（含多模态文本片段，忽略反射指令等系统注入）。
func MemoryMessageText(m protocol.Message) string {
	if len(m.ContentParts) == 0 {
		return strings.TrimSpace(m.Content)
	}
	var parts []string
	for _, cp := range m.ContentParts {
		if cp.Type == protocol.ContentPartText && strings.TrimSpace(cp.Text) != "" {
			parts = append(parts, strings.TrimSpace(cp.Text))
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// ParseExtractedFacts 鲁棒解析 LLM 输出的事实数组。
// 容忍前后缀文本（截取首个 '[' 到末尾 ']'）、坏 JSON（返回空）、空数组。
func ParseExtractedFacts(text string) []ExtractedFact {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '[')
	end := strings.LastIndexByte(text, ']')
	if start < 0 || end <= start {
		return nil
	}
	var facts []ExtractedFact
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
