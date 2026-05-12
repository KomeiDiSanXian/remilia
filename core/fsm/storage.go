package fsm

import "sync"

// Session represents a single FSM session's runtime state.
//
// It is persisted through the [Storage] interface between transition calls.
// The [Data] map carries user-defined key-value pairs across transitions.
type Session struct {
	// ID uniquely identifies this session (e.g. "platform:chatID").
	ID string
	// FSMName is the name of the FSM definition this session belongs to.
	FSMName string
	// Current is the current state of this session.
	Current State
	// Data is a user-defined key-value store that persists across transitions.
	Data map[string]any
	// ExpireAt is the Unix timestamp after which this session is considered expired.
	// Zero means no expiration.
	ExpireAt int64
	// CreatedAt is the Unix timestamp of session creation.
	CreatedAt int64
	// UpdatedAt is the Unix timestamp of the last transition.
	UpdatedAt int64
}

// Storage is the persistence interface for FSM sessions.
//
// Implementations must be concurrency-safe.
// The default implementation is [MemoryStorage].
type Storage interface {
	// Get retrieves a session by ID. Returns nil if not found or expired.
	Get(sessionID string) *Session
	// Save persists a session (create or update).
	Save(session *Session)
	// Delete removes a session by ID.
	Delete(sessionID string)
	// Cleanup removes all sessions with ExpireAt <= before.
	// Returns the number of removed sessions.
	Cleanup(before int64) int
}

// MemoryStorage is an in-memory implementation of [Storage].
//
// It uses a sync.RWMutex for concurrency safety and automatically
// treats expired sessions as not found in [Get].
type MemoryStorage struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewMemoryStorage creates an empty in-memory FSM session storage.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		sessions: make(map[string]*Session),
	}
}

// Get retrieves a session by ID. Returns nil if the session does
// not exist or has expired (ExpireAt in the past).
func (s *MemoryStorage) Get(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sess, ok := s.sessions[id]; ok {
		if sess.ExpireAt > 0 && sess.ExpireAt <= nowUnix() {
			return nil
		}
		return sess
	}
	return nil
}

// Save creates or updates a session.
func (s *MemoryStorage) Save(session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
}

// Delete removes a session by ID. No-op if the session does not exist.
func (s *MemoryStorage) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// Cleanup removes all sessions with ExpireAt <= before and returns
// the count of removed sessions.
func (s *MemoryStorage) Cleanup(before int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for id, sess := range s.sessions {
		if sess.ExpireAt > 0 && sess.ExpireAt <= before {
			delete(s.sessions, id)
			count++
		}
	}
	return count
}
