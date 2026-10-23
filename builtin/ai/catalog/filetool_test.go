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
	for _, tool := range BuildFileTools(FileToolOptions{AllowWrite: true}) {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("file tool %q not built", name)
	return toolkit.Tool{}
}

func TestBuildFileToolsShape(t *testing.T) {
	tools := BuildFileTools(FileToolOptions{})
	if len(tools) != 2 {
		t.Fatalf("read-only tools = %d, want 2", len(tools))
	}
	all := BuildFileTools(FileToolOptions{AllowWrite: true})
	if len(all) != 4 {
		t.Fatalf("tools with write = %d, want 4", len(all))
	}
	for _, tool := range all {
		if len(tool.Permissions) != 1 {
			t.Fatalf("%s permissions = %v", tool.Name, tool.Permissions)
		}
		switch tool.Name {
		case FileReadToolName, FileListToolName:
			if tool.Permissions[0] != FileReadPermission {
				t.Fatalf("%s permissions = %v, want %s", tool.Name, tool.Permissions, FileReadPermission)
			}
			if tool.Selection == nil || *tool.Selection != toolkit.SelectionBaseline {
				t.Fatalf("%s should be explicitly baseline-selected", tool.Name)
			}
		case FileWriteToolName, FileEditToolName:
			if tool.Permissions[0] != FileWritePermission {
				t.Fatalf("%s permissions = %v, want %s", tool.Name, tool.Permissions, FileWritePermission)
			}
			if !tool.RequiresApproval || !tool.AlwaysRequireApproval {
				t.Fatalf("%s must always require approval", tool.Name)
			}
		default:
			t.Fatalf("unexpected tool %q", tool.Name)
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
	edit := fileToolForTest(t, FileEditToolName)
	if len(edit.Parameters.Required) != 3 {
		t.Fatalf("file_edit should require path/old_string/new_string: %+v", edit.Parameters.Required)
	}
}

func TestExecuteFileRead(t *testing.T) {
	fake := &fakeFileAccess{content: FileContent{Path: "docs/readme.md", Text: "hello", Size: 5}}
	tool := fileToolForTest(t, FileReadToolName)
	inv := testInvocation(Capabilities{Files: fake})

	out, err := tool.ExecuteText(context.Background(), inv, map[string]any{"path": "docs/readme.md"})
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

	out, _ := tool.ExecuteText(context.Background(), nil, map[string]any{"path": "x"})
	if !strings.Contains(out, "未启用") {
		t.Fatalf("missing port should be reported: %q", out)
	}

	inv := testInvocation(Capabilities{Files: &fakeFileAccess{}})
	out, _ = tool.ExecuteText(context.Background(), inv, map[string]any{})
	if !strings.Contains(out, "path") {
		t.Fatalf("missing path should be reported: %q", out)
	}

	failing := &fakeFileAccess{readErr: errors.New("path is outside the workspace")}
	inv = testInvocation(Capabilities{Files: failing})
	out, _ = tool.ExecuteText(context.Background(), inv, map[string]any{"path": "../secret"})
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
	inv := testInvocation(Capabilities{Files: fake})

	out, err := tool.ExecuteText(context.Background(), inv, map[string]any{"path": "proj"})
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
	inv = testInvocation(Capabilities{Files: empty})
	if out, _ := tool.ExecuteText(context.Background(), inv, map[string]any{}); out != "目录为空" {
		t.Fatalf("empty listing = %q", out)
	}
}

// fakeFileWrite 记录调用并返回预设结果的写入端口替身。
type fakeFileWrite struct {
	writeResult FileWriteResult
	writeErr    error
	editResult  FileEditResult
	editErr     error

	writePath    string
	writeContent string
	editPath     string
	editOld      string
	editNew      string
}

func (f *fakeFileWrite) Write(rel, content string) (FileWriteResult, error) {
	f.writePath = rel
	f.writeContent = content
	return f.writeResult, f.writeErr
}

func (f *fakeFileWrite) Edit(rel, oldString, newString string) (FileEditResult, error) {
	f.editPath = rel
	f.editOld = oldString
	f.editNew = newString
	return f.editResult, f.editErr
}

func TestExecuteFileWrite(t *testing.T) {
	tool := fileToolForTest(t, FileWriteToolName)

	out, _ := tool.ExecuteText(context.Background(), nil, map[string]any{"path": "a.txt", "content": "x"})
	if !strings.Contains(out, "未启用") {
		t.Fatalf("missing write port should be reported: %q", out)
	}

	fake := &fakeFileWrite{writeResult: FileWriteResult{Path: "a.txt", Bytes: 5, PreviousBytes: 3}}
	inv := testInvocation(Capabilities{FileWrite: fake})
	out, err := tool.ExecuteText(context.Background(), inv, map[string]any{"path": "a.txt", "content": "hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "已覆盖") || !strings.Contains(out, "3 字节 → 5 字节") {
		t.Fatalf("unexpected result: %q", out)
	}
	if fake.writePath != "a.txt" || fake.writeContent != "hello" {
		t.Fatalf("port called with (%q, %q)", fake.writePath, fake.writeContent)
	}

	created := &fakeFileWrite{writeResult: FileWriteResult{Path: "new.txt", Bytes: 1, Created: true}}
	inv = testInvocation(Capabilities{FileWrite: created})
	if out, _ := tool.ExecuteText(context.Background(), inv, map[string]any{"path": "new.txt", "content": "y"}); !strings.Contains(out, "已创建") {
		t.Fatalf("new file should be reported as created: %q", out)
	}
}

func TestExecuteFileEdit(t *testing.T) {
	tool := fileToolForTest(t, FileEditToolName)
	fake := &fakeFileWrite{editResult: FileEditResult{
		Path:  "main.go",
		Bytes: 42,
		Diff:  "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n",
	}}
	inv := testInvocation(Capabilities{FileWrite: fake})

	out, err := tool.ExecuteText(context.Background(), inv, map[string]any{
		"path": "main.go", "old_string": "old", "new_string": "new",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "已更新 main.go") || !strings.Contains(out, "+new") {
		t.Fatalf("unexpected result: %q", out)
	}
	if fake.editOld != "old" || fake.editNew != "new" {
		t.Fatalf("port called with (%q, %q)", fake.editOld, fake.editNew)
	}

	if out, _ := tool.ExecuteText(context.Background(), inv, map[string]any{"path": "main.go", "new_string": "x"}); !strings.Contains(out, "old_string") {
		t.Fatalf("empty old_string should be rejected: %q", out)
	}
	failing := &fakeFileWrite{editErr: errors.New("pattern is not unique")}
	inv = testInvocation(Capabilities{FileWrite: failing})
	if out, _ := tool.ExecuteText(context.Background(), inv, map[string]any{"path": "a.txt", "old_string": "a", "new_string": "b"}); !strings.Contains(out, "替换失败") {
		t.Fatalf("port error should be surfaced: %q", out)
	}
}
