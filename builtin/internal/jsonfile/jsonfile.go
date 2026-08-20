// Package jsonfile provides helpers for atomic JSON file persistence.
// All builtin plugins use this package for optional file-based persistence.
package jsonfile

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
)

// opts 使用 encoding/json/v2（Go 1.27）并启用确定性输出：
// map 键按字典序序列化，保证同一数据写入的文件字节稳定。
//
// 注意：v2 与 v1 在语义上存在差异——"omitempty" 仅在值为
// null/""/{}/[] 时省略（数字 0 与 false 不会省略），且解到 any
// 时得到 jsontext.Value 而非 float64。jsonfile 的当前调用方
// （broadcast/pluginstore）不涉及这些差异；如需 v1 语义请继续使用
// encoding/json。
var opts = jsonv2.JoinOptions(
	jsonv2.MatchCaseInsensitiveNames(true),
	jsonv2.Deterministic(true),
)

// Write atomically writes v as JSON to path.
// Creates parent directories as needed.
// If path is empty, Write is a no-op and returns nil.
func Write(path string, v any) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := jsonv2.Marshal(v, opts)
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
	if err := jsonv2.Unmarshal(data, &zero, opts); err != nil {
		return zero, err
	}
	return zero, nil
}

// IsNotExist reports whether err indicates a missing file.
func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
