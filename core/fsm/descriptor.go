package fsm

// FSMDescriptor 是 FSM 定义的自包含包，类似于 [plugin.Descriptor]，
// 但不包含 Matcher 注册。通过 [Manager.Register] 注册，
// Adaptive Router 在事件分发时检查活跃的 FSM 会话并直接调用 [Engine.TryTransition]。
type FSMDescriptor struct {
	// Name 是此 FSM descriptor 的唯一标识。
	Name string
	// Version 是 FSM 定义的可选 semver 版本号。
	Version string
	// FSM 是 FSM 定义。
	FSM *FSM
	// ConfigSchema 是声明式配置的可选 schema。
	ConfigSchema any
}

// Validate 检查 FSMDescriptor 具有非空 Name 和有效的非 nil FSM 定义。
func (d *FSMDescriptor) Validate() error {
	if d.Name == "" {
		return ErrFSMDescriptorNameRequired
	}
	if d.FSM == nil {
		return ErrFSMDescriptorNilFSM
	}
	if err := d.FSM.Validate(); err != nil {
		return err
	}
	return nil
}
