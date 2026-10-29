// fileaccess.go — 工作区管理器到 catalog.FileAccess 端口的适配。

package ai

import (
	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/workspace"
)

// fileAccessor 把工作区管理器适配为 [catalog.FileAccess]。
//
// 只做类型映射：路径边界、符号链接解析与大小限制都在 workspace 包内，
// 适配器不改写错误、不追加策略。
type fileAccessor struct{ w *workspace.Manager }

// Read 读取工作区内的文本文件。
func (a fileAccessor) Read(rel string, maxBytes int) (catalog.FileContent, error) {
	content, err := a.w.Read(rel, maxBytes)
	if err != nil {
		return catalog.FileContent{}, err
	}
	return catalog.FileContent{
		Path:      content.Path,
		Text:      content.Text,
		Size:      content.Size,
		Truncated: content.Truncated,
	}, nil
}

// List 列出工作区内的目录条目。
func (a fileAccessor) List(rel string, maxEntries int) (catalog.FileListing, error) {
	listing, err := a.w.List(rel, maxEntries)
	if err != nil {
		return catalog.FileListing{}, err
	}
	entries := make([]catalog.FileEntry, 0, len(listing.Entries))
	for _, entry := range listing.Entries {
		entries = append(entries, catalog.FileEntry{
			Name:  entry.Name,
			IsDir: entry.IsDir,
			Size:  entry.Size,
		})
	}
	return catalog.FileListing{
		Entries:   entries,
		Total:     listing.Total,
		Truncated: listing.Truncated,
	}, nil
}

// Write 覆盖写入工作区内的文本文件。
func (a fileAccessor) Write(rel, content string) (catalog.FileWriteResult, error) {
	result, err := a.w.Write(rel, content)
	if err != nil {
		return catalog.FileWriteResult{}, err
	}
	return catalog.FileWriteResult{
		Path:          result.Path,
		Bytes:         result.Bytes,
		PreviousBytes: result.PreviousBytes,
		Created:       result.Created,
	}, nil
}

// Edit 对工作区内现有文件做一次唯一匹配的精确替换。
func (a fileAccessor) Edit(rel, oldString, newString string) (catalog.FileEditResult, error) {
	result, err := a.w.Edit(rel, oldString, newString)
	if err != nil {
		return catalog.FileEditResult{}, err
	}
	return catalog.FileEditResult{
		Path:  result.Path,
		Bytes: result.Bytes,
		Diff:  result.Diff,
	}, nil
}

// 端口实现断言：只读动作使用 FileAccess，写入动作使用 FileWriteAccess。
var (
	_ catalog.FileAccess      = fileAccessor{}
	_ catalog.FileWriteAccess = fileAccessor{}
)
