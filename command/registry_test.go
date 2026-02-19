package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandRegistry_Register(t *testing.T) {
	t.Run("register_basic_command", func(t *testing.T) {
		registry := NewCommandRegistry()

		def := &Definition{
			Name:        "/hello",
			Description: "Say hello",
			Usage:       "/hello [name]",
		}

		err := registry.Register(def)
		assert.NoError(t, err)

		meta, found := registry.Lookup("/hello")
		assert.True(t, found)
		assert.Equal(t, "/hello", meta.Name)
	})

	t.Run("register_with_aliases", func(t *testing.T) {
		registry := NewCommandRegistry()

		def := &Definition{
			Name:    "/weather",
			Aliases: []string{"/w", "/天气"},
		}

		err := registry.Register(def)
		assert.NoError(t, err)

		// 通过主名称查找
		meta, found := registry.Lookup("/weather")
		assert.True(t, found)

		// 通过别名查找
		meta, found = registry.Lookup("/w")
		assert.True(t, found)
		assert.Equal(t, "/weather", meta.Name)

		meta, found = registry.Lookup("/天气")
		assert.True(t, found)
		assert.Equal(t, "/weather", meta.Name)
	})

	t.Run("register_duplicate_should_fail", func(t *testing.T) {
		registry := NewCommandRegistry()

		def := &Definition{Name: "/test"}

		err := registry.Register(def)
		assert.NoError(t, err)

		// 重复注册应该失败
		err = registry.Register(def)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("register_conflicting_alias_should_fail", func(t *testing.T) {
		registry := NewCommandRegistry()

		def1 := &Definition{
			Name:    "/cmd1",
			Aliases: []string{"/c"},
		}

		def2 := &Definition{
			Name:    "/cmd2",
			Aliases: []string{"/c"}, // 冲突的别名
		}

		err := registry.Register(def1)
		assert.NoError(t, err)

		err = registry.Register(def2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already used")
	})

	t.Run("register_with_priority", func(t *testing.T) {
		registry := NewCommandRegistry()

		def1 := &Definition{Name: "/cmd1"}
		def2 := &Definition{Name: "/cmd2"}

		err := registry.RegisterWithOptions(def1, RegisterOptions{Priority: 10})
		assert.NoError(t, err)

		err = registry.RegisterWithOptions(def2, RegisterOptions{Priority: 20})
		assert.NoError(t, err)

		list := registry.List()
		assert.Len(t, list, 2)

		// 应该按优先级排序
		assert.Equal(t, "/cmd2", list[0].Name) // 优先级 20
		assert.Equal(t, "/cmd1", list[1].Name) // 优先级 10
	})

	t.Run("register_with_pattern", func(t *testing.T) {
		registry := NewCommandRegistry()

		def := &Definition{Name: "/regex"}

		err := registry.RegisterWithOptions(def, RegisterOptions{
			Pattern: `^/r\w+`,
		})
		assert.NoError(t, err)

		matches := registry.LookupByPattern("/regex")
		assert.Len(t, matches, 1)

		matches = registry.LookupByPattern("/random")
		assert.Len(t, matches, 1)

		matches = registry.LookupByPattern("/test")
		assert.Len(t, matches, 0)
	})

	t.Run("register_empty_name_should_fail", func(t *testing.T) {
		registry := NewCommandRegistry()

		def := &Definition{Name: ""}

		err := registry.Register(def)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})
}

func TestCommandRegistry_Lookup(t *testing.T) {
	registry := NewCommandRegistry()

	def := &Definition{
		Name:    "/weather",
		Aliases: []string{"/w", "/天气"},
	}

	err := registry.Register(def)
	require.NoError(t, err)

	t.Run("lookup_by_name", func(t *testing.T) {
		meta, found := registry.Lookup("/weather")
		assert.True(t, found)
		assert.Equal(t, "/weather", meta.Name)
	})

	t.Run("lookup_by_alias", func(t *testing.T) {
		meta, found := registry.Lookup("/w")
		assert.True(t, found)
		assert.Equal(t, "/weather", meta.Name)
	})

	t.Run("lookup_nonexistent", func(t *testing.T) {
		_, found := registry.Lookup("/nonexistent")
		assert.False(t, found)
	})
}

func TestCommandRegistry_Stats(t *testing.T) {
	registry := NewCommandRegistry()

	def := &Definition{
		Name:    "/test",
		Aliases: []string{"/t"},
	}

	err := registry.Register(def)
	require.NoError(t, err)

	// 执行一些查找
	registry.Lookup("/test") // hit
	registry.Lookup("/t")    // hit (alias)
	registry.Lookup("/miss") // miss

	stats := registry.GetStats()
	assert.Equal(t, 1, stats.CommandCount)
	assert.Equal(t, 1, stats.AliasCount)
	assert.Equal(t, int64(3), stats.LookupCount)
	assert.Equal(t, int64(2), stats.HitCount)
	assert.Equal(t, int64(1), stats.MissCount)
	assert.Equal(t, int64(1), stats.AliasHits)
	assert.InDelta(t, 0.666, stats.HitRate, 0.01)
}

func TestExtractCommandFast(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple_command", "/hello world", "/hello"},
		{"command_only", "/test", "/test"},
		{"command_with_numbers", "/cmd123 arg", "/cmd123"},
		{"command_with_underscore", "/my_command", "/my_command"},
		{"no_command", "hello world", ""},
		{"empty_string", "", ""},
		{"whitespace_prefix", "  /test", "/test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractCommandFast(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractCommandAndArgs(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedCommand string
		expectedArgs    string
	}{
		{"command_with_args", "/weather Beijing", "/weather", "Beijing"},
		{"command_only", "/help", "/help", ""},
		{"command_with_multiple_args", "/search foo bar baz", "/search", "foo bar baz"},
		{"no_command", "hello world", "", "hello world"},
		{"whitespace_handling", "/cmd   arg1   arg2", "/cmd", "arg1   arg2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := ExtractCommandAndArgs(tt.input)
			assert.Equal(t, tt.expectedCommand, cmd)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

func TestValidateCommandName(t *testing.T) {
	tests := []struct {
		name      string
		cmdName   string
		shouldErr bool
	}{
		{"valid_simple", "/hello", false},
		{"valid_with_numbers", "/cmd123", false},
		{"valid_with_underscore", "/my_command", false},
		{"valid_with_dash", "/my-command", false},
		{"invalid_no_slash", "command", true},
		{"invalid_empty", "", true},
		{"invalid_only_slash", "/", true},
		{"invalid_with_space", "/hello world", true},
		{"invalid_special_char", "/hello@world", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommandName(tt.cmdName)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Benchmark tests

func BenchmarkCommandRegistry_Lookup(b *testing.B) {
	registry := NewCommandRegistry()

	// 注册 100 个命令
	for i := range 100 {
		def := &Definition{
			Name: "/cmd" + string(rune('0'+i%10)),
		}
		registry.Register(def)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		registry.Lookup("/cmd5")
	}
}

func BenchmarkExtractCommandFast(b *testing.B) {
	input := "/weather Beijing --unit celsius"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExtractCommandFast(input)
	}
}
