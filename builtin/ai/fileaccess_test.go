package ai

import (
	"os"
	"path/filepath"
	"testing"

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

func TestToolCapabilitiesIncludeFiles(t *testing.T) {
	p := &Plugin{}
	if caps := p.toolCapabilities(); caps.Files != nil {
		t.Fatal("files port should be nil without a workspace")
	}

	ws, err := workspace.New([]string{t.TempDir()})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	p.files = ws
	if caps := p.toolCapabilities(); caps.Files == nil {
		t.Fatal("files port should be present when a workspace is configured")
	}
}
