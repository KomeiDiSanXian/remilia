package command

import (
	"strings"
	"testing"
)

func TestEnhancedSystem_ArgTypes_ParseValueMatrix(t *testing.T) {
	t.Parallel()

	mk := func(def *Definition) *Parser {
		p := NewParser("/")
		p.Register(def)
		return p
	}

	tests := []struct {
		name      string
		input     string
		def       *Definition
		check     func(t *testing.T, parsed *Parsed)
		wantError string
	}{
		{
			name:  "int argument ok",
			input: "/age 42",
			def: &Definition{
				Name:      "age",
				Arguments: []*Argument{{Name: "n", Type: ArgTypeInt, Required: true}},
			},
			check: func(t *testing.T, parsed *Parsed) {
				if got := parsed.GetInt("n"); got != 42 {
					t.Fatalf("expected 42, got %d", got)
				}
			},
		},
		{
			name:      "int argument invalid",
			input:     "/age abc",
			def:       &Definition{Name: "age", Arguments: []*Argument{{Name: "n", Type: ArgTypeInt, Required: true}}},
			wantError: "argument n",
		},
		{
			name:  "float argument ok",
			input: "/pi 3.14",
			def: &Definition{
				Name:      "pi",
				Arguments: []*Argument{{Name: "x", Type: ArgTypeFloat, Required: true}},
			},
			check: func(t *testing.T, parsed *Parsed) {
				if got := parsed.GetFloat("x"); got != 3.14 {
					t.Fatalf("expected 3.14, got %v", got)
				}
			},
		},
		{
			name:      "float argument invalid",
			input:     "/pi abc",
			def:       &Definition{Name: "pi", Arguments: []*Argument{{Name: "x", Type: ArgTypeFloat, Required: true}}},
			wantError: "argument x",
		},
		{
			name:  "bool argument ok",
			input: "/b yes",
			def: &Definition{
				Name:      "b",
				Arguments: []*Argument{{Name: "v", Type: ArgTypeBool, Required: true}},
			},
			check: func(t *testing.T, parsed *Parsed) {
				if got := parsed.GetBool("v"); got != true {
					t.Fatalf("expected true, got %v", got)
				}
			},
		},
		{
			name:      "bool argument invalid",
			input:     "/b maybe",
			def:       &Definition{Name: "b", Arguments: []*Argument{{Name: "v", Type: ArgTypeBool, Required: true}}},
			wantError: "invalid boolean value",
		},
		{
			name:  "bool flag ok",
			input: "/f -v on",
			def: &Definition{
				Name:  "f",
				Flags: []*Flag{{Name: "verbose", ShortName: "v", Type: ArgTypeBool, Required: true}},
			},
			check: func(t *testing.T, parsed *Parsed) {
				if got := parsed.GetBool("verbose"); got != true {
					t.Fatalf("expected true, got %v", got)
				}
			},
		},
		{
			name:      "bool flag invalid (case sensitive)",
			input:     "/f --verbose TRUE",
			def:       &Definition{Name: "f", Flags: []*Flag{{Name: "verbose", Type: ArgTypeBool, Required: true}}},
			wantError: "flag --verbose",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := mk(tt.def)
			parsed, err := p.Parse(tt.input)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tt.check != nil {
				tt.check(t, parsed)
			}
		})
	}
}
