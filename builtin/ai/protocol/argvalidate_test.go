package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateArgumentsEmptySchemaPasses(t *testing.T) {
	if err := ValidateArguments(ToolParamSchema{}, map[string]any{"anything": 1}); err != nil {
		t.Fatalf("empty schema should accept any arguments: %v", err)
	}
}

func TestValidateArgumentsRequired(t *testing.T) {
	schema := ToolParamSchema{
		Type: "object",
		Properties: map[string]ToolParamSchema{
			"path": {Type: "string"},
			"mode": {Type: "string"},
		},
		Required: []string{"path"},
	}
	if err := ValidateArguments(schema, map[string]any{"mode": "r"}); err == nil ||
		!strings.Contains(err.Error(), "path") {
		t.Fatalf("missing required field error = %v", err)
	}
	if err := ValidateArguments(schema, map[string]any{"path": "/tmp", "extra": true}); err != nil {
		t.Fatalf("unknown properties should be allowed: %v", err)
	}
}

func TestValidateArgumentsTypes(t *testing.T) {
	schema := ToolParamSchema{
		Type: "object",
		Properties: map[string]ToolParamSchema{
			"text":    {Type: "string"},
			"count":   {Type: "integer"},
			"ratio":   {Type: "number"},
			"enabled": {Type: "boolean"},
		},
	}
	cases := []struct {
		name string
		args map[string]any
		ok   bool
	}{
		{"valid", map[string]any{"text": "x", "count": 3, "ratio": 0.5, "enabled": true}, true},
		{"integer from float", map[string]any{"count": 3.0}, true},
		{"fractional integer", map[string]any{"count": 3.5}, false},
		{"number from int", map[string]any{"ratio": 4}, true},
		{"string for number", map[string]any{"ratio": "4"}, false},
		{"bool for string", map[string]any{"text": true}, false},
		{"string for bool", map[string]any{"enabled": "true"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArguments(schema, tc.args)
			if tc.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateArgumentsNumberSources(t *testing.T) {
	schema := ToolParamSchema{Type: "object", Properties: map[string]ToolParamSchema{
		"value": {Type: "number"},
	}}
	for _, value := range []any{int(1), int64(1), uint8(1), float32(1.5), float64(1.5), json.Number("2.5")} {
		if err := ValidateArguments(schema, map[string]any{"value": value}); err != nil {
			t.Fatalf("value %T(%v) should be a valid number: %v", value, value, err)
		}
	}
	if err := ValidateArguments(schema, map[string]any{"value": json.Number("not-a-number")}); err == nil {
		t.Fatal("invalid json.Number should be rejected")
	}
}

func TestValidateArgumentsEnum(t *testing.T) {
	schema := ToolParamSchema{
		Type: "object",
		Properties: map[string]ToolParamSchema{
			"status": {Type: "string", Enum: []string{"pending", "done"}},
		},
	}
	if err := ValidateArguments(schema, map[string]any{"status": "done"}); err != nil {
		t.Fatalf("valid enum value rejected: %v", err)
	}
	if err := ValidateArguments(schema, map[string]any{"status": "other"}); err == nil {
		t.Fatal("unknown enum value should be rejected")
	}
	if err := ValidateArguments(schema, map[string]any{"status": 1}); err == nil {
		t.Fatal("non-string enum value should be rejected")
	}
}

func TestValidateArgumentsArrayItemsAndNestedObjects(t *testing.T) {
	schema := ToolParamSchema{
		Type: "object",
		Properties: map[string]ToolParamSchema{
			"steps": {
				Type: "array",
				Items: &ToolParamSchema{
					Type: "object",
					Properties: map[string]ToolParamSchema{
						"id":    {Type: "string"},
						"order": {Type: "integer"},
					},
					Required: []string{"id"},
				},
			},
		},
	}

	if err := ValidateArguments(schema, map[string]any{
		"steps": []any{
			map[string]any{"id": "a", "order": 1},
			map[string]any{"id": "b"},
		},
	}); err != nil {
		t.Fatalf("valid nested array rejected: %v", err)
	}

	if err := ValidateArguments(schema, map[string]any{
		"steps": []any{map[string]any{"order": 1}},
	}); err == nil || !strings.Contains(err.Error(), "arguments.steps[0]") || !strings.Contains(err.Error(), "id") {
		t.Fatalf("nested required error = %v", err)
	}

	if err := ValidateArguments(schema, map[string]any{"steps": "not-an-array"}); err == nil {
		t.Fatal("non-array value should be rejected")
	}

	// 具体切片类型（非 []any）同样可校验。
	if err := ValidateArguments(schema, map[string]any{
		"steps": []map[string]any{{"id": "a"}},
	}); err != nil {
		t.Fatalf("typed slice rejected: %v", err)
	}
}

func TestValidateArgumentsImplicitObjectAndUnknownType(t *testing.T) {
	// 只声明 properties 的对象按 object 处理。
	schema := ToolParamSchema{Properties: map[string]ToolParamSchema{"name": {Type: "string"}}}
	if err := ValidateArguments(schema, map[string]any{"name": 1}); err == nil {
		t.Fatal("implicit object should validate its properties")
	}

	// 未识别的类型关键字不参与校验（外部 Schema 兼容）。
	if err := ValidateArguments(ToolParamSchema{Type: "weird"}, map[string]any{"x": 1}); err != nil {
		t.Fatalf("unknown type keyword should be ignored: %v", err)
	}
}
