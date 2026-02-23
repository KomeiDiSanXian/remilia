package pluginstore_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugins/pluginstore"
)

// fakeStorage is an in-memory storageBackend for tests.
type fakeStorage struct {
	data map[string][]byte
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{data: make(map[string][]byte)}
}
func (f *fakeStorage) Get(key string) ([]byte, error) {
	v, ok := f.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return v, nil
}
func (f *fakeStorage) Set(key string, value []byte, _ time.Duration) error {
	f.data[key] = value
	return nil
}
func TestRegisterFunc_SaveAndRestore(t *testing.T) {
	p := pluginstore.NewPlugin()
	fs := newFakeStorage()
	p.SetStorageForTest(fs)
	type myState struct {
		Counter int    `json:"counter"`
		Name    string `json:"name"`
	}
	saved := &myState{Counter: 42, Name: "hello"}
	restored := &myState{}
	p.RegisterFunc("myplugin",
		func() (any, error) { return saved, nil },
		func(v any) error {
			data, _ := json.Marshal(v)
			return json.Unmarshal(data, restored)
		},
	)
	// Save
	if err := p.Save("myplugin"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Wipe the restored value and restore again
	restored.Counter = 0
	restored.Name = ""
	// Re-register to trigger restore
	p.RegisterFunc("myplugin",
		func() (any, error) { return saved, nil },
		func(v any) error {
			data, _ := json.Marshal(v)
			return json.Unmarshal(data, restored)
		},
	)
	if restored.Counter != 42 || restored.Name != "hello" {
		t.Errorf("expected counter=42 name=hello, got counter=%d name=%s", restored.Counter, restored.Name)
	}
}
func TestSaveAll_MultiplePugins(t *testing.T) {
	p := pluginstore.NewPlugin()
	fs := newFakeStorage()
	p.SetStorageForTest(fs)
	p.RegisterFunc("alpha", func() (any, error) { return map[string]any{"v": 1}, nil }, func(any) error { return nil })
	p.RegisterFunc("beta", func() (any, error) { return map[string]any{"v": 2}, nil }, func(any) error { return nil })
	saved, failed := p.SaveAll()
	if saved != 2 {
		t.Errorf("expected 2 saved, got %d", saved)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed, got %d", failed)
	}
}
func TestSave_NoStorage_ReturnsError(t *testing.T) {
	p := pluginstore.NewPlugin()
	p.RegisterFunc("x", func() (any, error) { return 1, nil }, func(any) error { return nil })
	if err := p.Save("x"); err == nil {
		t.Error("expected error when no storage bound")
	}
}
func TestUnregister_RemovesPlugin(t *testing.T) {
	p := pluginstore.NewPlugin()
	p.RegisterFunc("z", func() (any, error) { return nil, nil }, func(any) error { return nil })
	if !contains(p.ListRegistered(), "z") {
		t.Fatal("z should be registered")
	}
	p.Unregister("z")
	if contains(p.ListRegistered(), "z") {
		t.Error("z should not be registered after Unregister")
	}
}
func TestHasStorage(t *testing.T) {
	p := pluginstore.NewPlugin()
	if p.HasStorage() {
		t.Error("HasStorage should be false before binding")
	}
	p.SetStorageForTest(newFakeStorage())
	if !p.HasStorage() {
		t.Error("HasStorage should be true after binding")
	}
}
func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}
