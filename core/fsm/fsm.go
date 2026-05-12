// Package fsm provides a simple, concurrency-safe finite state machine (FSM)
// engine designed for multi-step conversational flows in IM bots.
//
// Instead of hand-writing `if state == x` inside event handlers, users declare
// states, events, transitions, and callbacks declaratively via [FSM] and [Event].
// The [Engine] manages sessions, transitions, timeouts, and cleanup.
//
// Basic usage:
//
//	mgr := fsm.NewManager(nil)
//	signup := &fsm.FSM{
//	    Name: "signup", Initial: "idle",
//	    Events: []fsm.Event{
//	        {Name: "start", From: "idle", To: "ask_name",
//	            Match:  func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "/signup" },
//	            Action: func(ctx *fsm.FSMContext) error {
//	                ctx.Reply(platform.TextMessage("请输入姓名："))
//	                return nil
//	            }},
//	    },
//	}
//	mgr.Register(&fsm.FSMDescriptor{Name: "signup", FSM: signup})
package fsm

import (
	"fmt"
	"sync"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
)

// State represents a single node in the finite state machine.
// It is a string type alias for type-safe state references.
type State string

// Event defines a single transition rule in the FSM.
//
// When the current session state matches [From] and [Match] returns true,
// the FSM engine executes [Action] (if non-nil), then moves to [To].
//
// Use From="*" to match any current state (wildcard transition).
type Event struct {
	// Name is a human-readable identifier for this transition (used in error messages).
	Name string
	// From is the source state. Use "*" to match any state.
	From State
	// To is the destination state after a successful transition.
	To State
	// Match is the predicate that determines whether this event should fire.
	// It receives the original event context (not the FSMContext).
	Match func(ctx *corectx.Context) bool
	// Action is the side effect executed during the transition.
	// It receives [FSMContext] which embeds the original context and
	// carries the current session data.
	Action func(ctx *FSMContext) error
}

// FSM is a declarative finite state machine definition.
type FSM struct {
	// Name uniquely identifies this FSM within an [Engine].
	Name string
	// Initial is the starting state for every new session.
	Initial State
	// Events is the ordered list of transition rules.
	// The first matching event in order wins.
	Events []Event
	// OnEnter is called after the FSM enters a state (after successful transition).
	// If the callback returns an error, the transition is rolled back.
	OnEnter map[State]func(ctx *FSMContext) error
	// OnExit is called before leaving a state.
	// If the callback returns an error, the transition is aborted.
	OnExit map[State]func(ctx *FSMContext) error
	// Timeout specifies the session TTL. Sessions older than this are
	// automatically expired and cleaned up. Zero means no timeout.
	Timeout time.Duration
}

// Validate checks the FSM definition for structural correctness:
//   - Name, Initial, and at least one Event are required
//   - Every Event must have a Name, From, To, and non-nil Match
//   - At least one Event must transition from the Initial state
//   - States referenced in OnEnter/OnExit must appear in some Event
func (f *FSM) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("fsm: Name is required")
	}
	if f.Initial == "" {
		return fmt.Errorf("fsm: Initial state is required")
	}
	if len(f.Events) == 0 {
		return fmt.Errorf("fsm: at least one Event is required")
	}

	allStates := make(map[State]bool)
	hasInitial := false
	for _, ev := range f.Events {
		if ev.Name == "" {
			return fmt.Errorf("fsm: event with empty Name")
		}
		if ev.From == "" {
			return fmt.Errorf("fsm: event %q has empty From state", ev.Name)
		}
		if ev.To == "" {
			return fmt.Errorf("fsm: event %q has empty To state", ev.Name)
		}
		if ev.Match == nil {
			return fmt.Errorf("fsm: event %q has nil Match func", ev.Name)
		}
		allStates[ev.From] = true
		allStates[ev.To] = true
		if ev.From == f.Initial {
			hasInitial = true
		}
	}
	if !hasInitial {
		return fmt.Errorf("fsm: no event transitions from initial state %q", f.Initial)
	}

	for state := range f.OnEnter {
		if !allStates[state] && state != f.Initial {
			return fmt.Errorf("fsm: OnEnter state %q not referenced in any event", state)
		}
	}
	for state := range f.OnExit {
		if !allStates[state] {
			return fmt.Errorf("fsm: OnExit state %q not referenced in any event", state)
		}
	}
	return nil
}

// Engine manages FSM definitions and session transitions.
//
// It is concurrency-safe: reads use RLock, writes use full Lock.
// Sessions are persisted through the [Storage] interface (default: [MemoryStorage]).
type Engine struct {
	mu     sync.RWMutex
	fsms   map[string]*FSM
	stores Storage
}

// NewEngine creates an FSM engine with the given [Storage] backend.
// If storage is nil, [NewMemoryStorage] is used as default.
func NewEngine(storage Storage) *Engine {
	if storage == nil {
		storage = NewMemoryStorage()
	}
	return &Engine{
		fsms:   make(map[string]*FSM),
		stores: storage,
	}
}

// Register adds an FSM definition to the engine.
// The FSM is validated before registration. Returns an error if:
//   - The FSM is nil or fails validation
//   - An FSM with the same Name is already registered
func (e *Engine) Register(f *FSM) error {
	if f == nil {
		return fmt.Errorf("fsm: cannot register nil FSM")
	}
	if err := f.Validate(); err != nil {
		return fmt.Errorf("fsm: register %q: %w", f.Name, err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.fsms[f.Name]; exists {
		return fmt.Errorf("fsm: FSM %q already registered", f.Name)
	}
	e.fsms[f.Name] = f
	return nil
}

// Unregister removes an FSM definition by name.
// Existing sessions for this FSM will fail on next [TryTransition] call.
func (e *Engine) Unregister(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.fsms, name)
}

// GetFSM returns the FSM definition by name, or nil if not found.
func (e *Engine) GetFSM(name string) *FSM {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.fsms[name]
}

// ListFSMs returns the names of all registered FSM definitions.
func (e *Engine) ListFSMs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.fsms))
	for n := range e.fsms {
		names = append(names, n)
	}
	return names
}

// TryTransition attempts to advance a session through the FSM.
//
// It looks up the session by sessionID, finds its associated FSM definition,
// iterates events in order, and executes the first matching transition.
//
// Returns:
//   - newState: the state after transition attempt
//   - ok: true if a transition occurred
//   - err: non-nil if OnExit, Action, or OnEnter returned an error
//
// If no session is found for sessionID, returns ("", false, nil).
func (e *Engine) TryTransition(ctx *corectx.Context, sessionID string) (State, bool, error) {
	if ctx == nil || sessionID == "" {
		return "", false, nil
	}

	session := e.stores.Get(sessionID)
	if session == nil {
		return "", false, nil
	}

	e.mu.RLock()
	fsm, exists := e.fsms[session.FSMName]
	e.mu.RUnlock()
	if !exists {
		return "", false, fmt.Errorf("fsm: FSM %q not found for session %q", session.FSMName, sessionID)
	}

	for _, event := range fsm.Events {
		if event.From != "*" && event.From != session.Current {
			continue
		}
		if !event.Match(ctx) {
			continue
		}
		fsmCtx := &FSMContext{
			Context:   ctx,
			SessionID: sessionID,
			Current:   session.Current,
			Data:      session.Data,
			FSM:       fsm,
		}
		if fn := fsm.OnExit[session.Current]; fn != nil {
			if err := fn(fsmCtx); err != nil {
				return session.Current, false, fmt.Errorf("fsm: OnExit %q: %w", session.Current, err)
			}
		}
		if event.Action != nil {
			if err := event.Action(fsmCtx); err != nil {
				return session.Current, false, fmt.Errorf("fsm: action %q: %w", event.Name, err)
			}
		}
		prev := session.Current
		session.Current = event.To
		session.UpdatedAt = time.Now().Unix()
		fsmCtx.Current = event.To
		if fn := fsm.OnEnter[event.To]; fn != nil {
			if err := fn(fsmCtx); err != nil {
				session.Current = prev
				return prev, false, fmt.Errorf("fsm: OnEnter %q rollback: %w", event.To, err)
			}
		}
		e.stores.Save(session)
		return event.To, true, nil
	}
	return session.Current, false, nil
}

// StartSession creates a new FSM session and sets its initial state.
//
// The sessionID should uniquely identify the conversation (e.g. "platform:chatID").
// If the FSM has an OnEnter handler for the Initial state, it is called immediately.
//
// Returns an error if:
//   - sessionID is empty
//   - fsmName is not registered
//   - a session with the same sessionID already exists
//   - the OnEnter initial callback returns an error (session creation is rolled back)
func (e *Engine) StartSession(ctx *corectx.Context, fsmName, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("fsm: sessionID is required")
	}
	e.mu.RLock()
	fsm, exists := e.fsms[fsmName]
	e.mu.RUnlock()
	if !exists {
		return fmt.Errorf("fsm: FSM %q not found", fsmName)
	}
	existing := e.stores.Get(sessionID)
	if existing != nil {
		return fmt.Errorf("fsm: session %q already exists for FSM %q", sessionID, existing.FSMName)
	}
	now := time.Now().Unix()
	session := &Session{
		ID:        sessionID,
		FSMName:   fsmName,
		Current:   fsm.Initial,
		Data:      make(map[string]any),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if fsm.Timeout > 0 {
		session.ExpireAt = time.Now().Add(fsm.Timeout).Unix()
	}
	e.stores.Save(session)
	if fn := fsm.OnEnter[fsm.Initial]; fn != nil {
		fsmCtx := &FSMContext{
			Context:   ctx,
			SessionID: sessionID,
			Current:   fsm.Initial,
			Data:      session.Data,
			FSM:       fsm,
		}
		if err := fn(fsmCtx); err != nil {
			e.stores.Delete(sessionID)
			return fmt.Errorf("fsm: OnEnter initial %q: %w", fsm.Initial, err)
		}
	}
	return nil
}

// EndSession terminates an FSM session and removes it from storage.
// The session is permanently deleted; subsequent [TryTransition] calls
// for this sessionID will return ("", false, nil).
func (e *Engine) EndSession(sessionID string) {
	e.stores.Delete(sessionID)
}

// GetSession returns a copy of the session data, or nil if not found or expired.
func (e *Engine) GetSession(sessionID string) *Session {
	return e.stores.Get(sessionID)
}

// StartCleanup launches a background goroutine that periodically removes
// expired sessions from storage. The goroutine stops when stop is closed.
//
// If interval is <= 0, a default of 1 minute is used.
func (e *Engine) StartCleanup(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.stores.Cleanup(time.Now().Unix())
			case <-stop:
				return
			}
		}
	}()
}

// nowUnix returns the current Unix timestamp.
func nowUnix() int64 {
	return time.Now().Unix()
}
