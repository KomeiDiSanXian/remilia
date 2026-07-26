package persist_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/persist"
)

func TestFileLoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	f := persist.NewFile[string](path)

	err := f.Save("hello world")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var val string
	val, err = f.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if val != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", val)
	}
}

func TestFileLoadNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	f := persist.NewFile[string](path)

	val, err := f.Load()
	if err != nil {
		t.Fatalf("Load should not error on missing file: %v", err)
	}
	if val != "" {
		t.Errorf("expected zero value, got %q", val)
	}
}

func TestFileLoadSaveStruct(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "struct.json")

	type Config struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	f := persist.NewFile[Config](path)

	cfg := Config{Name: "test", Count: 42}
	if err := f.Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := f.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded != cfg {
		t.Errorf("expected %+v, got %+v", cfg, loaded)
	}
}

func TestFileCorruptedData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	os.WriteFile(path, []byte("{invalid json"), 0644)

	f := persist.NewFile[string](path)
	_, err := f.Load()
	if err == nil {
		t.Error("expected error for corrupted data")
	}
}

func TestFileOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overwrite.json")

	f := persist.NewFile[string](path)
	f.Save("first")
	f.Save("second")

	val, _ := f.Load()
	if val != "second" {
		t.Errorf("expected %q, got %q", "second", val)
	}
}
