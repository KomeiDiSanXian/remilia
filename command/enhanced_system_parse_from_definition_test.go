package command

import (
	"strings"
	"testing"
)

func TestParseFromDefinition_SuccessAndFailures(t *testing.T) {
	t.Parallel()

	root := &Definition{
		Name:    "admin",
		Aliases: []string{"adm"},
		SubCommands: []*Definition{
			{
				Name: "ban",
				Arguments: []*Argument{
					{Name: "user", Type: ArgTypeString, Required: true},
				},
			},
		},
	}

	tests := []struct {
		name      string
		input     string
		prefix    string
		rootDef   *Definition
		wantPath  []string
		wantUser  string
		wantError string
	}{
		{
			name:     "exact match",
			input:    "/admin ban alice",
			prefix:   "/",
			rootDef:  root,
			wantPath: []string{"admin", "ban"},
			wantUser: "alice",
		},
		{
			name:     "root alias match",
			input:    "/adm ban bob",
			prefix:   "/",
			rootDef:  root,
			wantPath: []string{"admin", "ban"},
			wantUser: "bob",
		},
		{
			name:      "empty input",
			input:     "   ",
			prefix:    "/",
			rootDef:   root,
			wantError: "empty input",
		},
		{
			name:      "nil root def",
			input:     "/admin",
			prefix:    "/",
			rootDef:   nil,
			wantError: "root command definition is nil",
		},
		{
			name:      "command mismatch",
			input:     "/other",
			prefix:    "/",
			rootDef:   root,
			wantError: "command mismatch",
		},
		{
			name:      "prefix mismatch",
			input:     "/admin",
			prefix:    "!",
			rootDef:   root,
			wantError: "command mismatch",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := ParseFromDefinition(tt.input, tt.rootDef, tt.prefix)
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
			if parsed == nil {
				t.Fatalf("expected parsed, got nil")
			}
			if len(parsed.CommandPath) != len(tt.wantPath) {
				t.Fatalf("expected path %v, got %v", tt.wantPath, parsed.CommandPath)
			}
			for i := range tt.wantPath {
				if parsed.CommandPath[i] != tt.wantPath[i] {
					t.Fatalf("expected path %v, got %v", tt.wantPath, parsed.CommandPath)
				}
			}
			if tt.wantUser != "" {
				if got := parsed.GetString("user"); got != tt.wantUser {
					t.Fatalf("expected user=%q, got %q", tt.wantUser, got)
				}
			}
		})
	}
}
