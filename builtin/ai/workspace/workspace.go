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
	// ErrBinary 目标文件是二进制文件，不支持按文本读取或修改。
	ErrBinary = errors.New("workspace: binary file")
	// ErrPatternNotFound 精确替换的目标内容不存在。
	ErrPatternNotFound = errors.New("workspace: pattern not found")
	// ErrPatternNotUnique 精确替换的目标内容出现多次，无法确定替换位置。
	ErrPatternNotUnique = errors.New("workspace: pattern is not unique")
)

const (
	// MaxReadBytes 单次读取的默认上限（64 KiB）。
	MaxReadBytes = 64 << 10
	// MaxListEntries 单次列目录的默认上限。
	MaxListEntries = 200
	// MaxWriteBytes 单次写入的上限（1 MiB）。
	MaxWriteBytes = 1 << 20
	// MaxEditFileBytes 可精确替换的文件大小上限（2 MiB）。
	MaxEditFileBytes = 2 << 20
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
	return m.resolve(rel)
}

// resolve 把相对路径解析为根目录内的真实路径；允许最末若干分量尚不存在
// （写入新文件时需要），已存在的路径分量一律按真实路径判断边界。
func (m *Manager) resolve(rel string) (string, error) {
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
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		// 词法越界：即使目标不存在，也明确归类为"工作区外"，
		// 而不是伪装成"文件不存在"。
		return "", fmt.Errorf("%w: %q", ErrOutside, rel)
	}

	var lastErr error
	for _, root := range m.roots {
		candidate := filepath.Join(root, clean)
		if !within(root, candidate) {
			lastErr = fmt.Errorf("%w: %q", ErrOutside, rel)
			continue
		}
		real, err := resolveExistingAncestor(candidate)
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

// resolveExistingAncestor 解析 candidate：
//   - 目标存在时等价于 EvalSymlinks（末段符号链接按最终目标判定）；
//   - 目标不存在时解析其最近的已存在祖先，再把剩余分量拼回，
//     使"在工作区内新建文件/目录"同样经过边界检查。
func resolveExistingAncestor(candidate string) (string, error) {
	existing := candidate
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", os.ErrNotExist
		}
		existing = parent
	}
	realAncestor, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	remainder, err := filepath.Rel(existing, candidate)
	if err != nil {
		return "", err
	}
	if remainder == "." {
		return realAncestor, nil
	}
	return filepath.Join(realAncestor, remainder), nil
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

// WriteResult 一次文件写入的结果。
type WriteResult struct {
	// Path 调用方传入的相对路径。
	Path string
	// Bytes 写入后的字节数。
	Bytes int
	// PreviousBytes 写入前的字节数（新建为 0）。
	PreviousBytes int
	// Created 是否新建了文件。
	Created bool
}

// EditResult 一次精确替换的结果。
type EditResult struct {
	// Path 调用方传入的相对路径。
	Path string
	// Bytes 替换后的字节数。
	Bytes int
	// Diff 替换处的单块 diff（unified 风格，供回填与交付报告使用）。
	Diff string
}

// Write 覆盖写入根目录内的文本文件（目标不存在时创建，父目录按需创建）。
//
// 写入以"临时文件 + 重命名"方式原子落盘；保留已有文件的权限位。
func (m *Manager) Write(rel, content string) (WriteResult, error) {
	if strings.IndexByte(content, 0) >= 0 {
		return WriteResult{}, ErrBinary
	}
	if len(content) > MaxWriteBytes {
		return WriteResult{}, fmt.Errorf("workspace: content exceeds %d bytes", MaxWriteBytes)
	}
	path, err := m.resolve(rel)
	if err != nil {
		return WriteResult{}, err
	}

	result := WriteResult{Path: rel, Bytes: len(content)}
	switch info, statErr := os.Stat(path); {
	case statErr == nil:
		if info.IsDir() {
			return WriteResult{}, fmt.Errorf("workspace: %q is a directory", rel)
		}
		if !info.Mode().IsRegular() {
			return WriteResult{}, fmt.Errorf("workspace: %q is not a regular file", rel)
		}
		result.PreviousBytes = int(info.Size())
	case errors.Is(statErr, os.ErrNotExist):
		result.Created = true
	default:
		return WriteResult{}, fmt.Errorf("workspace: stat %q: %w", rel, statErr)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return WriteResult{}, fmt.Errorf("workspace: create parent of %q: %w", rel, err)
	}
	if err := writeFileAtomic(path, []byte(content)); err != nil {
		return WriteResult{}, fmt.Errorf("workspace: write %q: %w", rel, err)
	}
	return result, nil
}

// Edit 对根目录内的文本文件做一次唯一匹配的字符串替换。
//
// 匹配不唯一或不存在时返回错误，不做任何写入——精确替换是模型能安全修改
// 现有文件的基线能力，模糊匹配会让"改错了地方"无声发生。
func (m *Manager) Edit(rel, oldString, newString string) (EditResult, error) {
	if oldString == "" {
		return EditResult{}, errors.New("workspace: old content must not be empty")
	}
	if strings.IndexByte(newString, 0) >= 0 {
		return EditResult{}, ErrBinary
	}
	path, err := m.resolve(rel)
	if err != nil {
		return EditResult{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return EditResult{}, fmt.Errorf("workspace: stat %q: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		return EditResult{}, fmt.Errorf("workspace: %q is not a regular file", rel)
	}
	if info.Size() > MaxEditFileBytes {
		return EditResult{}, fmt.Errorf("workspace: %q exceeds the edit size limit (%d bytes)", rel, MaxEditFileBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return EditResult{}, fmt.Errorf("workspace: read %q: %w", rel, err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return EditResult{}, fmt.Errorf("%w: %q", ErrBinary, rel)
	}
	content := string(data)
	switch count := strings.Count(content, oldString); {
	case count == 0:
		return EditResult{}, fmt.Errorf("%w: content to replace was not found in %q", ErrPatternNotFound, rel)
	case count > 1:
		return EditResult{}, fmt.Errorf("%w: content to replace appears %d times in %q", ErrPatternNotUnique, count, rel)
	}

	next := strings.Replace(content, oldString, newString, 1)
	if len(next) > MaxWriteBytes {
		return EditResult{}, fmt.Errorf("workspace: result exceeds %d bytes", MaxWriteBytes)
	}
	diff := buildEditHunk(rel, content, oldString, newString)
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return EditResult{}, fmt.Errorf("workspace: write %q: %w", rel, err)
	}
	return EditResult{Path: rel, Bytes: len(next), Diff: diff}, nil
}

// buildEditHunk 生成替换处的单块 diff（行号取替换起点所在行）。
func buildEditHunk(rel, content, oldString, newString string) string {
	before, _, ok := strings.Cut(content, oldString)
	if !ok {
		return ""
	}
	line := strings.Count(before, "\n") + 1
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n@@ -%d +%d @@\n", rel, rel, line, line)
	for l := range strings.SplitSeq(strings.TrimSuffix(oldString, "\n"), "\n") {
		b.WriteString("-" + l + "\n")
	}
	for l := range strings.SplitSeq(strings.TrimSuffix(newString, "\n"), "\n") {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

// writeFileAtomic 以临时文件 + 重命名的原子方式写入文件。
// 目标已存在时保留其权限位；新文件使用 0644。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".remilia-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// 成功路径下 Rename 已移走临时文件，这里的删除会失败或无事发生。
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
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
