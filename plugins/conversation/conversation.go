package conversation

import (
	"fmt"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// StepHandler is the function type for a conversation step.
// Return nil to advance, ErrStepDone to finish early, or another error to abort.
type StepHandler func(ctx *eventctx.Context, session *Session) error

// ErrStepDone signals that the session should end immediately.
var ErrStepDone = fmt.Errorf("conversation: session done")

// ErrSessionNotFound is returned when no active session exists.
var ErrSessionNotFound = fmt.Errorf("conversation: session not found")

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
}

// New creates the conversation plugin descriptor.
// Use NewPlugin() to also get a direct reference to the Plugin API.
func New() *plugin.PluginDescriptor {
	_, desc := NewPlugin()
	return desc
}

// NewPlugin creates the conversation plugin and returns both the Plugin API and its descriptor.
func NewPlugin() (*Plugin, *plugin.PluginDescriptor) {
	p := &Plugin{}
	desc := &plugin.PluginDescriptor{
		Name:        "conversation",
		Version:     "1.0.0",
		Author:      "Remilia Team",
		Description: "Multi-step conversation/FSM plugin with cross-message state tracking",
		Category:    "core",
		Tags:        []string{"conversation", "fsm", "session"},
		Deps:        []string{},
		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[Conversation] Plugin loaded")
			ctx.Manager.GetContainer().Register("conversation", p)
			return nil
		},
	}
	return p, desc
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
	if err == ErrStepDone {
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
func extractUserID(ctx *eventctx.Context) string {
	if a := ctx.GetAuthor(); a != nil {
		if a.UserOpenID != "" {
			return a.UserOpenID
		}
		if a.MemberOpenID != "" {
			return a.MemberOpenID
		}
	}
	return ""
}
func sendPrompt(ctx *eventctx.Context, prompt string) {
	msg := &dto.Message{Type: dto.TextMessage, Content: prompt}
	switch ctx.GetEventType() {
	case dto.GroupAtMessageCreate:
		if _, err := ctx.ReplyGroup(msg); err != nil {
			logger.WithError(err).Warn("[Conversation] Failed to send group prompt")
		}
	default:
		if _, err := ctx.ReplyPrivate(msg); err != nil {
			logger.WithError(err).Warn("[Conversation] Failed to send private prompt")
		}
	}
}
