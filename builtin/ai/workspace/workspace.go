// Package workspace 任务的文件访问边界。
//
// 工作区由一组已存在的根目录组成；所有路径都相对于根目录解释，解析后必须
// 仍位于某个根目录内。绝对路径、盘符路径与越界路径一律拒绝。
//
// 边界由 *os.Root 在内核层强制：每次打开/读写/改名都以根目录句柄为基准，
// ".." 分量与指向根目录外的符号链接都会被拒绝，不依赖"先解析再打开"的字符串
// 校验，因此也不存在检查与使用之间的竞态窗口。本包提供读取、列目录、写入
// （原子落盘）与精确替换；进程执行（builtin/ai/procexec）复用同一套根目录
// 语义取得工作目录。
package workspace

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
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

// Resolve 把相对路径解析为根目录内的绝对路径。
//
// 路径必须已存在（读取与列目录都要求存在）；符号链接按最终目标判断边界，
// 逃逸出根目录的符号链接返回 ErrOutside。
func (m *Manager) Resolve(rel string) (string, error) {
	root, name, err := m.openRoot(rel)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if _, err := root.Stat(name); err != nil {
		return "", mapRootErr(err)
	}
	return filepath.Join(root.Name(), name), nil
}

// Dir 校验 rel 指向根目录内的目录，并返回可用于子进程工作目录的绝对路径。
//
// 存在的判断由 *os.Root 完成：路径分量与符号链接都不能越出根目录。
func (m *Manager) Dir(rel string) (string, error) {
	root, name, err := m.openRoot(rel)
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil {
		return "", mapRootErr(err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace: %q is not a directory", rel)
	}
	return filepath.Join(root.Name(), name), nil
}

// openRoot 为相对路径选择一个根目录，返回已打开的 *os.Root 句柄与根内相对
// 路径。调用方负责关闭句柄。
//
// 所有文件访问都经由 *os.Root：越界（".." 分量、盘符、逃逸的符号链接）由
// 内核在每次操作时判定，既不依赖上层的字符串比较，也不存在"校验完再打开"
// 的 TOCTOU 窗口。多根目录时优先选择目标已存在的那个，都不存在则落在第一个
// 可用根目录上（写入新文件的情形）。
func (m *Manager) openRoot(rel string) (*os.Root, string, error) {
	if m == nil || len(m.roots) == 0 {
		return nil, "", ErrNoRoots
	}
	name, err := cleanRel(rel)
	if err != nil {
		return nil, "", err
	}
	var fallback *os.Root
	for _, dir := range m.roots {
		root, openErr := os.OpenRoot(dir)
		if openErr != nil {
			continue
		}
		if fallback == nil {
			fallback = root
			if _, statErr := root.Stat(name); statErr == nil {
				return root, name, nil
			}
			continue
		}
		_, statErr := root.Stat(name)
		_ = root.Close()
		if statErr != nil {
			continue
		}
		_ = fallback.Close()
		winner, openErr := os.OpenRoot(dir)
		if openErr != nil {
			continue
		}
		return winner, name, nil
	}
	if fallback == nil {
		return nil, "", ErrNoRoots
	}
	return fallback, name, nil
}

// cleanRel 词法归一化相对路径：拒绝空字节、绝对路径/盘符路径与 ".." 越界。
// 其余边界由 *os.Root 在操作时强制。
func cleanRel(rel string) (string, error) {
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
	return clean, nil
}

// mapRootErr 把 *os.Root 的越界错误归一化为 ErrOutside（逃逸的符号链接、
// 指向根外的路径分量）。其余错误原样返回，保留"不存在""无权限"等语义。
func mapRootErr(err error) error {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// os.Root 未导出逃逸错误类型，只能按错误文本识别（Go 1.24 起稳定为
	// "path escapes from parent"）。
	if strings.Contains(err.Error(), "escapes from parent") {
		return fmt.Errorf("%w: %w", ErrOutside, err)
	}
	return err
}

// Read 读取根目录内的文本文件；超过上限的内容被截断并标记。
func (m *Manager) Read(rel string, maxBytes int) (Content, error) {
	root, name, err := m.openRoot(rel)
	if err != nil {
		return Content{}, err
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil {
		return Content{}, fmt.Errorf("workspace: stat %q: %w", rel, mapRootErr(err))
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
	f, err := root.Open(name)
	if err != nil {
		return Content{}, fmt.Errorf("workspace: open %q: %w", rel, mapRootErr(err))
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
	root, name, err := m.openRoot(rel)
	if err != nil {
		return Listing{}, err
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil {
		return Listing{}, fmt.Errorf("workspace: stat %q: %w", rel, mapRootErr(err))
	}
	if !info.IsDir() {
		return Listing{}, fmt.Errorf("workspace: %q is not a directory", rel)
	}

	dir, err := root.Open(name)
	if err != nil {
		return Listing{}, fmt.Errorf("workspace: list %q: %w", rel, mapRootErr(err))
	}
	defer dir.Close()
	dirEntries, err := dir.ReadDir(-1)
	if err != nil {
		return Listing{}, fmt.Errorf("workspace: list %q: %w", rel, err)
	}
	limit := maxEntries
	if limit <= 0 || limit > MaxListEntries {
		limit = MaxListEntries
	}

	// 容量按固定上限而非调用方传入的 limit 预估：limit 已被夹到
	// MaxListEntries 以内，避免"用户可控值直接决定分配大小"。
	entries := make([]Entry, 0, min(len(dirEntries), MaxListEntries))
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
	root, name, err := m.openRoot(rel)
	if err != nil {
		return WriteResult{}, err
	}
	defer root.Close()

	result := WriteResult{Path: rel, Bytes: len(content)}
	switch info, statErr := root.Stat(name); {
	case statErr == nil:
		if info.IsDir() {
			return WriteResult{}, fmt.Errorf("workspace: %q is a directory", rel)
		}
		if !info.Mode().IsRegular() {
			return WriteResult{}, fmt.Errorf("workspace: %q is not a regular file", rel)
		}
		result.PreviousBytes = int(info.Size())
	case errors.Is(statErr, fs.ErrNotExist):
		result.Created = true
	default:
		return WriteResult{}, fmt.Errorf("workspace: stat %q: %w", rel, mapRootErr(statErr))
	}

	if dir := filepath.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return WriteResult{}, fmt.Errorf("workspace: create parent of %q: %w", rel, mapRootErr(err))
		}
	}
	if err := writeFileAtomic(root, name, []byte(content)); err != nil {
		return WriteResult{}, fmt.Errorf("workspace: write %q: %w", rel, mapRootErr(err))
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
	root, name, err := m.openRoot(rel)
	if err != nil {
		return EditResult{}, err
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil {
		return EditResult{}, fmt.Errorf("workspace: stat %q: %w", rel, mapRootErr(err))
	}
	if !info.Mode().IsRegular() {
		return EditResult{}, fmt.Errorf("workspace: %q is not a regular file", rel)
	}
	if info.Size() > MaxEditFileBytes {
		return EditResult{}, fmt.Errorf("workspace: %q exceeds the edit size limit (%d bytes)", rel, MaxEditFileBytes)
	}

	data, err := root.ReadFile(name)
	if err != nil {
		return EditResult{}, fmt.Errorf("workspace: read %q: %w", rel, mapRootErr(err))
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
	if err := writeFileAtomic(root, name, []byte(next)); err != nil {
		return EditResult{}, fmt.Errorf("workspace: write %q: %w", rel, mapRootErr(err))
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
//
// 临时文件与最终文件都经由同一个 *os.Root 创建/改名：路径不能越出根目录，
// 也不存在"先检查再写"的 TOCTOU 窗口。
func writeFileAtomic(root *os.Root, name string, data []byte) error {
	dir, base := filepath.Split(name)
	tmpName := filepath.Join(dir, "."+base+".remilia-"+randomSuffix()+".tmp")
	f, err := root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		// 成功路径下 Rename 已移走临时文件，这里的删除会失败或无事发生。
		_ = root.Remove(tmpName)
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if info, statErr := root.Stat(name); statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := root.Chmod(tmpName, perm); err != nil {
		return err
	}
	return root.Rename(tmpName, name)
}

// randomSuffix 返回临时文件名后缀（8 字节随机数的十六进制）。
func randomSuffix() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 失败极罕见（且此时写入本就不安全），退回时间戳保证唯一。
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// normalize 在大小写不敏感的平台（Windows）上折叠路径大小写。
func normalize(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
