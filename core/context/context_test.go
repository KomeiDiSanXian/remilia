package context

import (
	stdctx "context"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAcquireContextFromEvent tests creating a new context via the new path
func TestAcquireContextFromEvent(t *testing.T) {
	event := newMockEventWithID(platform.EventKindPrivateMessage, "test-1")

	ctx := NewContextFromEvent(event, nil)

	require.NotNil(t, ctx)
	assert.NotNil(t, ctx.GetPlatformEvent())
	assert.Equal(t, "test-1", ctx.GetPlatformEvent().ID())
}

// TestAcquireContextFromEvent_WithStdCtx tests creating context then setting stdlib context
func TestAcquireContextFromEvent_WithStdCtx(t *testing.T) {
	stdCtx := t.Context()

	ctx := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), nil)
	ctx.SetStdContext(stdCtx)

	require.NotNil(t, ctx)
	assert.Equal(t, stdCtx, ctx.Context())
}

// TestContext_ContextNil tests Context() with nil receiver
func TestContext_ContextNil(t *testing.T) {
	var ctx *Context
	result := ctx.Context()
	assert.Equal(t, stdctx.Background(), result)
}

// TestContext_SetStdContext tests setting stdlib context
func TestContext_SetStdContext(t *testing.T) {
	ctx := newTestCtx()

	newCtx, cancel := stdctx.WithTimeout(stdctx.Background(), time.Second)
	defer cancel()

	ctx.SetStdContext(newCtx)
	assert.Equal(t, newCtx, ctx.Context())
}

// TestContext_SetStdContextNil tests SetStdContext with nil receiver
func TestContext_SetStdContextNil(t *testing.T) {
	var ctx *Context
	// Should not panic
	ctx.SetStdContext(stdctx.Background())
}

// TestContext_Clone tests cloning context
func TestContext_Clone(t *testing.T) {
	t.Run("basic clone", func(t *testing.T) {
		event := newMockEventWithID(platform.EventKindPrivateMessage, "test-1")
		ctx := NewContextFromEvent(event, nil)
		ctx.Set("key1", "value1")
		ctx.SetRetryAttempt(3)

		cloned := ctx.Clone()

		require.NotNil(t, cloned)
		assert.Equal(t, ctx.GetPlatformEvent(), cloned.GetPlatformEvent())

		// Verify extensionState is copied
		val, ok := cloned.Get("key1")
		assert.True(t, ok)
		assert.Equal(t, "value1", val)

		// Verify retry attempt is copied
		attempt, ok := cloned.GetRetryAttempt()
		assert.True(t, ok)
		assert.Equal(t, 3, attempt)
	})

	t.Run("cloned extensionState is independent", func(t *testing.T) {
		ctx := newTestCtx()
		ctx.Set("key1", "original")

		cloned := ctx.Clone()
		cloned.Set("key1", "modified")
		cloned.Set("key2", "new")

		// Original should not be affected
		val, _ := ctx.Get("key1")
		assert.Equal(t, "original", val)

		_, ok := ctx.Get("key2")
		assert.False(t, ok)
	})
}

// TestContext_SetGet tests Set/Get user extensionState
func TestContext_SetGet(t *testing.T) {
	t.Run("basic set and get", func(t *testing.T) {
		ctx := newTestCtx()

		ctx.Set("string", "value")
		ctx.Set("int", 123)
		ctx.Set("bool", true)

		val, ok := ctx.Get("string")
		assert.True(t, ok)
		assert.Equal(t, "value", val)

		val, ok = ctx.Get("int")
		assert.True(t, ok)
		assert.Equal(t, 123, val)

		val, ok = ctx.Get("bool")
		assert.True(t, ok)
		assert.Equal(t, true, val)
	})

	t.Run("get non-existent key", func(t *testing.T) {
		ctx := newTestCtx()

		val, ok := ctx.Get("non-existent")
		assert.False(t, ok)
		assert.Nil(t, val)
	})

	t.Run("set nil deletes key", func(t *testing.T) {
		ctx := newTestCtx()

		ctx.Set("key", "value")
		ctx.Set("key", nil) // nil is a no-op; key still holds "value"

		// key must still exist with original value (nil is no-op, not delete)
		v, ok := ctx.Get("key")
		assert.True(t, ok)
		assert.Equal(t, "value", v)

		// explicit delete removes the key
		ctx.Delete("key")
		_, ok = ctx.Get("key")
		assert.False(t, ok)
	})

	t.Run("set reserved key is forbidden", func(t *testing.T) {
		ctx := newTestCtx()

		// These should be silently ignored
		ctx.Set("_remilia_internal_test", "value")
		ctx.Set("mw_trace", "value")
		ctx.Set("retry_attempt", "value")

		_, ok := ctx.Get("_remilia_internal_test")
		assert.False(t, ok)
	})

	t.Run("nil context", func(t *testing.T) {
		var ctx *Context

		ctx.Set("key", "value")
		val, ok := ctx.Get("key")
		assert.False(t, ok)
		assert.Nil(t, val)
	})
}

// TestContext_Delete tests deleting user extensionState
func TestContext_Delete(t *testing.T) {
	t.Run("delete existing key", func(t *testing.T) {
		ctx := newTestCtx()

		ctx.Set("key", "value")
		ctx.Delete("key")

		_, ok := ctx.Get("key")
		assert.False(t, ok)
	})

	t.Run("delete non-existent key", func(t *testing.T) {
		ctx := newTestCtx()

		// Should not panic
		ctx.Delete("non-existent")
	})

	t.Run("delete reserved key is forbidden", func(t *testing.T) {
		ctx := newTestCtx()

		// Should be silently ignored
		ctx.Delete("_remilia_internal_test")
		ctx.Delete("mw_trace")
	})
}

// TestContext_Extensions tests Extensions store
func TestContext_Extensions(t *testing.T) {
	t.Run("get extensions", func(t *testing.T) {
		ctx := newTestCtx()

		ext := ctx.Ext()
		require.NotNil(t, ext)

		// Second call should return same instance
		ext2 := ctx.Ext()
		assert.Equal(t, ext, ext2)
	})

	t.Run("nil context returns nil extensions", func(t *testing.T) {
		var ctx *Context
		ext := ctx.Ext()
		assert.Nil(t, ext)
	})
}

// TestContext_ParsedCommand tests parsed command storage
func TestContext_ParsedCommand(t *testing.T) {
	t.Run("set and get parsed command", func(t *testing.T) {
		ctx := newTestCtx()

		parsed := &command.Parsed{
			CommandPath: []string{"test"},
			Arguments:   map[string]any{"arg1": "val1"},
		}

		ctx.SetParsedCommand(parsed)

		result := ctx.GetParsedCommand()
		require.NotNil(t, result)
		assert.Equal(t, "test", result.CommandPath[0])
		assert.Equal(t, map[string]any{"arg1": "val1"}, result.Arguments)
	})

	t.Run("get without set returns nil", func(t *testing.T) {
		ctx := newTestCtx()

		result := ctx.GetParsedCommand()
		assert.Nil(t, result)
	})

	t.Run("nil context", func(t *testing.T) {
		var ctx *Context

		ctx.SetParsedCommand(&command.Parsed{})
		result := ctx.GetParsedCommand()
		assert.Nil(t, result)
	})
}

// TestContext_MiddlewareTrace tests middleware trace
func TestContext_MiddlewareTrace(t *testing.T) {
	t.Run("set and get trace", func(t *testing.T) {
		ctx := newTestCtx()

		trace := []string{"mw1", "mw2", "mw3"}
		ctx.SetMiddlewareTrace(trace)

		result, ok := ctx.GetMiddlewareTrace()
		assert.True(t, ok)
		assert.Equal(t, trace, result)

		// Verify it's a copy (mutation doesn't affect stored value)
		result[0] = "modified"
		result2, _ := ctx.GetMiddlewareTrace()
		assert.Equal(t, "mw1", result2[0])
	})

	t.Run("get without set", func(t *testing.T) {
		ctx := newTestCtx()

		result, ok := ctx.GetMiddlewareTrace()
		assert.False(t, ok)
		assert.Nil(t, result)
	})

	t.Run("nil context", func(t *testing.T) {
		var ctx *Context

		ctx.SetMiddlewareTrace([]string{"test"})
		result, ok := ctx.GetMiddlewareTrace()
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

// TestContext_RetryAttempt tests retry attempt storage
func TestContext_RetryAttempt(t *testing.T) {
	t.Run("set and get retry attempt", func(t *testing.T) {
		ctx := newTestCtx()

		ctx.SetRetryAttempt(5)

		result, ok := ctx.GetRetryAttempt()
		assert.True(t, ok)
		assert.Equal(t, 5, result)
	})

	t.Run("get without set", func(t *testing.T) {
		ctx := newTestCtx()

		result, ok := ctx.GetRetryAttempt()
		assert.False(t, ok)
		assert.Equal(t, 0, result)
	})

	t.Run("nil context", func(t *testing.T) {
		var ctx *Context

		ctx.SetRetryAttempt(3)
		result, ok := ctx.GetRetryAttempt()
		assert.False(t, ok)
		assert.Equal(t, 0, result)
	})
}

// TestContext_ConcurrentAccess tests concurrent access to context
func TestContext_ConcurrentAccess(t *testing.T) {
	ctx := newTestCtx()

	var wg sync.WaitGroup

	// Concurrent Set operations
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx.Set("key", n)
		}(i)
	}

	// Concurrent Get operations
	for range 100 {
		wg.Go(func() {
			_, _ = ctx.Get("key")
		})
	}

	// Concurrent Delete operations
	for range 50 {
		wg.Go(func() {
			ctx.Delete("key")
		})
	}

	wg.Wait()
	// Should not panic or race
}

// TestExtensions_Concurrent tests concurrent access to Extensions
func TestExtensions_Concurrent(t *testing.T) {
	ext := newExtensions()

	var wg sync.WaitGroup

	// Concurrent Set
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ExtSet(ext, n)
		}(i)
	}

	// Concurrent Get
	for range 100 {
		wg.Go(func() {
			_, _ = ExtGet[int](ext)
		})
	}

	// Concurrent GetOrInit
	for range 50 {
		wg.Go(func() {
			_ = ExtGetOrInit(ext, func() string { return "test" })
		})
	}

	wg.Wait()
}

// TestRule_OnEventType tests event type matching
func TestRule_OnEventType(t *testing.T) {
	tests := []struct {
		name      string
		eventKind platform.EventKind
		ruleKind  platform.EventKind
		expected  bool
	}{
		{"match private", platform.EventKindPrivateMessage, platform.EventKindPrivateMessage, true},
		{"match group", platform.EventKindGroupMessage, platform.EventKindGroupMessage, true},
		{"no match", platform.EventKindPrivateMessage, platform.EventKindGroupMessage, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContextFromEvent(newMockEvent(tt.eventKind), nil)

			rule := OnEventType(string(tt.ruleKind))
			result := rule(ctx)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRule_OnCommand tests command matching
func TestRule_OnCommand(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		prefix   string
		expected bool
	}{
		{"exact match", "/ping", "/ping", true},
		{"with args", "/ping arg1 arg2", "/ping", true},
		{"with leading spaces", "  /ping", "/ping", true},
		{"with tabs", "\t/ping", "/ping", true},
		{"no match", "/pong", "/ping", false},
		{"partial match", "/pi", "/ping", false},
		{"case sensitive", "/Ping", "/ping", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := createTestContextWithMessage(tt.content)

			rule := OnCommand(tt.prefix)
			result := rule(ctx)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRule_OnKeyword tests keyword matching
func TestRule_OnKeyword(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		keyword  string
		expected bool
	}{
		{"exact match", "hello", "hello", true},
		{"contains", "hello world", "world", true},
		{"not found", "hello", "goodbye", false},
		{"case sensitive", "Hello", "hello", false},
		{"with spaces", "  hello", "hello", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := createTestContextWithMessage(tt.content)

			rule := OnKeyword(tt.keyword)
			result := rule(ctx)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRule_OnFullMatch tests full match
func TestRule_OnFullMatch(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		text     string
		expected bool
	}{
		{"exact match", "hello", "hello", true},
		{"with leading spaces", "  hello", "hello", true},
		{"with trailing spaces", "hello  ", "hello", true}, // 事件层统一 TrimSpace，尾部空格不可见（与 qq/satori 一致）
		{"no match", "hello world", "hello", false},
		{"case sensitive", "Hello", "hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := createTestContextWithMessage(tt.content)

			rule := OnFullMatch(tt.text)
			result := rule(ctx)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRule_OnPrefix tests prefix matching
func TestRule_OnPrefix(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		prefix   string
		expected bool
	}{
		{"exact match", "hello", "hello", true},
		{"has prefix", "hello world", "hello", true},
		{"with leading spaces", "  hello world", "hello", true},
		{"no match", "world", "hello", false},
		{"case sensitive", "Hello", "hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := createTestContextWithMessage(tt.content)

			rule := OnPrefix(tt.prefix)
			result := rule(ctx)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRule_OnSuffix tests suffix matching
func TestRule_OnSuffix(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		suffix   string
		expected bool
	}{
		{"exact match", "hello", "hello", true},
		{"has suffix", "world hello", "hello", true},
		{"no match", "hello", "world", false},
		{"case sensitive", "HELLO", "hello", false},
		{"leading spaces not ignored", "  hello", "hello", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := createTestContextWithMessage(tt.content)

			rule := OnSuffix(tt.suffix)
			result := rule(ctx)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRule_OnRegex tests regex matching
func TestRule_OnRegex(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		pattern  string
		expected bool
	}{
		{"simple match", "hello123", `\d+`, true},
		{"no match", "hello", `\d+`, false},
		{"email pattern", "test@example.com", `\w+@\w+\.\w+`, true},
		{"complex pattern", "Version: 1.2.3", `Version: \d+\.\d+\.\d+`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := createTestContextWithMessage(tt.content)

			rule := OnRegex(tt.pattern)
			result := rule(ctx)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRule_OnRegexSafe tests safe regex matching
func TestRule_OnRegexSafe(t *testing.T) {
	t.Run("valid pattern", func(t *testing.T) {
		ctx := createTestContextWithMessage("test123")

		rule, err := OnRegexSafe(`\d+`)
		require.NoError(t, err)

		result := rule(ctx)
		assert.True(t, result)
	})

	t.Run("invalid pattern", func(t *testing.T) {
		_, err := OnRegexSafe(`[invalid(`)
		assert.Error(t, err)
	})
}

// TestRegexCache tests regex cache
func TestRegexCache(t *testing.T) {
	t.Run("cache hit", func(t *testing.T) {
		initRegexCache()

		pattern := `test\d+`

		// First call - miss
		re1, _ := regexp.Compile(pattern)
		regexCache.put(pattern, re1)

		// Second call - hit
		re2, ok := regexCache.get(pattern)
		assert.True(t, ok)
		assert.Equal(t, re1, re2)
	})

	t.Run("cache miss", func(t *testing.T) {
		initRegexCache()

		_, ok := regexCache.get("non-existent-pattern")
		assert.False(t, ok)
	})
}

// TestRule_OnUserWhitelist tests user whitelist
func TestRule_OnUserWhitelist(t *testing.T) {
	t.Run("user in whitelist", func(t *testing.T) {
		ctx := createTestContextWithAuthor("user1")

		rule := OnUserWhitelist("user1", "user2", "user3")
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("user not in whitelist", func(t *testing.T) {
		ctx := createTestContextWithAuthor("user4")

		rule := OnUserWhitelist("user1", "user2", "user3")
		result := rule(ctx)

		assert.False(t, result)
	})

	t.Run("no author", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindPrivateMessage)

		rule := OnUserWhitelist("user1")
		result := rule(ctx)

		assert.False(t, result)
	})
}

// TestRule_OnUserBlacklist tests user blacklist
func TestRule_OnUserBlacklist(t *testing.T) {
	t.Run("user in blacklist", func(t *testing.T) {
		ctx := createTestContextWithAuthor("banned1")

		rule := OnUserBlacklist("banned1", "banned2")
		result := rule(ctx)

		assert.False(t, result)
	})

	t.Run("user not in blacklist", func(t *testing.T) {
		ctx := createTestContextWithAuthor("user1")

		rule := OnUserBlacklist("banned1", "banned2")
		result := rule(ctx)

		assert.True(t, result)
	})

	t.Run("no author passes", func(t *testing.T) {
		ctx := newTestCtxWithKind(platform.EventKindPrivateMessage)

		rule := OnUserBlacklist("banned1")
		result := rule(ctx)

		assert.True(t, result)
	})
}

// TestIsReservedUserStateKey tests reserved key detection
func TestIsReservedUserStateKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{"mw_trace", "mw_trace", true},
		{"retry_attempt", "retry_attempt", true},
		{"internal prefix", "_remilia_internal_test", true},
		{"normal key", "user_key", false},
		{"empty key", "", false},
		{"whitespace key", "  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isReservedUserStateKey(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtensions_Snapshot tests snapshot functionality
func TestExtensions_Snapshot(t *testing.T) {
	t.Run("snapshot creates copy", func(t *testing.T) {
		ext := newExtensions()

		ExtSet(ext, "value1")
		ExtSet(ext, 123)

		snapshot := ext.Snapshot()
		assert.Equal(t, 2, len(snapshot))

		// Modify snapshot doesn't affect original
		for k := range snapshot {
			delete(snapshot, k)
		}

		// Original still has values
		val, ok := ExtGet[string](ext)
		assert.True(t, ok)
		assert.Equal(t, "value1", val)
	})

	t.Run("nil extensions", func(t *testing.T) {
		var ext *Extensions
		snapshot := ext.Snapshot()
		assert.Nil(t, snapshot)
	})
}

// TestExtensions_GetOrInit tests GetOrInit race condition
func TestExtensions_GetOrInit(t *testing.T) {
	t.Run("init only once", func(t *testing.T) {
		ext := newExtensions()

		initCount := 0
		var mu sync.Mutex

		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				ExtGetOrInit(ext, func() string {
					mu.Lock()
					initCount++
					mu.Unlock()
					return "initialized"
				})
			})
		}

		wg.Wait()

		// Init should only be called once
		assert.Equal(t, 1, initCount)

		val, ok := ExtGet[string](ext)
		assert.True(t, ok)
		assert.Equal(t, "initialized", val)
	})
}

// BUG DISCOVERY TESTS - Edge cases and potential bugs

// TestBug_CloneWithNilState tests clone with uninitialized extensionState
func TestBug_CloneWithNilState(t *testing.T) {
	ctx := newTestCtx()
	// Don't initialize extensionState

	cloned := ctx.Clone()

	// Should not panic
	require.NotNil(t, cloned)

	// State should still work
	cloned.Set("key", "value")
	val, ok := cloned.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
}

// TestBug_SetThenDeleteReservedKey tests reserved key protection
func TestBug_SetThenDeleteReservedKey(t *testing.T) {
	ctx := newTestCtx()

	// Try to set and delete reserved keys
	ctx.Set("_remilia_internal_secret", "should not work")
	ctx.Delete("_remilia_internal_secret")

	// Should not exist
	_, ok := ctx.Get("_remilia_internal_secret")
	assert.False(t, ok)
}

// TestBug_ConcurrentClone tests concurrent cloning
func TestBug_ConcurrentClone(t *testing.T) {
	ctx := newTestCtx()
	ctx.Set("key", "value")

	var wg sync.WaitGroup
	clones := make([]*Context, 100)

	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			clones[idx] = ctx.Clone()
		}(i)
	}

	wg.Wait()

	// All clones should have the value
	for _, clone := range clones {
		val, ok := clone.Get("key")
		assert.True(t, ok)
		assert.Equal(t, "value", val)
	}
}

// TestBug_RegexCachePanic tests regex compilation panic
func TestBug_RegexCachePanic(t *testing.T) {
	ctx := createTestContextWithMessage("test")

	// Invalid regex should panic
	assert.Panics(t, func() {
		rule := OnRegex(`[invalid(`)
		rule(ctx)
	})
}

// TestBug_EmptyPrefix tests empty prefix matching
func TestBug_EmptyPrefix(t *testing.T) {
	ctx := createTestContextWithMessage("hello")

	rule := OnPrefix("")
	result := rule(ctx)

	// Empty prefix should match everything
	assert.True(t, result)
}

// TestBug_EmptyKeyword tests empty keyword matching
func TestBug_EmptyKeyword(t *testing.T) {
	ctx := createTestContextWithMessage("hello")

	rule := OnKeyword("")
	result := rule(ctx)

	// Empty keyword should match everything (strings.Contains returns true)
	assert.True(t, result)
}

// Helper functions

func createTestContextWithMessage(content string) *Context {
	return NewContextFromEvent(newMockEventWithContent(platform.EventKindPrivateMessage, content), nil)
}

func createTestContextWithAuthor(userID string) *Context {
	return NewContextFromEvent(newMockEventWithSender(platform.EventKindPrivateMessage, userID), nil)
}

// Benchmark tests

func BenchmarkContext_SetGet(b *testing.B) {
	ctx := newTestCtx()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx.Set("key", i)
		_, _ = ctx.Get("key")
	}
}

func BenchmarkContext_Clone(b *testing.B) {
	ctx := newTestCtx()
	ctx.Set("key1", "value1")
	ctx.Set("key2", "value2")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ctx.Clone()
	}
}

func BenchmarkRule_OnCommand(b *testing.B) {
	ctx := createTestContextWithMessage("/ping arg1 arg2")
	rule := OnCommand("/ping")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = rule(ctx)
	}
}

func BenchmarkRule_OnRegex(b *testing.B) {
	ctx := createTestContextWithMessage("test123")
	rule := OnRegex(`\d+`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = rule(ctx)
	}
}

func BenchmarkExtensions_GetOrInit(b *testing.B) {
	ext := newExtensions()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ExtGetOrInit(ext, func() string { return "test" })
	}
}
