package persist

import (
	"encoding/json"
	"os"
)

// File 基于 JSON 文件的泛型持久化。
// T 必须是可 JSON 序列化的类型。
type File[T any] struct {
	path string
}

// NewFile 创建文件持久化实例。
func NewFile[T any](path string) *File[T] {
	return &File[T]{path: path}
}

// Load 从文件读取并反序列化。文件不存在时返回零值。
func (f *File[T]) Load() (val T, err error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return val, nil
		}
		return val, err
	}
	err = json.Unmarshal(data, &val)
	return val, err
}

// Save 序列化并写入文件。
func (f *File[T]) Save(val T) error {
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, data, 0644)
}
