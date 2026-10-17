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
