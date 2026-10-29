// argvalidate.go — 工具参数 Schema 的调用前校验。
//
// 模型生成的参数不能直接当作已授权的执行请求：调用前至少要做一次结构校验，
// 让"参数错误"在进入工具实现之前变成可读的失败结果，而不是让工具在内部
// 断言类型、panic 或带着空参数执行。
//
// 校验范围刻意保守：只处理 [ToolParamSchema] 能表达的约束（type / required /
// properties / items / enum），其余 JSON Schema 关键字（anyOf、oneOf、$ref、
// format 等）不参与判断。MCP 等外部来源的 Schema 可能使用这些关键字，
// 宁可放行也不误拒合法调用。

package protocol

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
)

// maxSchemaDepth 嵌套校验的最大深度。
//
// ToolParamSchema 是值类型、Items 是单链指针，正常构造不会成环；深度上限用于
// 防御手工构造的异常 Schema，超过后停止校验而不是递归失控。
const maxSchemaDepth = 16

// ValidateArguments 校验模型给出的工具参数是否满足 Schema。
//
// schema 未声明任何约束时直接放行（未声明参数的工具保持既有行为）。
func ValidateArguments(schema ToolParamSchema, args map[string]any) error {
	if !schemaHasConstraints(schema) {
		return nil
	}
	return validateValue(schema, args, "arguments", 0)
}

// schemaHasConstraints 报告 Schema 是否声明了需要校验的约束。
func schemaHasConstraints(schema ToolParamSchema) bool {
	return schema.Type != "" || len(schema.Properties) > 0 ||
		len(schema.Required) > 0 || schema.Items != nil || len(schema.Enum) > 0
}

// validateValue 校验单个值；path 用于生成面向模型的字段定位。
func validateValue(schema ToolParamSchema, value any, path string, depth int) error {
	if depth > maxSchemaDepth {
		return nil
	}
	typ := schema.Type
	// 只声明了 properties / items 的 Schema 按对象 / 数组处理，
	// 与模型侧对工具定义的常见写法一致。
	if typ == "" && len(schema.Properties) > 0 {
		typ = "object"
	}
	if typ == "" && schema.Items != nil {
		typ = "array"
	}

	if len(schema.Enum) > 0 {
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s 必须是字符串枚举值，实际为 %s", path, valueTypeName(value))
		}
		found := slices.Contains(schema.Enum, s)
		if !found {
			return fmt.Errorf("%s 必须是以下值之一：%s", path, strings.Join(schema.Enum, " / "))
		}
	}

	switch typ {
	case "", "any":
		return nil
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return typeMismatch(path, "object", value)
		}
		for _, name := range schema.Required {
			if _, present := obj[name]; !present {
				return fmt.Errorf("%s 缺少必填字段 %q", path, name)
			}
		}
		for name, propSchema := range schema.Properties {
			propValue, present := obj[name]
			if !present {
				continue
			}
			if err := validateValue(propSchema, propValue, path+"."+name, depth+1); err != nil {
				return err
			}
		}
		return nil
	case "array":
		items, ok := sliceValues(value)
		if !ok {
			return typeMismatch(path, "array", value)
		}
		if schema.Items != nil {
			for i, item := range items {
				if err := validateValue(*schema.Items, item, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return typeMismatch(path, "string", value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return typeMismatch(path, "boolean", value)
		}
	case "number":
		if !isNumber(value, false) {
			return typeMismatch(path, "number", value)
		}
	case "integer":
		if !isNumber(value, true) {
			return typeMismatch(path, "integer", value)
		}
	case "null":
		if value != nil {
			return typeMismatch(path, "null", value)
		}
	default:
		// 未识别的类型关键字：不参与校验，避免误拒外部 Schema 的合法写法。
	}
	return nil
}

// typeMismatch 构造类型不匹配的可读错误。
func typeMismatch(path, want string, value any) error {
	return fmt.Errorf("%s 类型不正确：期望 %s，实际为 %s", path, want, valueTypeName(value))
}

// sliceValues 把任意切片转换为 []any（JSON 反序列化得到 []any，
// 手工构造的调用可能使用 []string 等具体切片类型）。
func sliceValues(value any) ([]any, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// isNumber 报告值是否为数字；integer 为真时要求没有小数部分
// （JSON 数字统一解码为 float64，整数值的 3.0 视为合法整数）。
func isNumber(value any, integer bool) bool {
	if n, ok := value.(json.Number); ok {
		if integer {
			_, err := n.Int64()
			return err == nil
		}
		_, err := n.Float64()
		return err == nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		return !integer || f == math.Trunc(f)
	default:
		return false
	}
}

// valueTypeName 返回面向模型与日志的值类型名。
func valueTypeName(value any) string {
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map:
		return "object"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	default:
		return fmt.Sprintf("%T", value)
	}
}
