package conversation

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/internal/jsonfile"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// StepHandler is the function type for a conversation step.
// Return nil to advance, ErrStepDone to finish early, or another error to abort.
type StepHandler func(ctx *eventctx.Context, session *Session) error

// ErrStepDone signals that the session should end immediately.
var ErrStepDone = fmt.Errorf("conversation: session done")

// ErrStepRepeat signals that the current step should stay active without advancing.
// Use this when the user's input is invalid and you want to wait for a corrected input
// without consuming the attempt or producing an error.
//
// Example:
//
//	m.Step("guess", "请输入数字：", func(c *eventctx.Context, s *Session) error {
//	    input := c.GetMessageContent()
//	    if _, err := strconv.Atoi(input); err != nil {
//	        _ = reply(c, "⚠️ 请输入有效数字")
//	        return ErrStepRepeat // 保持当前步骤，等待下一次输入
//	    }
//	    // 处理有效输入 ...
//	    return nil
//	})
var ErrStepRepeat = fmt.Errorf("conversation: step repeat")

// ErrSessionNotFound is returned when no active session exists.
var ErrSessionNotFound = fmt.Errorf("conversation: session not found")

// gcInterval 过期会话后台 GC 间隔（Bug 2.4 修复）
const gcInterval = 2 * time.Minute

type step struct {
	name    string
	prompt  string
	handle  StepHandler
	waitFor func(*Session) string // 若非 nil，仅当发送者 ID 与返回值匹配时才推进步骤
}

// Machine is an immutable conversation state machine definition.
type Machine struct {
	name    string
	steps   []step
	onDone  StepHandler
	timeout time.Duration
}

// Session is the runtime state for one active conversation.
type Session struct {
	ID        string
	UserID    string
	ChatID    string // 会话所在聊天上下文（群ID或用户ID），防止跨群干扰
	Machine   string
	StepIdx   int
	Data      map[string]any
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Plugin is the conversation plugin API.
type Plugin struct {
	sessions sync.Map
	machines sync.Map
	dataFile string // 持久化文件路径（空字符串=纯内存）
}

// Option 配置选项
type Option func(*Plugin)

// WithDataFile 设置 JSON 持久化文件路径。空字符串表示纯内存模式。
func WithDataFile(path string) Option {
	return func(p *Plugin) { p.dataFile = path }
}

// NewPlugin 创建并返回一个 Conversation Plugin 实例。
// 配合 p.Descriptor() 使用，适合需要在注册前持有插件引用的场景（如测试）：
//
//	p := conversation.NewPlugin()
//	pm.Register(p.Descriptor())
//	p.Start(ctx, machine)
func NewPlugin(opts ...Option) *Plugin {
	p := &Plugin{}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Descriptor 根据已有 Plugin 实例生成插件描述符，供 pm.Register 使用。
func (p *Plugin) Descriptor() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "conversation",
		Version: "1.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "Multi-step conversation/FSM plugin with cross-message state tracking",
			Category:    "core",
			Tags:        []string{"conversation", "fsm", "session"},
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			p.restoreSessions()
			// 后台定期 GC 过期会话，防止 sync.Map 无限增长
			ctx.Go(func(runCtx context.Context) {
				ticker := time.NewTicker(gcInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						removed := p.GC()
						if removed > 0 {
							ctx.Log.Infof("GC: removed %d expired sessions", removed)
						}
					case <-runCtx.Done():
						return
					}
				}
			})
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).persistSessions()
			return nil
		},
	}
}

// New 创建会话状态机插件描述符（便捷入口）。
func New(opts ...Option) *plugin.Descriptor {
	return NewPlugin(opts...).Descriptor()
}

// NewMachine creates a new Machine definition with a default 10-minute timeout.
func (p *Plugin) NewMachine(name string) *Machine {
	return &Machine{name: name, timeout: 10 * time.Minute}
}

// Step appends a step to the machine.
func (m *Machine) Step(name, prompt string, handle StepHandler) *Machine {
	m.steps = append(m.steps, step{name: name, prompt: prompt, handle: handle})
	return m
}

// Done sets the completion callback.
func (m *Machine) Done(handle StepHandler) *Machine {
	m.onDone = handle
	return m
}

// WithTimeout sets the session timeout.
func (m *Machine) WithTimeout(d time.Duration) *Machine {
	m.timeout = d
	return m
}

// WaitFor 限制最近一个步骤只接受特定用户的消息。
//
// fn 在每次消息到达时以当前 Session 为参数调用，返回期望的用户 ID：
//   - 返回非空字符串：仅当发送者 ID 与其相等时步骤才推进
//   - 返回空字符串 ""：无限制，任意用户均可推进（等同于不调用 WaitFor）
//
// 不匹配的消息被静默忽略（会话不报错，不推进，等待下一条消息）。
//
// 典型用途：challenge/accept 流程——User A 发起挑战，步骤需等候 User B 接受。
//
// 示例：
//
//	m.Step("accept", "请接受或拒绝挑战（接受/拒绝）:", handleAccept).
//	    WaitFor(func(s *Session) string {
//	        return s.Data["challenged_id"].(string)
//	    })
func (m *Machine) WaitFor(fn func(*Session) string) *Machine {
	if len(m.steps) == 0 {
		return m
	}
	m.steps[len(m.steps)-1].waitFor = fn
	return m
}

// WaitForUser 是 [WaitFor] 的便捷版，限制最近一个步骤只接受静态指定用户的消息。
//
// 示例：
//
//	m.Step("confirm", "管理员请确认：", handleConfirm).
//	    WaitForUser(adminID)
func (m *Machine) WaitForUser(userID string) *Machine {
	return m.WaitFor(func(_ *Session) string { return userID })
}

// Start begins a conversation session for the current user and sends the first prompt.
func (p *Plugin) Start(ctx *eventctx.Context, m *Machine) error {
	return p.StartWithData(ctx, m, nil)
}

// StartWithData begins a conversation session with pre-populated session data.
//
// initialData 中的键值对会在会话创建时写入 Session.Data，
// 供后续步骤的 StepHandler 直接读取（无需再从外部传入）。
func (p *Plugin) StartWithData(ctx *eventctx.Context, m *Machine, initialData map[string]any) error {
	p.machines.Store(m.name, m)
	userID := extractUserID(ctx)
	if userID == "" {
		return fmt.Errorf("conversation: cannot determine user ID")
	}
	chatID := extractChatID(ctx)
	data := make(map[string]any)
	maps.Copy(data, initialData)
	now := time.Now()
	session := &Session{
		ID:        chatID + ":" + userID + ":" + m.name,
		UserID:    userID,
		ChatID:    chatID,
		Machine:   m.name,
		StepIdx:   0,
		Data:      data,
		CreatedAt: now,
		ExpiresAt: now.Add(m.timeout),
	}
	p.sessions.Store(session.ID, session)
	logger.Debugf("[Conversation] Started session %s", session.ID)
	if len(m.steps) > 0 && m.steps[0].prompt != "" {
		sendPrompt(ctx, m.steps[0].prompt)
	}
	return nil
}

// Dispatch routes the current message to the user's active session.
//
// 除了匹配"发送者是会话所有者"的常规情况外，还会查找
// 当前步骤通过 [Machine.WaitFor] 指定等待本发送者的会话，
// 实现跨用户（如 challenge/accept）的多步会话流程。
func (p *Plugin) Dispatch(ctx *eventctx.Context) error {
	userID := extractUserID(ctx)
	chatID := extractChatID(ctx)
	if userID == "" {
		return nil
	}
	var session *Session
	p.sessions.Range(func(k, v any) bool {
		s := v.(*Session)
		if s.ChatID != chatID || isExpired(s) {
			return true
		}
		// 常规情况：发送者是会话所有者
		if s.UserID == userID {
			session = s
			return false
		}
		// WaitFor 情况：当前步骤正在等待本发送者
		if p.stepExpectedUser(s) == userID {
			session = s
			return false
		}
		return true
	})
	if session == nil {
		return ErrSessionNotFound
	}
	return p.advance(ctx, session)
}

// DispatchFor returns a Handler that routes to the named machine's session.
func (p *Plugin) DispatchFor(machineName string) eventctx.Handler {
	return func(ctx *eventctx.Context) error {
		userID := extractUserID(ctx)
		chatID := extractChatID(ctx)
		if userID == "" {
			return nil
		}
		v, ok := p.sessions.Load(chatID + ":" + userID + ":" + machineName)
		if !ok {
			return ErrSessionNotFound
		}
		s := v.(*Session)
		if isExpired(s) {
			p.sessions.Delete(s.ID)
			return ErrSessionNotFound
		}
		return p.advance(ctx, s)
	}
}

// stepExpectedUser 返回 session 当前步骤通过 WaitFor 指定的期望用户 ID。
// 若当前步骤无 WaitFor 限制，返回空字符串。
func (p *Plugin) stepExpectedUser(s *Session) string {
	mv, ok := p.machines.Load(s.Machine)
	if !ok {
		return ""
	}
	m := mv.(*Machine)
	if s.StepIdx >= len(m.steps) {
		return ""
	}
	st := m.steps[s.StepIdx]
	if st.waitFor == nil {
		return ""
	}
	return st.waitFor(s)
}

func (p *Plugin) advance(ctx *eventctx.Context, session *Session) error {
	mv, ok := p.machines.Load(session.Machine)
	if !ok {
		return fmt.Errorf("conversation: machine %q not found", session.Machine)
	}
	m := mv.(*Machine)
	if session.StepIdx >= len(m.steps) {
		p.sessions.Delete(session.ID)
		return nil
	}
	st := m.steps[session.StepIdx]

	// WaitFor 前置检查：若当前步骤限定了期望用户，验证发送者是否匹配
	if st.waitFor != nil {
		expected := st.waitFor(session)
		if expected != "" && extractUserID(ctx) != expected {
			// 不是期望用户，静默忽略——既不推进步骤，也不报错
			logger.Debugf("[Conversation] Session %s step %d waiting for user %s, got %s (ignored)",
				session.ID, session.StepIdx, expected, extractUserID(ctx))
			return nil
		}
	}

	err := st.handle(ctx, session)
	if errors.Is(err, ErrStepDone) {
		p.sessions.Delete(session.ID)
		if m.onDone != nil {
			return m.onDone(ctx, session)
		}
		return nil
	}
	if errors.Is(err, ErrStepRepeat) {
		// 保持当前步骤不前进，不报错，等待下一次消息
		logger.Debugf("[Conversation] Session %s step repeat (stay at step %d)", session.ID, session.StepIdx)
		return nil
	}
	if err != nil {
		return err
	}
	session.StepIdx++
	if session.StepIdx >= len(m.steps) {
		p.sessions.Delete(session.ID)
		logger.Debugf("[Conversation] Session %s completed", session.ID)
		if m.onDone != nil {
			return m.onDone(ctx, session)
		}
		return nil
	}
	if next := m.steps[session.StepIdx]; next.prompt != "" {
		sendPrompt(ctx, next.prompt)
	}
	return nil
}

// Cancel cancels a user's named machine session in the given chat context.
func (p *Plugin) Cancel(userID, machineName string) {
	// Legacy: try both old and new key formats for backward compatibility
	p.sessions.Delete(userID + ":" + machineName)
	// New format keys will be found by range scan
	p.sessions.Range(func(k, v any) bool {
		s := v.(*Session)
		if s.UserID == userID && s.Machine == machineName {
			p.sessions.Delete(k)
		}
		return true
	})
}

// CancelInChat cancels a user's named machine session in a specific chat.
func (p *Plugin) CancelInChat(chatID, userID, machineName string) {
	p.sessions.Delete(chatID + ":" + userID + ":" + machineName)
}

// InSession returns a Rule that matches when the user has an active session for machineName
// in the current chat context (group or private).
func (p *Plugin) InSession(machineName string) eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		userID := extractUserID(ctx)
		chatID := extractChatID(ctx)
		if userID == "" {
			return false
		}
		v, ok := p.sessions.Load(chatID + ":" + userID + ":" + machineName)
		if !ok {
			return false
		}
		s := v.(*Session)
		if isExpired(s) {
			p.sessions.Delete(s.ID)
			return false
		}
		return true
	}
}

// HasActiveSession reports whether the user has any active session in the current chat.
func (p *Plugin) HasActiveSession(userID string) bool {
	found := false
	p.sessions.Range(func(_, v any) bool {
		s := v.(*Session)
		if s.UserID == userID && !isExpired(s) {
			found = true
			return false
		}
		return true
	})
	return found
}

// ActiveSessions returns the count of active sessions.
func (p *Plugin) ActiveSessions() int {
	n := 0
	p.sessions.Range(func(_, _ any) bool { n++; return true })
	return n
}

// GC removes expired sessions. Call periodically via scheduler.
func (p *Plugin) GC() int {
	var keys []string
	p.sessions.Range(func(k, v any) bool {
		if isExpired(v.(*Session)) {
			keys = append(keys, k.(string))
		}
		return true
	})
	for _, k := range keys {
		p.sessions.Delete(k)
	}
	if len(keys) > 0 {
		logger.Debugf("[Conversation] GC removed %d expired sessions", len(keys))
	}
	return len(keys)
}
func isExpired(s *Session) bool {
	return !s.ExpiresAt.IsZero() && time.Now().After(s.ExpiresAt)
}

// persistSessions 将所有活跃会话保存到 JSON 文件
func (p *Plugin) persistSessions() {
	if p.dataFile == "" {
		return
	}
	sessions := make(map[string]*Session)
	p.sessions.Range(func(k, v any) bool {
		s := v.(*Session)
		if !isExpired(s) {
			sessions[k.(string)] = s
		}
		return true
	})
	if err := jsonfile.Write(p.dataFile, sessions); err != nil {
		logger.WithError(err).Warn("[Conversation] Failed to persist sessions")
	} else {
		logger.Infof("[Conversation] Persisted %d sessions", len(sessions))
	}
}

// restoreSessions 从 JSON 文件恢复会话
func (p *Plugin) restoreSessions() {
	if p.dataFile == "" {
		return
	}
	sessions, err := jsonfile.Read[map[string]*Session](p.dataFile)
	if err != nil {
		return
	}
	count := 0
	for k, s := range sessions {
		if !isExpired(s) {
			p.sessions.Store(k, s)
			count++
		}
	}
	logger.Infof("[Conversation] Restored %d sessions", count)
}

func extractUserID(ctx *eventctx.Context) string {
	return ctx.GetSenderInfo().ID
}

// extractChatID 提取会话的聊天上下文 ID（群组消息返回群 ID，私聊返回用户 ID）。
// 用于区分"同一用户在不同群"的会话，防止跨群干扰。
func extractChatID(ctx *eventctx.Context) string {
	chat := ctx.GetChatInfo()
	if chat.IsGroup && chat.ID != "" {
		return "g:" + chat.ID
	}
	// 私聊：以用户 ID 作为会话上下文
	uid := ctx.GetSenderInfo().ID
	return "u:" + uid
}

func sendPrompt(ctx *eventctx.Context, prompt string) {
	if _, err := ctx.Reply(platform.TextMessage(prompt)); err != nil {
		logger.WithError(err).Warn("[Conversation] Failed to send prompt")
	}
}
