package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// fakeFileAccess 记录调用并返回预设结果的端口替身。
type fakeFileAccess struct {
	content FileContent
	readErr error
	listing FileListing
	listErr error

	readPath  string
	readMax   int
	listPath  string
	listMax   int
	readCalls int
	listCalls int
}

func (f *fakeFileAccess) Read(rel string, maxBytes int) (FileContent, error) {
	f.readCalls++
	f.readPath = rel
	f.readMax = maxBytes
	return f.content, f.readErr
}

func (f *fakeFileAccess) List(rel string, maxEntries int) (FileListing, error) {
	f.listCalls++
	f.listPath = rel
	f.listMax = maxEntries
	return f.listing, f.listErr
}

// fileToolForTest 从构建结果中取指定动作。
func fileToolForTest(t *testing.T, name string) toolkit.Tool {
	t.Helper()
	for _, tool := range BuildFileTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("file tool %q not built", name)
	return toolkit.Tool{}
}

func TestBuildFileToolsShape(t *testing.T) {
	tools := BuildFileTools()
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
	for _, tool := range tools {
		if len(tool.Permissions) != 1 || tool.Permissions[0] != FileReadPermission {
			t.Fatalf("%s permissions = %v, want %s", tool.Name, tool.Permissions, FileReadPermission)
		}
		if tool.Selection == nil || *tool.Selection != toolkit.SelectionBaseline {
			t.Fatalf("%s should be explicitly baseline-selected", tool.Name)
		}
	}
	read := fileToolForTest(t, FileReadToolName)
	if len(read.Parameters.Required) != 1 || read.Parameters.Required[0] != "path" {
		t.Fatalf("file_read should require path: %+v", read.Parameters)
	}
	list := fileToolForTest(t, FileListToolName)
	if len(list.Parameters.Required) != 0 {
		t.Fatalf("file_list path should be optional: %+v", list.Parameters)
	}
}

func TestExecuteFileRead(t *testing.T) {
	fake := &fakeFileAccess{content: FileContent{Path: "docs/readme.md", Text: "hello", Size: 5}}
	tool := fileToolForTest(t, FileReadToolName)
	ctx := WithCapabilities(context.Background(), Capabilities{Files: fake})

	out, err := tool.Execute(ctx, map[string]any{"path": "docs/readme.md"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"docs/readme.md", "hello", "数据结束"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result missing %q: %s", want, out)
		}
	}
	if fake.readPath != "docs/readme.md" || fake.readMax != FileReadMaxBytes {
		t.Fatalf("port called with (%q, %d)", fake.readPath, fake.readMax)
	}
}

func TestExecuteFileReadErrors(t *testing.T) {
	tool := fileToolForTest(t, FileReadToolName)

	out, _ := tool.Execute(context.Background(), map[string]any{"path": "x"})
	if !strings.Contains(out, "未启用") {
		t.Fatalf("missing port should be reported: %q", out)
	}

	ctx := WithCapabilities(context.Background(), Capabilities{Files: &fakeFileAccess{}})
	out, _ = tool.Execute(ctx, map[string]any{})
	if !strings.Contains(out, "path") {
		t.Fatalf("missing path should be reported: %q", out)
	}

	failing := &fakeFileAccess{readErr: errors.New("path is outside the workspace")}
	ctx = WithCapabilities(context.Background(), Capabilities{Files: failing})
	out, _ = tool.Execute(ctx, map[string]any{"path": "../secret"})
	if !strings.Contains(out, "读取失败") || !strings.Contains(out, "outside") {
		t.Fatalf("port error should be surfaced: %q", out)
	}
}

func TestExecuteFileList(t *testing.T) {
	fake := &fakeFileAccess{listing: FileListing{
		Entries: []FileEntry{
			{Name: "src", IsDir: true},
			{Name: "main.go", Size: 128},
		},
		Total: 3, Truncated: true,
	}}
	tool := fileToolForTest(t, FileListToolName)
	ctx := WithCapabilities(context.Background(), Capabilities{Files: fake})

	out, err := tool.Execute(ctx, map[string]any{"path": "proj"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"[目录] src/", "[文件] main.go", "共 3 项", "仅显示前 2 项"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result missing %q: %s", want, out)
		}
	}
	if fake.listPath != "proj" || fake.listMax != FileListMaxEntries {
		t.Fatalf("port called with (%q, %d)", fake.listPath, fake.listMax)
	}

	empty := &fakeFileAccess{}
	ctx = WithCapabilities(context.Background(), Capabilities{Files: empty})
	if out, _ := tool.Execute(ctx, map[string]any{}); out != "目录为空" {
		t.Fatalf("empty listing = %q", out)
	}
}
