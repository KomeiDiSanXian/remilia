package fsm

// FSMDescriptor is a self-contained package for an FSM definition,
// analogous to [plugin.Descriptor] but without Matcher registration.
//
// FSMDescriptors are registered via [Manager.Register] rather than
// through the plugin system's RegistryWriter. The Adaptive Router
// checks for active FSM sessions during event dispatch and calls
// [Engine.TryTransition] directly.
type FSMDescriptor struct {
	// Name is the unique identifier for this FSM descriptor.
	Name string
	// Version is an optional semver string for the FSM definition.
	Version string
	// FSM is the FSM definition.
	FSM *FSM
	// ConfigSchema is an optional schema for declarative configuration.
	ConfigSchema any
}

// Validate checks that the FSMDescriptor has a non-empty Name and a
// valid non-nil FSM definition.
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
