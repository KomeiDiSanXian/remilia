package remilia

import (
	"net/http"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/openapi/protocol/webhook"
	"github.com/stretchr/testify/assert"
)

// MockWebHook is a mock implementation of webhook.WebHook for testing
type MockWebHook struct{}

func (m *MockWebHook) Verify(header http.Header, body []byte) (bool, error) {
	return true, nil
}

func (m *MockWebHook) Sign(header http.Header, body []byte) ([]byte, error) {
	return body, nil
}

func (m *MockWebHook) Handle(w http.ResponseWriter, r *http.Request) {}

func (m *MockWebHook) Addr() string {
	return ":8080"
}

func (m *MockWebHook) EventStream() <-chan *dto.Payload {
	ch := make(chan *dto.Payload)
	return ch
}

var _ webhook.WebHook = (*MockWebHook)(nil) // Ensure MockWebHook implements webhook.WebHook

func TestNew(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	bot := New(info)

	assert.NotNil(t, bot)
	assert.NotNil(t, bot.tm)
	assert.NotNil(t, bot.engine)
	assert.NotNil(t, bot.api)
	assert.Nil(t, bot.adapter)
}

func TestNewWithWebHook(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	mockWebhook := &MockWebHook{}

	bot := New(info, WithWebHook(mockWebhook))

	assert.NotNil(t, bot)
	assert.NotNil(t, bot.adapter)
}

func TestNewWithEngine(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	customEngine := NewEngine()

	bot := New(info, WithEngine(customEngine))

	assert.NotNil(t, bot)
	assert.Equal(t, customEngine, bot.engine)
	// Check that the custom engine is being used (not the global one)
	assert.Same(t, customEngine, bot.engine)
}

func TestNewWithMultipleOptions(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	customEngine := NewEngine()
	mockWebhook := &MockWebHook{}

	bot := New(info,
		WithEngine(customEngine),
		WithWebHook(mockWebhook),
	)

	assert.NotNil(t, bot)
	assert.Equal(t, customEngine, bot.engine)
	assert.NotNil(t, bot.adapter)
}

func TestBotGetEngine(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	customEngine := NewEngine()
	bot := New(info, WithEngine(customEngine))

	engine := bot.GetEngine()
	assert.Equal(t, customEngine, engine)
}

func TestBotGetAPI(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	bot := New(info)

	api := bot.GetAPI()
	assert.NotNil(t, api)
}

func TestBotOptionFunctions(t *testing.T) {
	customEngine := NewEngine()
	mockWebhook := &MockWebHook{}

	// Test WithWebHook option
	whOption := WithWebHook(mockWebhook)
	assert.NotNil(t, whOption)

	// Test WithEngine option
	engineOption := WithEngine(customEngine)
	assert.NotNil(t, engineOption)
}

func TestBotOptionsAreApplied(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	customEngine := NewEngine()
	customEngine.SetBlock(true)

	bot := New(info, WithEngine(customEngine))

	// 验证 block 状态（通过 GetMatcherStats）
	stats := bot.engine.GetMatcherStats()
	assert.False(t, stats.GlobalEnabled) // block=true 时 GlobalEnabled=false
}

func TestMultipleBots(t *testing.T) {
	info1 := &dto.BotInfo{
		AppID:     12345,
		Token:     "token-1",
		AppSecret: "secret-1",
	}

	info2 := &dto.BotInfo{
		AppID:     67890,
		Token:     "token-2",
		AppSecret: "secret-2",
	}

	engine1 := NewEngine()
	engine2 := NewEngine()

	bot1 := New(info1, WithEngine(engine1))
	bot2 := New(info2, WithEngine(engine2))

	assert.NotSame(t, bot1, bot2)
	assert.NotSame(t, bot1.engine, bot2.engine)
	assert.NotSame(t, bot1.api, bot2.api)
}

func TestNew_DefaultEngineCreation(t *testing.T) {
	info := &dto.BotInfo{
		AppID:     12345,
		Token:     "test-token",
		AppSecret: "test-secret",
	}

	// New() should auto-create Engine
	bot := New(info)

	assert.NotNil(t, bot)
	assert.NotNil(t, bot.engine, "Engine should be auto-created")
}

func TestBotDelegation(t *testing.T) {
	info := &dto.BotInfo{AppID: 12345}
	bot := New(info)

	// Test On
	matcher := bot.On(dto.C2CMessageCreate)
	assert.NotNil(t, matcher)
	assert.Equal(t, dto.C2CMessageCreate, matcher.EventType)

	// Test OnC2C
	c2cMatcher := bot.OnC2C()
	assert.NotNil(t, c2cMatcher)
	assert.Equal(t, dto.C2CMessageCreate, c2cMatcher.EventType)

	// Test OnCommand
	cmdMatcher := bot.OnCommand(dto.C2CMessageCreate, "/ping")
	assert.NotNil(t, cmdMatcher)
	assert.Equal(t, "/ping", cmdMatcher.GetCommand())

	// Test Use (Chaining)
	assert.Equal(t, bot, bot.Use(func(next HandlerE) HandlerE { return next }))
}
