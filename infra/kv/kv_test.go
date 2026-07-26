package kv_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/kv"
)

func TestOpenClose(t *testing.T) {
	dir := t.TempDir()
	db, err := kv.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestSetGet(t *testing.T) {
	dir := t.TempDir()
	db, err := kv.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := db.Set([]byte("key1"), []byte("value1")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := db.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("expected %q, got %q", "value1", string(val))
	}
}

func TestGetNotFound(t *testing.T) {
	dir := t.TempDir()
	db, err := kv.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	_, err = db.Get([]byte("nonexistent"))
	if err != kv.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	db, _ := kv.Open(dir)
	defer db.Close()

	db.Set([]byte("delkey"), []byte("delval"))
	if err := db.Delete([]byte("delkey")); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := db.Get([]byte("delkey"))
	if err != kv.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteNonExistent(t *testing.T) {
	dir := t.TempDir()
	db, _ := kv.Open(dir)
	defer db.Close()

	if err := db.Delete([]byte("neverexisted")); err != nil {
		t.Errorf("Delete of nonexistent key should not error, got %v", err)
	}
}

func TestOverwrite(t *testing.T) {
	dir := t.TempDir()
	db, _ := kv.Open(dir)
	defer db.Close()

	db.Set([]byte("key"), []byte("old"))
	db.Set([]byte("key"), []byte("new"))

	val, _ := db.Get([]byte("key"))
	if string(val) != "new" {
		t.Errorf("expected %q, got %q", "new", string(val))
	}
}

func TestMultipleKeys(t *testing.T) {
	dir := t.TempDir()
	db, _ := kv.Open(dir)
	defer db.Close()

	pairs := map[string]string{
		"a": "1",
		"b": "2",
		"c": "3",
	}
	for k, v := range pairs {
		db.Set([]byte(k), []byte(v))
	}
	for k, v := range pairs {
		val, err := db.Get([]byte(k))
		if err != nil {
			t.Errorf("Get(%q) failed: %v", k, err)
		}
		if string(val) != v {
			t.Errorf("Get(%q): expected %q, got %q", k, v, string(val))
		}
	}
}
