package plugin

import "fmt"

// register_validate.go — RegisterV2 各阶段验证逻辑（拆分自 register.go）
// validateDescriptor 检查描述符的基础合法性（无锁，无 Manager 依赖）。
func validateDescriptor(desc *PluginDescriptor) error {
	if desc == nil {
		return fmt.Errorf("plugin descriptor is nil")
	}
	if desc.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if desc.Setup == nil {
		return fmt.Errorf("plugin setup function is required")
	}
	return nil
}

// checkDependencies 检查所有声明依赖的存在性与就绪状态。调用方须持有 pm.mu。
func checkDependencies(pm *Manager, desc *PluginDescriptor, registeredList func() []string) error {
	name := desc.Name
	for _, rawDep := range desc.Deps {
		spec := parseDepSpec(rawDep)
		depInst, exists := pm.plugins[spec.name]
		if !exists {
			return &PluginError{
				PluginName:        name,
				Operation:         "register",
				Cause:             fmt.Errorf("missing required dependency %q", spec.name),
				RegisteredPlugins: registeredList(),
				Hint:              fmt.Sprintf("register %q before %q", spec.name, name),
			}
		}
		state := depInst.GetState()
		if state != Loaded {
			return &PluginError{
				PluginName:        name,
				Operation:         "register",
				Cause:             fmt.Errorf("dependency %q is not ready (state: %s)", spec.name, state),
				RegisteredPlugins: registeredList(),
				Hint:              "register plugins in dependency order, or use RegisterMultipleV2Atomic() for automatic ordering",
			}
		}
	}
	return nil
}

// validateVersionConstraints 检查依赖的版本约束是否满足。调用方须持有 pm.mu。
func validateVersionConstraints(pm *Manager, desc *PluginDescriptor) error {
	for _, rawDep := range desc.Deps {
		spec := parseDepSpec(rawDep)
		if spec.constraint == "" {
			continue
		}
		depInst, exists := pm.plugins[spec.name]
		if !exists {
			continue
		}
		ok, _ := checkVersionConstraint(depInst.desc.Version, spec.constraint)
		if !ok {
			return &VersionConstraintError{
				Plugin:     desc.Name,
				Dependency: spec.name,
				Required:   spec.constraint,
				Have:       depInst.desc.Version,
			}
		}
	}
	return nil
}

// validateConfigSchema 若声明了 ConfigSchema，用当前 Config 校验。调用方须持有 pm.mu。
func validateConfigSchema(name string, desc *PluginDescriptor, config Config) error {
	if config == nil || desc.Advanced == nil || desc.Advanced.ConfigSchema == nil {
		return nil
	}
	return ValidateConfigSchema(name, desc.Advanced.ConfigSchema, config)
}
