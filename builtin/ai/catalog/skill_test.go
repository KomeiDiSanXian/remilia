package catalog

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

func TestUserSkillNamePattern(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"my_skill", true},
		{"skill-123", true},
		{"a", true},
		{"", false},
		{"a b", false},
		{"abc!", false},
		{"中文", false},
	}
	for _, tt := range tests {
		got := UserSkillNamePattern.MatchString(tt.name)
		if got != tt.want {
			t.Errorf("UserSkillNamePattern.MatchString(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestApplyDefaultParamSchema(t *testing.T) {
	skill := toolkit.Skill{
		Name:        "test",
		OwnerID:     toolkit.OwnerSystem,
		Description: "test",
	}
	ApplyDefaultParamSchema(&skill)
	if len(skill.Parameters.Properties) == 0 {
		t.Error("expected default parameters to be applied")
	}
	if _, ok := skill.Parameters.Properties["query"]; !ok {
		t.Error("expected query parameter in default schema")
	}
}

func TestApplyDefaultParamSchemaExisting(t *testing.T) {
	skill := toolkit.Skill{
		Name:        "test",
		OwnerID:     toolkit.OwnerSystem,
		Description: "test",
		Parameters: protocol.ToolParamSchema{
			Type: "object",
			Properties: map[string]protocol.ToolParamSchema{
				"custom": {Type: "string"},
			},
		},
	}
	ApplyDefaultParamSchema(&skill)
	if _, ok := skill.Parameters.Properties["query"]; ok {
		t.Error("should not add default when custom params exist")
	}
}
