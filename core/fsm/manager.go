package fsm

import (
	"fmt"
	"sync"
)

var (
	// ErrFSMDescriptorNameRequired 当 FSMDescriptor 的 Name 为空时返回。
	ErrFSMDescriptorNameRequired = fmt.Errorf("fsm: FSMDescriptor.Name is required")
	// ErrFSMDescriptorNilFSM 当 FSMDescriptor 的 FSM 字段为 nil 时返回。
	ErrFSMDescriptorNilFSM = fmt.Errorf("fsm: FSMDescriptor.FSM is nil")
)

// Manager 管理 FSM descriptor 并提供对底层 FSM 引擎的访问。
type Manager struct {
	engine   *Engine
	fsmDescs map[string]*FSMDescriptor
	mu       sync.RWMutex
}

// NewManager 创建一个使用指定存储后端的 FSM 管理器。如果 storage 为 nil，默认使用 [NewMemoryStorage]。
func NewManager(storage Storage) *Manager {
	if storage == nil {
		storage = NewMemoryStorage()
	}
	return &Manager{
		engine:   NewEngine(storage),
		fsmDescs: make(map[string]*FSMDescriptor),
	}
}

// Register 校验并注册一个 FSM descriptor。返回错误的情况：
//   - desc 为 nil 或校验失败
//   - 同名 FSM descriptor 已经注册
//   - 底层 FSM 校验失败
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

// Unregister 从管理器中移除一个 FSM descriptor，并从引擎中移除其 FSM。
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

// GetEngine 返回底层 FSM 引擎以供直接访问。大多数用户应使用 [Engine]——它是此方法的别名。
func (m *Manager) GetEngine() *Engine {
	return m.engine
}

// Engine 是 [Manager.GetEngine] 的别名。
func (m *Manager) Engine() *Engine {
	return m.engine
}

// ListDescriptors 返回所有已注册 FSM descriptor 的名称列表。
func (m *Manager) ListDescriptors() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.fsmDescs))
	for n := range m.fsmDescs {
		names = append(names, n)
	}
	return names
}
