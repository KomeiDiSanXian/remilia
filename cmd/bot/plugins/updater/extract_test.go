package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// makeTarGz 构造包含指定条目的 tar.gz 归档。
func makeTarGz(t *testing.T, dir string, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(dir, "test.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	for name, data := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o755, Size: int64(len(data)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// makeZip 构造包含指定条目的 zip 归档。
func makeZip(t *testing.T, dir string, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(dir, "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestExtractTarGz(t *testing.T) {
	payload := []byte("#!/bin/sh\necho new-version\n")
	archive := makeTarGz(t, t.TempDir(), map[string][]byte{
		"remilia":   payload,
		"README.md": []byte("docs"),
	})

	dest := t.TempDir()
	got, err := extractBinary(archive, dest, "remilia")
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	data, err := os.ReadFile(got)
	if err != nil || string(data) != string(payload) {
		t.Errorf("extracted content mismatch: %q err=%v", data, err)
	}
}

func TestExtractZip(t *testing.T) {
	payload := []byte("MZ fake windows binary")
	archive := makeZip(t, t.TempDir(), map[string][]byte{
		"remilia.exe": payload,
	})

	dest := t.TempDir()
	got, err := extractBinary(archive, dest, "remilia.exe")
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	data, err := os.ReadFile(got)
	if err != nil || string(data) != string(payload) {
		t.Errorf("extracted content mismatch: %q err=%v", data, err)
	}
}

func TestExtractMissingBinary(t *testing.T) {
	archive := makeTarGz(t, t.TempDir(), map[string][]byte{
		"LICENSE": []byte("text"),
	})
	if _, err := extractBinary(archive, t.TempDir(), "remilia"); err == nil {
		t.Error("should fail when binary missing")
	}
}

func TestExtractPathTraversal(t *testing.T) {
	// 恶意归档：条目名带 ../ 逃逸
	archive := makeTarGz(t, t.TempDir(), map[string][]byte{
		"../../evil": []byte("pwned"),
		"remilia":    []byte("bin"),
	})
	dest := t.TempDir()
	if _, err := extractBinary(archive, dest, "remilia"); err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "..", "evil")); err == nil {
		t.Error("path traversal must be blocked")
	}
}

func TestBinaryMatches(t *testing.T) {
	cases := []struct {
		name, want string
		ok         bool
	}{
		{"remilia", "remilia", true},
		{"remilia.exe", "remilia.exe", true},
		{"./remilia", "remilia", true},
		{"dist/remilia", "remilia", true},
		{"remilia_other", "remilia", false},
		{"README.md", "remilia", false},
	}
	for _, c := range cases {
		if got := binaryMatches(c.name, c.want); got != c.ok {
			t.Errorf("binaryMatches(%q, %q) = %v, want %v", c.name, c.want, got, c.ok)
		}
	}
}
