package plugin

import (
	"fmt"
	"reflect"
	"strings"
)

// SchemaField 描述一个配置字段的约束
//
// 示例：
//
//	Advanced: &plugin.Advanced{
//	    ConfigSchema: map[string]plugin.SchemaField{
//	        "mode":    {Type: "string", Required: true},
//	        "timeout": {Type: "duration", Required: false},
//	        "limit":   {Type: "int", Required: false, Default: 100},
//	    },
//	}
type SchemaField struct {
	// Type 期望的配置值类型："string", "int", "bool", "float64", "duration", "[]string", "map"
	// 空串表示不检查类型
	Type string

	// Required 若为 true 且配置中不存在该键，则返回 SchemaValidationError
	Required bool

	// Default 默认值（仅用于文档目的；Config 的 Get* 方法已各自接受 defaultVal）
	Default any
}

// ValidateConfigSchema 验证插件配置是否符合 schema 约束
//
// schema 支持两种形式：
//  1. map[string]SchemaField — 推荐，明确声明每个字段的类型和必填性
//  2. 任意 struct 指针       — 通过反射读取字段名（字段必须导出），Required 默认为 false
//
// 返回首个验证错误，若全部通过则返回 nil。
func ValidateConfigSchema(pluginName string, schema any, cfg Config) error {
	if schema == nil || cfg == nil {
		return nil
	}

	switch s := schema.(type) {
	case map[string]SchemaField:
		return validateMapSchema(pluginName, s, cfg)
	default:
		return validateStructSchema(pluginName, schema, cfg)
	}
}

// validateMapSchema 验证 map[string]SchemaField 形式的 schema
func validateMapSchema(pluginName string, schema map[string]SchemaField, cfg Config) error {
	all := cfg.GetAll()
	for field, def := range schema {
		val, exists := all[field]
		if !exists || val == nil {
			if def.Required {
				return &SchemaValidationError{
					Plugin: pluginName,
					Field:  field,
					Reason: "required field is missing",
				}
			}
			continue
		}
		if def.Type == "" {
			continue
		}
		if err := checkFieldType(pluginName, field, def.Type, val); err != nil {
			return err
		}
	}
	return nil
}

// validateStructSchema 通过反射验证 struct 形式的 schema（仅检查必填性）
func validateStructSchema(pluginName string, schema any, cfg Config) error {
	rv := reflect.ValueOf(schema)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil // 非 struct 类型跳过验证
	}
	rt := rv.Type()
	all := cfg.GetAll()

	for i := range rt.NumField() {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		// 读取 schema tag：`schema:"required"` 或 `schema:"optional"`
		tag := field.Tag.Get("schema")
		fieldName := field.Name
		// 也支持 `yaml:"fieldname"` 或 `json:"fieldname"` 作为配置键名
		if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			fieldName = strings.SplitN(jsonTag, ",", 2)[0]
		}

		val, exists := all[fieldName]
		if (!exists || val == nil) && tag == "required" {
			return &SchemaValidationError{
				Plugin: pluginName,
				Field:  fieldName,
				Reason: "required field is missing",
			}
		}
	}
	return nil
}

// checkFieldType 检查配置值是否与期望类型匹配
func checkFieldType(pluginName, field, expectedType string, val any) error {
	var ok bool
	switch expectedType {
	case "string":
		_, ok = val.(string)
	case "int":
		switch val.(type) {
		case int, int32, int64, float64:
			ok = true
		}
	case "bool":
		_, ok = val.(bool)
	case "float64":
		switch val.(type) {
		case float64, float32, int, int64:
			ok = true
		}
	case "duration":
		_, ok = val.(string) // duration 通常以字符串形式存储（如 "10s"）
		if !ok {
			switch val.(type) {
			case int, int64, float64:
				ok = true
			}
		}
	case "[]string":
		switch v := val.(type) {
		case []string:
			ok = true
		case []any:
			ok = true
			_ = v
		}
	case "map":
		switch val.(type) {
		case map[string]any:
			ok = true
		}
	default:
		return nil // 未知类型不验证
	}

	if !ok {
		return &SchemaValidationError{
			Plugin: pluginName,
			Field:  field,
			Reason: fmt.Sprintf("expected type %q, got %T", expectedType, val),
		}
	}
	return nil
}
