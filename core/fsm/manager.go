package fsm

import (
	"fmt"
	"sync"
)

var (
	// ErrFSMDescriptorNameRequired is returned when an FSMDescriptor has an empty Name.
	ErrFSMDescriptorNameRequired = fmt.Errorf("fsm: FSMDescriptor.Name is required")
	// ErrFSMDescriptorNilFSM is returned when an FSMDescriptor has a nil FSM field.
	ErrFSMDescriptorNilFSM = fmt.Errorf("fsm: FSMDescriptor.FSM is nil")
)

// Manager manages FSM descriptors and provides access to the underlying FSM engine.
//
// Usage:
//
//	mgr := fsm.NewManager(nil)
//	mgr.Register(&fsm.FSMDescriptor{Name: "signup", FSM: signupFSM})
//	eng := mgr.GetEngine()
type Manager struct {
	engine   *Engine
	fsmDescs map[string]*FSMDescriptor
	mu       sync.RWMutex
}

// NewManager creates an FSM manager with the given storage backend.
// If storage is nil, [NewMemoryStorage] is used.
func NewManager(storage Storage) *Manager {
	if storage == nil {
		storage = NewMemoryStorage()
	}
	return &Manager{
		engine:   NewEngine(storage),
		fsmDescs: make(map[string]*FSMDescriptor),
	}
}

// Register validates and registers an FSM descriptor.
//
// Returns an error if:
//   - desc is nil or fails validation
//   - an FSM descriptor with the same Name is already registered
//   - the underlying FSM fails validation
func (m *Manager) Register(desc *FSMDescriptor) error {
	if desc == nil {
		return fmt.Errorf("fsm: cannot register nil FSMDescriptor")
	}
	if err := desc.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.fsmDescs[desc.Name]; exists {
		return fmt.Errorf("fsm: FSMDescriptor %q already registered", desc.Name)
	}
	if err := m.engine.Register(desc.FSM); err != nil {
		return err
	}
	m.fsmDescs[desc.Name] = desc
	return nil
}

// Unregister removes an FSM descriptor from the manager and its FSM from the engine.
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.fsmDescs[name]; !exists {
		return fmt.Errorf("fsm: FSMDescriptor %q not found", name)
	}
	m.engine.Unregister(name)
	delete(m.fsmDescs, name)
	return nil
}

// GetEngine returns the underlying FSM engine for direct access.
// Most users should use [Engine] which is an alias for this method.
func (m *Manager) GetEngine() *Engine {
	return m.engine
}

// Engine is an alias for [Manager.GetEngine].
func (m *Manager) Engine() *Engine {
	return m.engine
}

// ListDescriptors returns the names of all registered FSM descriptors.
func (m *Manager) ListDescriptors() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.fsmDescs))
	for n := range m.fsmDescs {
		names = append(names, n)
	}
	return names
}
