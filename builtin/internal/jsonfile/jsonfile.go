// Package jsonfile provides helpers for atomic JSON file persistence.
// All builtin plugins use this package for optional file-based persistence.
package jsonfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	data, err := json.Marshal(v)
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
	if err := json.Unmarshal(data, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

// IsNotExist reports whether err indicates a missing file.
func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
