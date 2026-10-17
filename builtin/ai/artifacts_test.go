package ai

import (
	"strings"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/task"
)

// fakeFileWriter 记录调用并返回预设结果的写入端口替身。
type fakeFileWriter struct {
	write catalog.FileWriteResult
	edit  catalog.FileEditResult
}

func (f *fakeFileWriter) Write(string, string) (catalog.FileWriteResult, error) {
	return f.write, nil
}

func (f *fakeFileWriter) Edit(string, string, string) (catalog.FileEditResult, error) {
	return f.edit, nil
}

func TestFileArtifactRecording(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	sess := p.sm.GetOrCreate("synthetic:chat:user", "user", "chat")
	pa := p.planAccessFor(ctx, sess)
	execPlanTool(t, planToolForTest(t, catalog.PlanCreateToolName), pa,
		map[string]any{"task": "改文件", "steps": []any{"第一步", "第二步"}})

	// 包装端口：一次新建、一次修改、一次重复修改（就地更新）。
	wrapped := p.fileArtifactRecordingWriter(ctx, &fakeFileWriter{
		write: catalog.FileWriteResult{Path: "docs/note.md", Bytes: 10, Created: true},
	})
	if wrapped == nil {
		t.Fatal("wrapper should be created when a port and context exist")
	}
	if _, err := wrapped.Write("docs/note.md", "hello"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	wrapped = p.fileArtifactRecordingWriter(ctx, &fakeFileWriter{
		edit: catalog.FileEditResult{Path: "docs/note.md", Bytes: 42},
	})
	if _, err := wrapped.Edit("docs/note.md", "a", "b"); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if _, err := wrapped.Edit("docs/note.md", "a", "c"); err != nil {
		t.Fatalf("Edit(again): %v", err)
	}

	tk, err := p.tasks.ActiveBySession(sess.ID)
	if err != nil {
		t.Fatalf("ActiveBySession: %v", err)
	}
	if len(tk.Artifacts) != 2 {
		t.Fatalf("artifacts = %+v, want create and update", tk.Artifacts)
	}
	if tk.Artifacts[0].Kind != task.ArtifactFileCreated || tk.Artifacts[0].Name != "docs/note.md" {
		t.Fatalf("create artifact = %+v", tk.Artifacts[0])
	}
	if tk.Artifacts[1].Kind != task.ArtifactFileUpdated || tk.Artifacts[1].Size != 42 {
		t.Fatalf("update artifact = %+v", tk.Artifacts[1])
	}

	// 详情文本包含产物清单。
	detail := formatTaskDetail(tk)
	if !strings.Contains(detail, "新建 docs/note.md") || !strings.Contains(detail, "修改 docs/note.md") {
		t.Fatalf("detail should list artifacts: %s", detail)
	}

	// 没有写入端口时不创建包装。
	if got := p.fileArtifactRecordingWriter(ctx, nil); got != nil {
		t.Fatal("nil port should stay nil")
	}
}

func TestArtifactRecordingWithoutTaskIsSilent(t *testing.T) {
	p := newTaskPlugin(t)
	ctx := makeManageCtx("/ai", false)
	// 没有活动任务：登记静默跳过，不 panic。
	p.recordFileArtifact(ctx, "orphan.txt", 1, true)
}
