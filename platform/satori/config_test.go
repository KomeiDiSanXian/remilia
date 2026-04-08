package satori

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "chronocat", "123456")
	if cfg.ServerURL != "http://localhost:5140" {
		t.Errorf("ServerURL: got %q", cfg.ServerURL)
	}
	if cfg.Platform != "chronocat" {
		t.Errorf("Platform: got %q", cfg.Platform)
	}
	if cfg.UserID != "123456" {
		t.Errorf("UserID: got %q", cfg.UserID)
	}
	if cfg.Version != "v1" {
		t.Errorf("Version: got %q, want v1", cfg.Version)
	}
	if cfg.ReconnectDelay != 2*time.Second {
		t.Errorf("ReconnectDelay: got %v", cfg.ReconnectDelay)
	}
	if cfg.MaxReconnectDelay != 60*time.Second {
		t.Errorf("MaxReconnectDelay: got %v", cfg.MaxReconnectDelay)
	}
	if cfg.MaxReconnects != 0 {
		t.Errorf("MaxReconnects: got %d, want 0 (unlimited)", cfg.MaxReconnects)
	}
	if cfg.EventBufferSize != 256 {
		t.Errorf("EventBufferSize: got %d, want 256", cfg.EventBufferSize)
	}
	if cfg.PingInterval != 10*time.Second {
		t.Errorf("PingInterval: got %v", cfg.PingInterval)
	}
	if cfg.HTTPTimeout != 15*time.Second {
		t.Errorf("HTTPTimeout: got %v", cfg.HTTPTimeout)
	}
}

func TestConfig_Validate_OK(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "chronocat", "123456")
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate on valid config: unexpected error: %v", err)
	}
}

func TestConfig_Validate_MissingServerURL(t *testing.T) {
	cfg := DefaultConfig("", "chronocat", "123456")
	if err := cfg.Validate(); err == nil {
		t.Error("Validate: expected error for missing ServerURL, got nil")
	}
}

func TestConfig_Validate_MissingPlatform(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "", "123456")
	if err := cfg.Validate(); err == nil {
		t.Error("Validate: expected error for missing Platform, got nil")
	}
}

func TestConfig_Validate_MissingUserID(t *testing.T) {
	cfg := DefaultConfig("http://localhost:5140", "chronocat", "")
	if err := cfg.Validate(); err == nil {
		t.Error("Validate: expected error for missing UserID, got nil")
	}
}

func TestConfig_Validate_FillsDefaults(t *testing.T) {
	cfg := Config{
		ServerURL: "http://localhost:5140",
		Platform:  "test",
		UserID:    "999",
		// Leave all optional fields zero
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if cfg.Version != "v1" {
		t.Errorf("Validate should set Version=v1, got %q", cfg.Version)
	}
	if cfg.ReconnectDelay != 2*time.Second {
		t.Errorf("Validate should set ReconnectDelay=2s, got %v", cfg.ReconnectDelay)
	}
	if cfg.MaxReconnectDelay != 60*time.Second {
		t.Errorf("Validate should set MaxReconnectDelay=60s, got %v", cfg.MaxReconnectDelay)
	}
	if cfg.EventBufferSize != 256 {
		t.Errorf("Validate should set EventBufferSize=256, got %d", cfg.EventBufferSize)
	}
	if cfg.PingInterval != 10*time.Second {
		t.Errorf("Validate should set PingInterval=10s, got %v", cfg.PingInterval)
	}
	if cfg.HTTPTimeout != 15*time.Second {
		t.Errorf("Validate should set HTTPTimeout=15s, got %v", cfg.HTTPTimeout)
	}
}

func TestConfig_Validate_PreservesNonZeroValues(t *testing.T) {
	cfg := Config{
		ServerURL:         "http://localhost:5140",
		Platform:          "test",
		UserID:            "999",
		Version:           "v2",
		ReconnectDelay:    5 * time.Second,
		MaxReconnectDelay: 120 * time.Second,
		EventBufferSize:   512,
		PingInterval:      30 * time.Second,
		HTTPTimeout:       30 * time.Second,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if cfg.Version != "v2" {
		t.Errorf("Validate should preserve Version=v2, got %q", cfg.Version)
	}
	if cfg.ReconnectDelay != 5*time.Second {
		t.Errorf("Validate should preserve ReconnectDelay=5s, got %v", cfg.ReconnectDelay)
	}
	if cfg.MaxReconnectDelay != 120*time.Second {
		t.Errorf("Validate should preserve MaxReconnectDelay=120s, got %v", cfg.MaxReconnectDelay)
	}
	if cfg.EventBufferSize != 512 {
		t.Errorf("Validate should preserve EventBufferSize=512, got %d", cfg.EventBufferSize)
	}
	if cfg.PingInterval != 30*time.Second {
		t.Errorf("Validate should preserve PingInterval=30s, got %v", cfg.PingInterval)
	}
	if cfg.HTTPTimeout != 30*time.Second {
		t.Errorf("Validate should preserve HTTPTimeout=30s, got %v", cfg.HTTPTimeout)
	}
}
