// Package jsonfile provides helpers for atomic JSON file persistence.
// All builtin plugins use this package for optional file-based persistence.
package jsonfile

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
)

// 读旧文件时放宽大小写匹配：历史版本写下的状态文件可能出现键名大小写差异。
// 写新文件仍走 jsonx 默认的严格语义。
var compat = jsonx.CaseInsensitiveNames()

// Write atomically writes v as JSON to path.
// Creates parent directories as needed.
// If path is empty, Write is a no-op and returns nil.
//
// 序列化走 infra/jsonx（encoding/json/v2 + 确定性 map 顺序），保证同一数据
// 写入的文件字节稳定；v2 的 omitempty 只省略 null/""/{}/[]，bool 与数值
// 字段需用 omitzero 才会省略 0/false（本包调用方不受影响）。
func Write(path string, v any) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := jsonx.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Read reads and unmarshals a JSON file into a value of type T.
// Returns [os.ErrNotExist] if the file does not exist.
func Read[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	if err := jsonx.Unmarshal(data, &zero, compat); err != nil {
		return zero, err
	}
	return zero, nil
}

// IsNotExist reports whether err indicates a missing file.
func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
