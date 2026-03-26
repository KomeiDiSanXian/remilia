package remilia_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBotBuilder tests the BotBuilder pattern
func TestBotBuilder(t *testing.T) {
	t.Run("MinimalBuild", func(t *testing.T) {
		adapter := qq.SimpleWebhookAdapter(8080)

		bot, err := remilia.NewBotBuilder().
			WithPlatformAdapter(adapter).
			Build()

		require.NoError(t, err)
		assert.NotNil(t, bot)
	})

	t.Run("WithQQAdapter", func(t *testing.T) {
		botInfo := &dto.BotInfo{
			QQNum:     54321,
			AppID:     12345,
			Token:     "test-token",
			AppSecret: "test-secret",
		}
		adapter := qq.NewWebhookServerAdapter(":8080", botInfo)

		bot, err := remilia.NewBotBuilder().
			WithPlatformAdapter(adapter).
			Build()

		require.NoError(t, err)
		assert.NotNil(t, bot)
	})

	t.Run("WithCustomEngine", func(t *testing.T) {
		customEngine := engine.NewEngine()
		adapter := qq.SimpleWebhookAdapter(8080)

		bot, err := remilia.NewBotBuilder().
			WithEngine(customEngine).
			WithPlatformAdapter(adapter).
			Build()

		require.NoError(t, err)
		assert.NotNil(t, bot)
		assert.Equal(t, customEngine, bot.Engine())
	})

	t.Run("WithName", func(t *testing.T) {
		adapter := qq.SimpleWebhookAdapter(8080)

		bot, err := remilia.NewBotBuilder().
			WithPlatformAdapter(adapter).
			WithName("test-bot").
			Build()

		require.NoError(t, err)
		assert.NotNil(t, bot)
	})

	t.Run("WithDebug", func(t *testing.T) {
		adapter := qq.SimpleWebhookAdapter(8080)

		bot, err := remilia.NewBotBuilder().
			WithPlatformAdapter(adapter).
			WithDebug(true).
			Build()

		require.NoError(t, err)
		assert.NotNil(t, bot)
	})

	t.Run("ChainedCalls", func(t *testing.T) {
		botInfo := &dto.BotInfo{
			QQNum:     54321,
			AppID:     12345,
			Token:     "test-token",
			AppSecret: "test-secret",
		}
		adapter := qq.NewWebhookServerAdapter(":8080", botInfo)

		bot, err := remilia.NewBotBuilder().
			WithPlatformAdapter(adapter).
			WithName("chained-bot").
			WithVersion("1.0.0").
			WithDebug(false).
			Build()

		require.NoError(t, err)
		assert.NotNil(t, bot)
	})
}

// TestBotBuilder_Errors tests error cases
func TestBotBuilder_Errors(t *testing.T) {
	t.Run("NoAdapter", func(t *testing.T) {
		bot, err := remilia.NewBotBuilder().Build()

		assert.Error(t, err)
		assert.Nil(t, bot)
		assert.Equal(t, errutil.ErrAdapterRequired, err)
	})
}

// TestBotBuilder_MustBuild tests the MustBuild method
func TestBotBuilder_MustBuild(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		adapter := qq.SimpleWebhookAdapter(8080)

		assert.NotPanics(t, func() {
			bot := remilia.NewBotBuilder().
				WithPlatformAdapter(adapter).
				MustBuild()
			assert.NotNil(t, bot)
		})
	})

	t.Run("Panic", func(t *testing.T) {
		assert.Panics(t, func() {
			remilia.NewBotBuilder().MustBuild() // No adapter
		})
	})
}

// TestSimpleWebhookAdapter tests the simplified webhook adapter
func TestSimpleWebhookAdapter(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		adapter := qq.SimpleWebhookAdapter(8080)
		assert.NotNil(t, adapter)
	})

	t.Run("WithBot", func(t *testing.T) {
		adapter := qq.SimpleWebhookAdapter(8080)
		newEngine := engine.NewEngine()

		bot := remilia.NewBot(adapter, newEngine)
		assert.NotNil(t, bot)
	})
}

// BenchmarkBotBuilder benchmarks bot creation
func BenchmarkBotBuilder(b *testing.B) {
	adapter := qq.SimpleWebhookAdapter(8080)

	b.Run("Builder", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = remilia.NewBotBuilder().
				WithPlatformAdapter(adapter).
				Build()
		}
	})

	b.Run("Direct", func(b *testing.B) {
		b.ReportAllocs()
		eng := engine.NewEngine()
		for i := 0; i < b.N; i++ {
			_ = remilia.NewBot(adapter, eng)
		}
	})
}
