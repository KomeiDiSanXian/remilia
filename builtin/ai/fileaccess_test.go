package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/config"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
)

func TestFileAccessorReadAndList(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("workspace note"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	ws, err := workspace.New([]string{dir})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	accessor := fileAccessor{w: ws}

	content, err := accessor.Read("note.txt", 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content.Path != "note.txt" || content.Text != "workspace note" || content.Size != 14 {
		t.Fatalf("unexpected content: %+v", content)
	}

	listing, err := accessor.List("", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Total != 2 || len(listing.Entries) != 2 {
		t.Fatalf("unexpected listing: %+v", listing)
	}
	if !listing.Entries[0].IsDir || listing.Entries[0].Name != "sub" {
		t.Fatalf("directories should come first: %+v", listing.Entries)
	}

	if _, err := accessor.Read(filepath.Join("..", "escape.txt"), 0); err == nil {
		t.Fatal("path traversal must be rejected by the workspace")
	}
}

func TestFileAccessorWriteAndEdit(t *testing.T) {
	dir := t.TempDir()
	ws, err := workspace.New([]string{dir})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	accessor := fileAccessor{w: ws}

	// 写入新文件（父目录按需创建）。
	write, err := accessor.Write(filepath.Join("sub", "new.txt"), "hello")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !write.Created || write.Bytes != 5 || write.PreviousBytes != 0 {
		t.Fatalf("unexpected write result: %+v", write)
	}
	content, err := accessor.Read(filepath.Join("sub", "new.txt"), 0)
	if err != nil || content.Text != "hello" {
		t.Fatalf("read back = (%+v, %v)", content, err)
	}

	// 精确替换并返回 diff。
	edit, err := accessor.Edit(filepath.Join("sub", "new.txt"), "hello", "world")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if edit.Bytes != 5 || !strings.Contains(edit.Diff, "+world") || !strings.Contains(edit.Diff, "-hello") {
		t.Fatalf("unexpected edit result: %+v", edit)
	}

	// 越界写入与不唯一替换都被拒绝。
	if _, err := accessor.Write(filepath.Join("..", "escape.txt"), "x"); err == nil {
		t.Fatal("write outside the workspace must be rejected")
	}
	if _, err := accessor.Write("dup.txt", "a a a"); err != nil {
		t.Fatalf("Write(dup): %v", err)
	}
	if _, err := accessor.Edit("dup.txt", "a", "b"); err == nil {
		t.Fatal("non-unique replacement must be rejected")
	}
}

func TestToolCapabilitiesIncludeFiles(t *testing.T) {
	p := &Plugin{}
	if caps := p.toolCapabilities(nil); caps.Files != nil {
		t.Fatal("files port should be nil without a workspace")
	}

	ws, err := workspace.New([]string{t.TempDir()})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	p.files = ws
	p.cfg = &config.Config{}
	if caps := p.toolCapabilities(nil); caps.Files == nil {
		t.Fatal("files port should be present when a workspace is configured")
	}
	if caps := p.toolCapabilities(nil); caps.FileWrite != nil {
		t.Fatal("write port must stay nil unless workspace_allow_write is set")
	}

	p.cfg.WorkspaceAllowWrite = true
	if caps := p.toolCapabilities(nil); caps.FileWrite == nil {
		t.Fatal("write port should be present when writes are enabled")
	}
}
