package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/core/storage"
)

var bg = context.Background()

// ─── NewWithBackend alias ───────────────────────────────────────────────────

func TestNewWithBackend_Alias(t *testing.T) {
	mem := storage.NewMemoryStorage()
	desc1 := storage.NewWithBackend(mem)
	desc2 := storage.NewV2WithBackend(mem)
	if desc1.Name != desc2.Name {
		t.Errorf("NewWithBackend should produce same descriptor as NewV2WithBackend")
	}
}

// ─── Plugin.NS + generic Get/Set ───────────────────────────────────────────

func TestPlugin_NS_GetSet(t *testing.T) {
	p := storage.NewPlugin(storage.NewMemoryStorage())
	store := p.NS("test")

	type User struct {
		Name string
		Age  int
	}
	u := User{Name: "Alice", Age: 30}
	if err := storage.Set(bg, store, "user:1", u, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := storage.Get[User](bg, store, "user:1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Alice" || got.Age != 30 {
		t.Errorf("unexpected value: %+v", got)
	}
}

func TestPlugin_NS_GetOrDefault(t *testing.T) {
	p := storage.NewPlugin(storage.NewMemoryStorage())
	store := p.NS("test")

	v := storage.GetOrDefault(bg, store, "missing", 42)
	if v != 42 {
		t.Errorf("expected default 42, got %d", v)
	}
}

// ─── Namespace isolation ───────────────────────────────────────────────────

func TestPlugin_NS_Isolation(t *testing.T) {
	p := storage.NewPlugin(storage.NewMemoryStorage())
	a := p.NS("alpha")
	b := p.NS("beta")

	storage.Set(bg, a, "key", "from-alpha", 0) //nolint:errcheck
	storage.Set(bg, b, "key", "from-beta", 0)  //nolint:errcheck

	va, _ := storage.Get[string](bg, a, "key")
	vb, _ := storage.Get[string](bg, b, "key")
	if va != "from-alpha" || vb != "from-beta" {
		t.Errorf("namespace isolation broken: alpha=%q beta=%q", va, vb)
	}
}

func TestStore_Keys_NamespaceScoped(t *testing.T) {
	p := storage.NewPlugin(storage.NewMemoryStorage())
	store := p.NS("acl")
	other := p.NS("other")

	storage.Set(bg, store, "a", 1, 0)  //nolint:errcheck
	storage.Set(bg, store, "b", 2, 0)  //nolint:errcheck
	storage.Set(bg, other, "x", 99, 0) //nolint:errcheck

	keys, err := store.Keys(bg, "*")
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	for _, k := range keys {
		if k != "a" && k != "b" {
			t.Errorf("unexpected key %q in namespace acl", k)
		}
	}
}

func TestStore_Clear_NamespaceScoped(t *testing.T) {
	p := storage.NewPlugin(storage.NewMemoryStorage())
	ns1 := p.NS("ns1")
	ns2 := p.NS("ns2")

	storage.Set(bg, ns1, "x", 1, 0) //nolint:errcheck
	storage.Set(bg, ns2, "y", 2, 0) //nolint:errcheck

	if err := ns1.Clear(bg); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if ns1.Exists(bg, "x") {
		t.Error("ns1:x should be deleted after Clear")
	}
	if !ns2.Exists(bg, "y") {
		t.Error("ns2:y must not be affected by ns1.Clear")
	}
}

// ─── TTL ───────────────────────────────────────────────────────────────────

func TestStore_TTL(t *testing.T) {
	p := storage.NewPlugin(storage.NewMemoryStorage())
	store := p.NS("test")

	storage.Set(bg, store, "ttl_key", "val", 30*time.Millisecond) //nolint:errcheck
	if !store.Exists(bg, "ttl_key") {
		t.Error("key should exist before TTL expiry")
	}
	time.Sleep(50 * time.Millisecond)
	if store.Exists(bg, "ttl_key") {
		t.Error("key should not exist after TTL expiry")
	}
}

// ─── Delete ────────────────────────────────────────────────────────────────

func TestStore_Delete(t *testing.T) {
	p := storage.NewPlugin(storage.NewMemoryStorage())
	store := p.NS("test")

	storage.Set(bg, store, "del_key", "x", 0) //nolint:errcheck
	store.Delete(bg, "del_key")               //nolint:errcheck
	if store.Exists(bg, "del_key") {
		t.Error("key should not exist after delete")
	}
}

// ─── SQLite backend ─────────────────────────────────────────────────────────

func TestSQLiteStorage_BasicOps(t *testing.T) {
	s, err := storage.NewSQLiteStorage(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	defer s.Close()

	p := storage.NewPlugin(s)
	store := p.NS("test")

	type Item struct{ V string }
	if err := storage.Set(bg, store, "k", Item{"hello"}, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := storage.Get[Item](bg, store, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.V != "hello" {
		t.Errorf("expected 'hello', got %q", got.V)
	}
	if !store.Exists(bg, "k") {
		t.Error("Exists should return true")
	}
	store.Delete(bg, "k") //nolint:errcheck
	if store.Exists(bg, "k") {
		t.Error("Exists should return false after Delete")
	}
}

func TestSQLiteStorage_TTL(t *testing.T) {
	s, err := storage.NewSQLiteStorage(t.TempDir() + "/ttl.db")
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	defer s.Close()

	p := storage.NewPlugin(s)
	store := p.NS("test")
	storage.Set(bg, store, "expiring", "v", 30*time.Millisecond) //nolint:errcheck

	time.Sleep(50 * time.Millisecond)
	_, err = storage.Get[string](bg, store, "expiring")
	if !errors.Is(err, storage.ErrExpired) && !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrExpired or ErrNotFound after TTL, got %v", err)
	}
}

func TestSQLiteStorage_WALMode(t *testing.T) {
	s, err := storage.NewSQLiteStorage(t.TempDir() + "/wal.db")
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	defer s.Close()

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	mode, ok := stats["journal_mode"]
	if !ok {
		t.Fatal("Stats should include journal_mode")
	}
	if mode != "wal" {
		t.Errorf("expected journal_mode=wal, got %q", mode)
	}
}
