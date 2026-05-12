package fsm

import corectx "github.com/KomeiDiSanXian/remilia/core/context"

// FSMContext is the context passed to [Event.Action], [FSM.OnEnter],
// and [FSM.OnExit] callbacks.
//
// It embeds [corectx.Context] so that all methods like [corectx.Context.Reply],
// [corectx.Context.GetMessageContent], etc. are directly available.
type FSMContext struct {
	*corectx.Context
	// SessionID is the unique identifier for this FSM session.
	SessionID string
	// Current is the current state at the time the callback is invoked.
	Current State
	// Data is the session's user-defined key-value store. Modifications
	// made here persist across transitions.
	Data map[string]any
	// FSM is the FSM definition this session belongs to.
	FSM *FSM
}
