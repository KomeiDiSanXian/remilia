package ai_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

func TestDefaultConfig(t *testing.T) {
	cfg := ai.DefaultConfig
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
	cfg := ai.DefaultConfig
	prov, err := ai.NewOpenAIProvider(&cfg)
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewOpenAIProvider returned nil")
	}
}

func TestNewOpenAIProviderCustomBaseURL(t *testing.T) {
	cfg := ai.DefaultConfig
	cfg.BaseURL = "https://api.deepseek.com"
	prov, err := ai.NewOpenAIProvider(&cfg)
	if err != nil {
		t.Fatalf("NewOpenAIProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewOpenAIProvider returned nil")
	}
}

func TestNewAnthropicProvider(t *testing.T) {
	cfg := ai.DefaultConfig
	cfg.Provider = "anthropic"
	prov, err := ai.NewAnthropicProvider(&cfg)
	if err != nil {
		t.Fatalf("NewAnthropicProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewAnthropicProvider returned nil")
	}
}

func TestNewProviderOpenAI(t *testing.T) {
	cfg := ai.DefaultConfig
	cfg.Provider = "openai"
	prov, err := ai.NewProvider(&cfg)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewProvider returned nil")
	}
}

func TestNewProviderAnthropic(t *testing.T) {
	cfg := ai.DefaultConfig
	cfg.Provider = "anthropic"
	prov, err := ai.NewProvider(&cfg)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewProvider returned nil")
	}
}

func TestNewProviderDefault(t *testing.T) {
	cfg := ai.DefaultConfig
	cfg.Provider = ""
	prov, err := ai.NewProvider(&cfg)
	if err != nil {
		t.Fatalf("NewProvider with empty provider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewProvider returned nil")
	}
}

func TestNewProviderUnknown(t *testing.T) {
	cfg := ai.DefaultConfig
	cfg.Provider = "unknown_provider"
	_, err := ai.NewProvider(&cfg)
	if err == nil {
		t.Error("expected error for unknown provider")
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
	ai.NewGormSessionStore(nil)
}
