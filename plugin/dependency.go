package plugin

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// DependencyInjector 依赖注入接口
// 插件可以选择实现此接口来自定义依赖注入行为
type DependencyInjector interface {
	// InjectDependency 注入单个依赖
	InjectDependency(name string, dep interface{}) error
}

// AutoDependencyProvider 自动依赖提取接口
// 实现此接口的插件支持自动依赖提取
type AutoDependencyProvider interface {
	// GetDependencyFields 返回用于依赖注入的字段信息
	GetDependencyFields() []DependencyField
}

// DependencyField 依赖字段信息
type DependencyField struct {
	Name       string        // 字段名称
	PluginName string        // 依赖的插件名称
	FieldValue reflect.Value // 字段的反射值
	Required   bool          // 是否必需
}

// ExtractDependencies 从插件结构体中自动提取依赖
// 通过反射扫描带有 `inject:"plugin:xxx"` 标签的字段
// plugin: 可以是任何实现了 Plugin 接口的实例，或包含依赖字段的结构体
func ExtractDependencies(plugin interface{}) []string {
	deps := make(map[string]bool)

	// 获取插件的反射值
	v := reflect.ValueOf(plugin)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	// 扫描所有字段
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 跳过未导出的字段
		if !field.IsExported() {
			continue
		}

		// 检查 inject 标签
		tag := field.Tag.Get("inject")
		if tag == "" {
			continue
		}

		// 解析标签格式: plugin:permission 或 plugin:permission,required
		parts := strings.Split(tag, ",")
		if len(parts) == 0 {
			continue
		}

		// 提取插件名称
		depSpec := strings.TrimSpace(parts[0])
		if strings.HasPrefix(depSpec, "plugin:") {
			depName := strings.TrimPrefix(depSpec, "plugin:")
			depName = strings.TrimSpace(depName)
			if depName != "" {
				deps[depName] = true
				logger.Debugf("[Plugin] Detected dependency from tag: %s -> %s", field.Name, depName)
			}
		}
	}

	// 转换为切片
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}

	return result
}

// InjectDependencies 自动注入依赖到插件
// plugin: 可以是任何包含依赖字段的结构体
// deps: 依赖映射 {"permission": permissionPlugin, ...}
func InjectDependencies(plugin interface{}, deps map[string]interface{}) error {
	// 检查插件是否实现了自定义注入接口
	if injector, ok := plugin.(DependencyInjector); ok {
		for name, dep := range deps {
			if err := injector.InjectDependency(name, dep); err != nil {
				logger.WithError(err).Warnf("[Plugin] Custom injection failed for %s", name)
			}
		}
		return nil
	}

	// 使用反射自动注入
	return injectDependenciesReflect(plugin, deps)
}

// injectDependenciesReflect 使用反射自动注入依赖
func injectDependenciesReflect(plugin interface{}, deps map[string]interface{}) error {
	v := reflect.ValueOf(plugin)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if !v.CanAddr() {
		return fmt.Errorf("plugin must be addressable for dependency injection")
	}

	t := v.Type()
	injectedCount := 0

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 跳过未导出的字段
		if !field.IsExported() {
			continue
		}

		// 检查 inject 标签
		tag := field.Tag.Get("inject")
		if tag == "" {
			continue
		}

		// 解析标签
		parts := strings.Split(tag, ",")
		if len(parts) == 0 {
			continue
		}

		depSpec := strings.TrimSpace(parts[0])
		var depName string
		var required bool

		// 解析插件名称
		if strings.HasPrefix(depSpec, "plugin:") {
			depName = strings.TrimPrefix(depSpec, "plugin:")
			depName = strings.TrimSpace(depName)
		} else {
			// 支持非 plugin: 前缀的标签（如 manager, engine）
			depName = depSpec
		}

		// 检查是否标记为 required
		for j := 1; j < len(parts); j++ {
			if strings.TrimSpace(parts[j]) == "required" {
				required = true
			}
		}

		// 查找依赖
		dep, exists := deps[depName]
		if !exists {
			if required {
				return fmt.Errorf("required dependency '%s' not found for field '%s'", depName, field.Name)
			}
			logger.Debugf("[Plugin] Optional dependency '%s' not found, skipping", depName)
			continue
		}

		// 获取字段值
		fieldValue := v.Field(i)
		if !fieldValue.CanSet() {
			logger.Warnf("[Plugin] Field '%s' cannot be set, skipping", field.Name)
			continue
		}

		// 类型检查并设置
		depValue := reflect.ValueOf(dep)
		if !depValue.Type().AssignableTo(fieldValue.Type()) {
			return fmt.Errorf("dependency '%s' type mismatch: cannot assign %s to %s",
				depName, depValue.Type(), fieldValue.Type())
		}

		fieldValue.Set(depValue)
		injectedCount++
		logger.Debugf("[Plugin] Injected dependency '%s' into field '%s'", depName, field.Name)
	}

	if injectedCount > 0 {
		logger.Infof("[Plugin] Successfully injected %d dependencies", injectedCount)
	}

	return nil
}

// GetDependencyFields 获取插件的依赖字段信息
func GetDependencyFields(plugin interface{}) []DependencyField {
	fields := make([]DependencyField, 0)

	v := reflect.ValueOf(plugin)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get("inject")
		if tag == "" {
			continue
		}

		parts := strings.Split(tag, ",")
		if len(parts) == 0 {
			continue
		}

		depSpec := strings.TrimSpace(parts[0])
		var depName string
		var required bool

		if strings.HasPrefix(depSpec, "plugin:") {
			depName = strings.TrimPrefix(depSpec, "plugin:")
			depName = strings.TrimSpace(depName)
		} else {
			// 支持非 plugin: 前缀
			depName = depSpec
		}

		for j := 1; j < len(parts); j++ {
			if strings.TrimSpace(parts[j]) == "required" {
				required = true
			}
		}

		// 只记录 plugin: 前缀的依赖
		if strings.HasPrefix(depSpec, "plugin:") {
			fields = append(fields, DependencyField{
				Name:       field.Name,
				PluginName: depName,
				FieldValue: v.Field(i),
				Required:   required,
			})
		}
	}

	return fields
}

// ValidateDependencies 验证插件的依赖是否已满足
func ValidateDependencies(plugin interface{}, availablePlugins map[string]Plugin) error {
	fields := GetDependencyFields(plugin)

	for _, field := range fields {
		if field.Required {
			if _, exists := availablePlugins[field.PluginName]; !exists {
				return fmt.Errorf("required dependency '%s' (field '%s') is not available",
					field.PluginName, field.Name)
			}
		}
	}

	return nil
}
