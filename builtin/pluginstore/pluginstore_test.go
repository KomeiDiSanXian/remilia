package pluginstore_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/pluginstore"
)

func newTestDataFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "pluginstore_test.json")
}

func TestRegisterFunc_SaveAndRestore(t *testing.T) {
	p := pluginstore.NewPlugin()
	p.SetDataFileForTest(newTestDataFile(t))
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
	p.SetDataFileForTest(newTestDataFile(t))
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

func TestSave_NoDataFile_ReturnsError(t *testing.T) {
	p := pluginstore.NewPlugin()
	p.RegisterFunc("x", func() (any, error) { return 1, nil }, func(any) error { return nil })
	if err := p.Save("x"); err == nil {
		t.Error("expected error when no data file configured")
	}
}

func TestUnregister_RemovesPlugin(t *testing.T) {
	p := pluginstore.NewPlugin()
	p.RegisterFunc("z", func() (any, error) { return nil, nil }, func(any) error { return nil })
	if !slices.Contains(p.ListRegistered(), "z") {
		t.Fatal("z should be registered")
	}
	p.Unregister("z")
	if slices.Contains(p.ListRegistered(), "z") {
		t.Error("z should not be registered after Unregister")
	}
}

func TestHasStorage(t *testing.T) {
	p := pluginstore.NewPlugin()
	if p.HasStorage() {
		t.Error("HasStorage should be false before binding")
	}
	p.SetDataFileForTest(newTestDataFile(t))
	if !p.HasStorage() {
		t.Error("HasStorage should be true after binding")
	}
}
