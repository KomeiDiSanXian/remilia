package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractCommandFastWithPrefix(t *testing.T) {
	tests := []struct {
		name    string
		content string
		prefix  string
		want    string
	}{
		{"slash prefix", "/help arg1", "/", "/help"},
		{"bang prefix", "!help arg1", "!", "!help"},
		{"dollar prefix", "$help", "$", "$help"},
		{"dot prefix", ".help arg1 arg2", ".", ".help"},
		{"no match - different prefix", "/help", "!", ""},
		{"no match - no prefix", "help", "!", ""},
		{"empty content", "", "!", ""},
		{"empty prefix", "/help", "", ""},
		{"only prefix", "!", "!", "!"},
		{"only command no args", "!help", "!", "!help"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCommandFast(tt.content, tt.prefix)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractCommandAndArgsWithPrefix(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		prefix   string
		wantCmd  string
		wantArgs string
	}{
		{"slash prefix with args", "/help foo bar", "/", "/help", "foo bar"},
		{"bang prefix with args", "!help foo bar", "!", "!help", "foo bar"},
		{"bang prefix no args", "!help", "!", "!help", ""},
		{"no match returns full content", "hello world", "!", "", "hello world"},
		{"empty prefix returns full", "/help", "", "", ""},
		{"empty content", "", "!", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotArgs := ExtractCommandAndArgs(tt.content, tt.prefix)
			assert.Equal(t, tt.wantCmd, gotCmd)
			assert.Equal(t, tt.wantArgs, gotArgs)
		})
	}
}

func TestValidateCommandNameWithPrefix(t *testing.T) {
	tests := []struct {
		name    string
		cmdName string
		prefix  string
		wantErr bool
	}{
		{"valid / prefix", "/help", "/", false},
		{"valid ! prefix", "!help", "!", false},
		{"valid $ prefix", "$help", "$", false},
		{"valid dot prefix", ".help", ".", false},
		{"valid hyphenated", "/get-help", "/", false},
		{"empty name", "", "/", true},
		{"only prefix", "/", "/", true},
		{"wrong prefix", "!help", "/", true},
		{"wrong prefix 2", "/help", "!", true},
		{"no prefix at all", "help", "/", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommandName(tt.cmdName, tt.prefix)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
