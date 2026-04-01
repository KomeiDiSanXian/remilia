package conversation

import (
	"context"
	stdctx "context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/storage"
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

// ErrSessionNotFound is returned when no active session exists.
var ErrSessionNotFound = fmt.Errorf("conversation: session not found")

// gcInterval 过期会话后台 GC 间隔（Bug 2.4 修复）
const gcInterval = 2 * time.Minute

type step struct {
	name   string
	prompt string
	handle StepHandler
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
	store    *storage.Store // 可选持久化后端
}

// storageBackend 接口已合并至 storage.Client，见 plugins/core/storage

// NewPlugin 创建并返回一个 Conversation Plugin 实例。
// 配合 Descriptor(p) 使用，适合需要在注册前持有插件引用的场景（如测试）：
//
//	p := conversation.NewPlugin()
//	pm.Register(conversation.Descriptor(p))
//	p.Start(ctx, machine)
func NewPlugin() *Plugin {
	return &Plugin{}
}

// Descriptor 根据已有 Plugin 实例生成插件描述符，供 pm.Register 使用。
func Descriptor(p *Plugin) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:         "conversation",
		Version:      "1.0.0",
		Deps:         []string{},
		OptionalDeps: []string{"storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "Multi-step conversation/FSM plugin with cross-message state tracking",
			Category:    "core",
			Tags:        []string{"conversation", "fsm", "session"},
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			if sb, ok := plugin.Try[storage.Plugin](ctx, "storage"); ok {
				p.store = sb.NS("conversation")
				p.restoreSessions()
			}
			// 后台定期 GC 过期会话，防止 sync.Map 无限增长（Bug 2.4 修复）
			ctx.Go(func(runCtx stdctx.Context) {
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

// New 创建会话状态机插件描述符（便捷入口，内部创建 Plugin 实例）。
// 若需要持有 Plugin 引用，改用 NewPlugin() + Descriptor()。
func New() *plugin.Descriptor {
	return Descriptor(NewPlugin())
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

// Start begins a conversation session for the current user and sends the first prompt.
func (p *Plugin) Start(ctx *eventctx.Context, m *Machine) error {
	p.machines.Store(m.name, m)
	userID := extractUserID(ctx)
	if userID == "" {
		return fmt.Errorf("conversation: cannot determine user ID")
	}
	now := time.Now()
	session := &Session{
		ID:        userID + ":" + m.name,
		UserID:    userID,
		Machine:   m.name,
		StepIdx:   0,
		Data:      make(map[string]any),
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
func (p *Plugin) Dispatch(ctx *eventctx.Context) error {
	userID := extractUserID(ctx)
	if userID == "" {
		return nil
	}
	var session *Session
	p.sessions.Range(func(k, v any) bool {
		s := v.(*Session)
		if s.UserID == userID && !isExpired(s) {
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
		if userID == "" {
			return nil
		}
		v, ok := p.sessions.Load(userID + ":" + machineName)
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
	err := m.steps[session.StepIdx].handle(ctx, session)
	if errors.Is(err, ErrStepDone) {
		p.sessions.Delete(session.ID)
		if m.onDone != nil {
			return m.onDone(ctx, session)
		}
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

// Cancel cancels a user's named machine session.
func (p *Plugin) Cancel(userID, machineName string) {
	p.sessions.Delete(userID + ":" + machineName)
}

// InSession returns a Rule that matches when the user has an active session for machineName.
func (p *Plugin) InSession(machineName string) eventctx.Rule {
	return func(ctx *eventctx.Context) bool {
		userID := extractUserID(ctx)
		if userID == "" {
			return false
		}
		v, ok := p.sessions.Load(userID + ":" + machineName)
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

// HasActiveSession reports whether the user has any active session.
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

// persistSessions 将所有活跃会话保存到 storage
func (p *Plugin) persistSessions() {
	if p.store == nil {
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
	if err := storage.Set(context.Background(), p.store, "sessions", sessions, 0); err != nil {
		logger.WithError(err).Warn("[Conversation] Failed to persist sessions")
	} else {
		logger.Infof("[Conversation] Persisted %d sessions", len(sessions))
	}
}

// restoreSessions 从 storage 恢复会话
func (p *Plugin) restoreSessions() {
	if p.store == nil {
		return
	}
	sessions, err := storage.Get[map[string]*Session](context.Background(), p.store, "sessions")
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
	logger.Infof("[Conversation] Restored %d sessions from storage", count)
}
func extractUserID(ctx *eventctx.Context) string {
	return ctx.GetSenderInfo().ID
}
func sendPrompt(ctx *eventctx.Context, prompt string) {
	if _, err := ctx.Reply(platform.TextMessage(prompt)); err != nil {
		logger.WithError(err).Warn("[Conversation] Failed to send prompt")
	}
}
