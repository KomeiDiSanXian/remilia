// Package workspace 任务的文件访问边界。
//
// 工作区由一组已存在的根目录组成；所有路径都相对于根目录解释，解析后必须
// 仍位于某个根目录内（符号链接按解析后的真实路径判断）。绝对路径、盘符路径
// 与越界路径一律拒绝。
//
// 本包当前只提供只读能力（读取文件与列目录）。写入、差异与进程执行属于后续
// 能力，它们必须复用同一套路径解析与边界检查，而不是各自拼接路径。
package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// 领域错误。
var (
	// ErrNoRoots 没有配置任何可用的工作区根目录。
	ErrNoRoots = errors.New("workspace: no roots configured")
	// ErrOutside 路径解析后不在任何工作区根目录内。
	ErrOutside = errors.New("workspace: path is outside the workspace")
	// ErrInvalidPath 路径包含空字节等非法内容。
	ErrInvalidPath = errors.New("workspace: invalid path")
	// ErrBinary 目标文件是二进制文件，不支持按文本读取。
	ErrBinary = errors.New("workspace: binary file")
)

const (
	// MaxReadBytes 单次读取的默认上限（64 KiB）。
	MaxReadBytes = 64 << 10
	// MaxListEntries 单次列目录的默认上限。
	MaxListEntries = 200
)

// Content 一次文件读取的结果。
type Content struct {
	// Path 调用方传入的相对路径。
	Path string
	// Text 文件内容（可能被截断）。
	Text string
	// Size 文件完整大小（字节）。
	Size int64
	// Truncated 内容是否因超过上限被截断。
	Truncated bool
}

// Entry 目录中的一项。
type Entry struct {
	// Name 文件或目录名。
	Name string
	// IsDir 是否为目录。
	IsDir bool
	// Size 文件大小（字节；目录为 0）。
	Size int64
}

// Listing 一次列目录的结果。
type Listing struct {
	// Entries 已排序的条目（目录在前，随后按名称升序）。
	Entries []Entry
	// Total 目录中的条目总数（截断前）。
	Total int
	// Truncated 是否因超过上限被截断。
	Truncated bool
}

// Manager 工作区路径解析与只读访问。
type Manager struct {
	roots []string
}

// New 构造工作区管理器：根目录必须是已存在的目录，构造时解析为真实路径。
func New(roots []string) (*Manager, error) {
	if len(roots) == 0 {
		return nil, ErrNoRoots
	}
	resolved := make([]string, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	for _, raw := range roots {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, fmt.Errorf("workspace: resolve root %q: %w", raw, err)
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("workspace: root %q: %w", raw, err)
		}
		info, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("workspace: root %q: %w", raw, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("workspace: root %q is not a directory", raw)
		}
		key := normalize(real)
		if seen[key] {
			continue
		}
		seen[key] = true
		resolved = append(resolved, real)
	}
	if len(resolved) == 0 {
		return nil, ErrNoRoots
	}
	return &Manager{roots: resolved}, nil
}

// Roots 返回解析后的根目录（副本）。
func (m *Manager) Roots() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.roots...)
}

// Resolve 把相对路径解析为根目录内的真实路径。
//
// 路径必须已存在（读取与列目录都要求存在）；符号链接按最终目标判断边界。
func (m *Manager) Resolve(rel string) (string, error) {
	if m == nil || len(m.roots) == 0 {
		return "", ErrNoRoots
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		rel = "."
	}
	if strings.ContainsRune(rel, 0) {
		return "", ErrInvalidPath
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("%w: %q", ErrOutside, rel)
	}

	var lastErr error
	for _, root := range m.roots {
		candidate := filepath.Join(root, rel)
		// 先做词法边界检查：即使目标不存在，也能把 "../" 之类的越界路径
		// 明确归类为"工作区外"，而不是伪装成"文件不存在"。
		if !within(root, candidate) {
			lastErr = fmt.Errorf("%w: %q", ErrOutside, rel)
			continue
		}
		real, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			lastErr = err
			continue
		}
		if !within(root, real) {
			lastErr = fmt.Errorf("%w: %q", ErrOutside, rel)
			continue
		}
		return real, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("%w: %q", ErrOutside, rel)
}

// Read 读取根目录内的文本文件；超过上限的内容被截断并标记。
func (m *Manager) Read(rel string, maxBytes int) (Content, error) {
	path, err := m.Resolve(rel)
	if err != nil {
		return Content{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Content{}, fmt.Errorf("workspace: stat %q: %w", rel, err)
	}
	if info.IsDir() {
		return Content{}, fmt.Errorf("workspace: %q is a directory", rel)
	}
	if !info.Mode().IsRegular() {
		return Content{}, fmt.Errorf("workspace: %q is not a regular file", rel)
	}

	limit := maxBytes
	if limit <= 0 || limit > MaxReadBytes {
		limit = MaxReadBytes
	}
	f, err := os.Open(path)
	if err != nil {
		return Content{}, fmt.Errorf("workspace: open %q: %w", rel, err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return Content{}, fmt.Errorf("workspace: read %q: %w", rel, err)
	}
	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return Content{}, fmt.Errorf("%w: %q", ErrBinary, rel)
	}
	return Content{Path: rel, Text: string(data), Size: info.Size(), Truncated: truncated}, nil
}

// List 列出根目录内某个目录的条目。
func (m *Manager) List(rel string, maxEntries int) (Listing, error) {
	path, err := m.Resolve(rel)
	if err != nil {
		return Listing{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Listing{}, fmt.Errorf("workspace: stat %q: %w", rel, err)
	}
	if !info.IsDir() {
		return Listing{}, fmt.Errorf("workspace: %q is not a directory", rel)
	}

	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return Listing{}, fmt.Errorf("workspace: list %q: %w", rel, err)
	}
	limit := maxEntries
	if limit <= 0 || limit > MaxListEntries {
		limit = MaxListEntries
	}

	entries := make([]Entry, 0, min(len(dirEntries), limit))
	for _, de := range dirEntries {
		if len(entries) >= limit {
			break
		}
		entryInfo, statErr := de.Info()
		if statErr != nil {
			// 列目录与读取元数据之间条目可能被删除，跳过而不是让整个列表失败。
			continue
		}
		entries = append(entries, Entry{Name: de.Name(), IsDir: de.IsDir(), Size: entryInfo.Size()})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	return Listing{Entries: entries, Total: len(dirEntries), Truncated: len(dirEntries) > len(entries)}, nil
}

// within 报告 target 是否位于 root 内（比较前按平台归一化大小写）。
func within(root, target string) bool {
	root = normalize(filepath.Clean(root))
	target = normalize(filepath.Clean(target))
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// normalize 在大小写不敏感的平台（Windows）上折叠路径大小写。
func normalize(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
