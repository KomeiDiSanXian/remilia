package plugin

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/errutil"
)

// PluginError 插件操作错误，携带丰富的诊断上下文
//
// 示例输出：
//
//	plugin "antispam": register failed — missing required dependency "storage"
//	  currently registered: [permission, cache, acl]
//	  hint: register "storage" before "antispam"
type PluginError struct {
	// PluginName 发生错误的插件名
	PluginName string
	// Operation 失败的操作（"register", "load", "unload", "reload", "dependency"）
	Operation string
	// Cause 原始错误
	Cause error
	// Hint 给开发者的修复建议（可选）
	Hint string
	// RegisteredPlugins 发生错误时已注册的插件列表（辅助诊断）
	RegisteredPlugins []string
}

func (e *PluginError) Error() string {
	var sb strings.Builder
	if e.PluginName != "" {
		sb.WriteString(fmt.Sprintf("plugin %q: %s failed", e.PluginName, e.Operation))
	} else {
		sb.WriteString(fmt.Sprintf("%s failed", e.Operation))
	}
	if e.Cause != nil {
		sb.WriteString(" — ")
		sb.WriteString(e.Cause.Error())
	}
	if len(e.RegisteredPlugins) > 0 {
		sb.WriteString(fmt.Sprintf("\n  currently registered: [%s]", strings.Join(e.RegisteredPlugins, ", ")))
	}
	if e.Hint != "" {
		sb.WriteString("\n  hint: ")
		sb.WriteString(e.Hint)
	}
	return sb.String()
}

func (e *PluginError) Unwrap() error {
	return e.Cause
}

// DependencyError 依赖错误（保留以兼容已有代码）
type DependencyError struct {
	Plugin     string
	Dependency string
	Err        error
}

func (e *DependencyError) Error() string {
	return fmt.Sprintf("plugin %s: dependency %s not found", e.Plugin, e.Dependency)
}

func (e *DependencyError) Unwrap() error {
	return e.Err
}

// CircularDependencyError 循环依赖错误
type CircularDependencyError struct {
	Cycle []string
}

func (e *CircularDependencyError) Error() string {
	return fmt.Sprintf("circular dependency detected: %v", e.Cycle)
}

func (e *CircularDependencyError) Unwrap() error {
	return errutil.ErrCircularDependency
}

// VersionConstraintError 版本约束不满足错误
type VersionConstraintError struct {
	Plugin     string
	Dependency string
	Required   string
	Have       string
}

func (e *VersionConstraintError) Error() string {
	return fmt.Sprintf(
		"plugin %q: dependency %q version constraint not satisfied (required: %s, have: %s)",
		e.Plugin, e.Dependency, e.Required, e.Have,
	)
}

func (e *VersionConstraintError) Unwrap() error {
	return errutil.ErrDependencyNotFound
}

// SchemaValidationError 配置 Schema 验证错误
type SchemaValidationError struct {
	Plugin string
	Field  string
	Reason string
}

func (e *SchemaValidationError) Error() string {
	return fmt.Sprintf("plugin %q: config validation failed for field %q: %s", e.Plugin, e.Field, e.Reason)
}
