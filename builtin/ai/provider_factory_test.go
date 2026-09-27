// provider_factory_test.go — newProvider 的装配侧用例。
//
// 这些用例原先放在外部测试包 ai_test（通过 ai.NewProvider 调用），
// newProvider 收口为包内装配细节后随之迁入，改为直接断言真实入口。
package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
)

func TestNewProviderOpenAI(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.Provider = "openai"
	prov, err := newProvider(&cfg)
	if err != nil {
		t.Fatalf("newProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("newProvider returned nil")
	}
}

func TestNewProviderAnthropic(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.Provider = "anthropic"
	prov, err := newProvider(&cfg)
	if err != nil {
		t.Fatalf("newProvider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("newProvider returned nil")
	}
}

func TestNewProviderDefault(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.Provider = ""
	prov, err := newProvider(&cfg)
	if err != nil {
		t.Fatalf("newProvider with empty provider failed: %v", err)
	}
	if prov == nil {
		t.Fatal("newProvider returned nil")
	}
}

func TestNewProviderUnknown(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.Provider = "unknown_provider"
	_, err := newProvider(&cfg)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}
