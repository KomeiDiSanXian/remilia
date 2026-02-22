package storage_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugins/core/storage"
)

func TestNewWithBackend_Alias(t *testing.T) {
	mem := storage.NewMemoryStorage()
	desc1 := storage.NewWithBackend(mem)
	desc2 := storage.NewV2WithBackend(mem)
	if desc1.Name != desc2.Name {
		t.Errorf("NewWithBackend should produce same descriptor as NewV2WithBackend")
	}
}
func TestPlugin_SetGetJSON(t *testing.T) {
	p := &storage.Plugin{}
	// Use memory backend indirectly via plugin
	mem := storage.NewMemoryStorage()
	p.SetStorage(mem)
	type User struct {
		Name string
		Age  int
	}
	u := User{Name: "Alice", Age: 30}
	if err := p.SetJSON("user:1", u, 0); err != nil {
		t.Fatalf("SetJSON: %v", err)
	}
	var got User
	if err := p.GetJSON("user:1", &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got.Name != "Alice" || got.Age != 30 {
		t.Errorf("unexpected value: %+v", got)
	}
}
func TestPlugin_TTL(t *testing.T) {
	p := &storage.Plugin{}
	p.SetStorage(storage.NewMemoryStorage())
	p.Set("ttl_key", []byte("val"), 30*time.Millisecond)
	if !p.Exists("ttl_key") {
		t.Error("key should exist before TTL expiry")
	}
	time.Sleep(50 * time.Millisecond)
	if p.Exists("ttl_key") {
		t.Error("key should not exist after TTL expiry")
	}
}
func TestPlugin_Delete(t *testing.T) {
	p := &storage.Plugin{}
	p.SetStorage(storage.NewMemoryStorage())
	p.Set("del_key", []byte("x"), 0)
	p.Delete("del_key")
	if p.Exists("del_key") {
		t.Error("key should not exist after delete")
	}
}
func TestPlugin_Keys(t *testing.T) {
	p := &storage.Plugin{}
	p.SetStorage(storage.NewMemoryStorage())
	p.Set("pfx:a", []byte("1"), 0)
	p.Set("pfx:b", []byte("2"), 0)
	p.Set("other", []byte("3"), 0)
	keys, err := p.Keys("pfx:*")
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys with pfx: prefix, got %d: %v", len(keys), keys)
	}
}
func TestSQLiteStorage_BasicOps(t *testing.T) {
	t.TempDir()
	s, err := storage.NewSQLiteStorage(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	defer s.Close()
	key := "test_key"
	val := []byte("hello sqlite")
	if err := s.Set(key, val, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(val) {
		t.Errorf("expected %q, got %q", val, got)
	}
	if !s.Exists(key) {
		t.Error("Exists should return true")
	}
	s.Delete(key)
	if s.Exists(key) {
		t.Error("Exists should return false after Delete")
	}
}
func TestSQLiteStorage_TTL(t *testing.T) {
	s, err := storage.NewSQLiteStorage(t.TempDir() + "/ttl.db")
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	defer s.Close()
	s.Set("expiring", []byte("v"), 30*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	_, err = s.Get("expiring")
	if err != storage.ErrExpired && err != storage.ErrNotFound {
		t.Errorf("expected ErrExpired or ErrNotFound after TTL, got %v", err)
	}
}
