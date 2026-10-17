package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newTestManager 建立以临时目录为根的工作区。
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := New([]string{dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, dir
}

func TestNewRejectsInvalidRoots(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrNoRoots) {
		t.Fatalf("New(nil) = %v, want ErrNoRoots", err)
	}
	if _, err := New([]string{"  "}); !errors.Is(err, ErrNoRoots) {
		t.Fatalf("New(blank) = %v, want ErrNoRoots", err)
	}
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := New([]string{file}); err == nil {
		t.Fatal("New with a file root should fail")
	}
}

func TestResolveStaysInsideRoots(t *testing.T) {
	m, dir := newTestManager(t)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := m.Resolve(filepath.Join("sub", "a.txt"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// 用管理器解析后的根目录比较：Windows 上 EvalSymlinks 可能把临时目录
	// 展开成 8.3 短名，原始 dir 与真实路径大小写/形式不一致。
	root := m.Roots()[0]
	if !strings.HasPrefix(strings.ToLower(got), strings.ToLower(root)) {
		t.Fatalf("resolved path %q not under root %q", got, root)
	}

	if _, err := m.Resolve(filepath.Join("..", "outside.txt")); !errors.Is(err, ErrOutside) {
		t.Fatalf("traversal error = %v, want ErrOutside", err)
	}
	if _, err := m.Resolve(filepath.Join(dir, "sub", "a.txt")); !errors.Is(err, ErrOutside) {
		t.Fatalf("absolute path error = %v, want ErrOutside", err)
	}
	if runtime.GOOS == "windows" {
		if _, err := m.Resolve(`C:\Windows\win.ini`); !errors.Is(err, ErrOutside) {
			t.Fatalf("drive path error = %v, want ErrOutside", err)
		}
	}
	if _, err := m.Resolve("bad\x00path"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("NUL path error = %v, want ErrInvalidPath", err)
	}
}

func TestResolveRejectsEscapingSymlink(t *testing.T) {
	m, dir := newTestManager(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	if _, err := m.Resolve("link.txt"); !errors.Is(err, ErrOutside) {
		t.Fatalf("escaping symlink error = %v, want ErrOutside", err)
	}
}

func TestReadTruncatesAndRejectsBinary(t *testing.T) {
	m, dir := newTestManager(t)
	if err := os.WriteFile(filepath.Join(dir, "text.txt"), []byte("0123456789"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	content, err := m.Read("text.txt", 4)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content.Text != "0123" || !content.Truncated || content.Size != 10 {
		t.Fatalf("unexpected content: %+v", content)
	}

	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatalf("WriteFile(bin): %v", err)
	}
	if _, err := m.Read("bin.dat", 0); !errors.Is(err, ErrBinary) {
		t.Fatalf("binary read error = %v, want ErrBinary", err)
	}

	if _, err := m.Read(".", 0); err == nil {
		t.Fatal("reading a directory should fail")
	}
}

func TestListSortsAndCaps(t *testing.T) {
	m, dir := newTestManager(t)
	for _, name := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "zdir"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	listing, err := m.List(".", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Total != 3 || len(listing.Entries) != 3 || listing.Truncated {
		t.Fatalf("unexpected listing: %+v", listing)
	}
	if listing.Entries[0].Name != "zdir" || !listing.Entries[0].IsDir {
		t.Fatalf("directories should come first: %+v", listing.Entries)
	}
	if listing.Entries[1].Name != "a.txt" || listing.Entries[2].Name != "b.txt" {
		t.Fatalf("files should be sorted by name: %+v", listing.Entries)
	}

	capped, err := m.List(".", 2)
	if err != nil {
		t.Fatalf("List(capped): %v", err)
	}
	if len(capped.Entries) != 2 || !capped.Truncated || capped.Total != 3 {
		t.Fatalf("unexpected capped listing: %+v", capped)
	}

	if _, err := m.List("a.txt", 0); err == nil {
		t.Fatal("listing a file should fail")
	}
}

func TestWriteCreatesAndOverwrites(t *testing.T) {
	m, dir := newTestManager(t)

	result, err := m.Write(filepath.Join("sub", "new.txt"), "hello")
	if err != nil {
		t.Fatalf("Write(new): %v", err)
	}
	if !result.Created || result.Bytes != 5 || result.PreviousBytes != 0 {
		t.Fatalf("unexpected new-file result: %+v", result)
	}
	content, err := m.Read(filepath.Join("sub", "new.txt"), 0)
	if err != nil || content.Text != "hello" {
		t.Fatalf("read back = (%+v, %v)", content, err)
	}

	result, err = m.Write(filepath.Join("sub", "new.txt"), "hi")
	if err != nil {
		t.Fatalf("Write(overwrite): %v", err)
	}
	if result.Created || result.PreviousBytes != 5 || result.Bytes != 2 {
		t.Fatalf("unexpected overwrite result: %+v", result)
	}

	// 越界、二进制与超大内容都被拒绝。
	if _, err := m.Write(filepath.Join("..", "escape.txt"), "x"); !errors.Is(err, ErrOutside) {
		t.Fatalf("traversal write error = %v, want ErrOutside", err)
	}
	if _, err := m.Write("nul.txt", "a\x00b"); !errors.Is(err, ErrBinary) {
		t.Fatalf("NUL content error = %v, want ErrBinary", err)
	}
	tooBig := strings.Repeat("x", MaxWriteBytes+1)
	if _, err := m.Write("big.txt", tooBig); err == nil {
		t.Fatal("oversized write must be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "big.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected write must not leave a file behind")
	}
}

func TestEditReplacesUniquely(t *testing.T) {
	m, dir := newTestManager(t)
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := m.Edit("code.go", "func main() {}", "func main() { println(\"hi\") }")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if result.Bytes == 0 || !strings.Contains(result.Diff, "-func main() {}") ||
		!strings.Contains(result.Diff, `+func main() { println("hi") }`) {
		t.Fatalf("unexpected edit result: %+v", result)
	}
	content, err := m.Read("code.go", 0)
	if err != nil || !strings.Contains(content.Text, `println("hi")`) {
		t.Fatalf("read back = (%+v, %v)", content, err)
	}

	if _, err := m.Edit("code.go", "missing", "x"); !errors.Is(err, ErrPatternNotFound) {
		t.Fatalf("missing pattern error = %v, want ErrPatternNotFound", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dup.txt"), []byte("a a"), 0o600); err != nil {
		t.Fatalf("WriteFile(dup): %v", err)
	}
	if _, err := m.Edit("dup.txt", "a", "b"); !errors.Is(err, ErrPatternNotUnique) {
		t.Fatalf("duplicate pattern error = %v, want ErrPatternNotUnique", err)
	}
	if _, err := m.Edit("dup.txt", "", "b"); err == nil {
		t.Fatal("empty old_string must be rejected")
	}
	if _, err := m.Edit("missing.txt", "a", "b"); err == nil {
		t.Fatal("editing a missing file must fail")
	}
}
