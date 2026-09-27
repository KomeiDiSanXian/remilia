package ai_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
)

// providerOptionsForTest 复刻装配侧的"配置 → 协议参数"映射。
// ai.providerOptions 未导出，外部测试包只能自己构造等价参数。
func providerOptionsForTest(cfg *config.Config) protocol.ProviderOptions {
	return protocol.ProviderOptions{
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		MaxTokens:    cfg.MaxTokens,
		APITimeout:   cfg.APITimeout,
		MaxRetries:   cfg.MaxRetries,
		IncludeUsage: cfg.IncludeUsage,
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig
	if cfg.Provider != "openai" {
		t.Errorf("expected provider %q, got %q", "openai", cfg.Provider)
	}
	if cfg.Model != "gpt-4o-mini" {
		t.Errorf("expected model %q, got %q", "gpt-4o-mini", cfg.Model)
	}
	if cfg.MaxDepth != 5 {
		t.Errorf("expected MaxDepth 5, got %d", cfg.MaxDepth)
	}
	if cfg.MaxHistory != 20 {
		t.Errorf("expected MaxHistory 20, got %d", cfg.MaxHistory)
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("expected Temperature 0.7, got %f", cfg.Temperature)
	}
	if cfg.APITimeout != 60*time.Second {
		t.Errorf("expected APITimeout 60s, got %v", cfg.APITimeout)
	}
	if cfg.TriggerCmd != "/ai" {
		t.Errorf("expected TriggerCmd %q, got %q", "/ai", cfg.TriggerCmd)
	}
}

func TestNewOpenAIProvider(t *testing.T) {
	cfg := config.DefaultConfig
	prov, err := protocol.NewOpenAIProvider(providerOptionsForTest(&cfg))
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewOpenAIProvider returned nil")
	}
}

func TestNewOpenAIProviderCustomBaseURL(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.BaseURL = "https://api.deepseek.com"
	prov, err := protocol.NewOpenAIProvider(providerOptionsForTest(&cfg))
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewOpenAIProvider returned nil")
	}
}

func TestNewAnthropicProvider(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.Provider = "anthropic"
	prov, err := protocol.NewAnthropicProvider(providerOptionsForTest(&cfg))
	if err != nil {
		t.Fatalf("NewAnthropicProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewAnthropicProvider returned nil")
	}
}

func TestNewPluginDescriptor(t *testing.T) {
	_ = ai.New
}

func TestPluginNew(t *testing.T) {
	d := ai.New(nil)
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "ai" {
		t.Errorf("expected name %q, got %q", "ai", d.Name)
	}
	if d.Version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", d.Version)
	}
	if d.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if d.Meta.Description != "AI 对话插件，支持多提供商和工具调用" {
		t.Errorf("unexpected description: %q", d.Meta.Description)
	}
}

func TestNewGormSessionStorePanicsWithNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil client")
		}
	}()
	session.NewGormSessionStore(nil)
}
