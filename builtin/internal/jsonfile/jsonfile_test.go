package jsonfile

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testData struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestWriteReadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	want := testData{Name: "hello", Value: 42}

	err := Write(path, want)
	require.NoError(t, err)

	got, err := Read[testData](path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestWriteCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "data.json")
	want := testData{Name: "nested", Value: 99}

	err := Write(path, want)
	require.NoError(t, err)

	got, err := Read[testData](path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestWriteEmptyPathIsNoop(t *testing.T) {
	err := Write("", testData{Name: "noop", Value: 1})
	assert.NoError(t, err)
}

func TestReadNonExistentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")

	got, err := Read[testData](path)
	assert.Error(t, err)
	assert.True(t, IsNotExist(err))
	assert.Zero(t, got)
}

func TestReadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	err := os.WriteFile(path, []byte("{invalid}"), 0644)
	require.NoError(t, err)

	got, err := Read[testData](path)
	assert.Error(t, err)
	assert.Zero(t, got)
}

func TestIsNotExist(t *testing.T) {
	assert.True(t, IsNotExist(os.ErrNotExist))
	assert.False(t, IsNotExist(io.EOF))
	assert.False(t, IsNotExist(nil))
}

// 历史文件可能由 v1（大小写不敏感）或不同大小写的键名写成，
// 读取必须兼容；写出仍是严格语义下的规范键名。
func TestReadAcceptsCaseInsensitiveKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"Name":"legacy","VALUE":7}`), 0644))

	got, err := Read[testData](path)
	require.NoError(t, err)
	assert.Equal(t, "legacy", got.Name)
	assert.Equal(t, 7, got.Value)
}

// 写出时 map 键必须稳定排序，否则同一份数据每次落盘都不同字节，
// 会让"内容没变"的写入变成无意义的文件改动。
func TestWriteIsDeterministic(t *testing.T) {
	type holder struct {
		Tags map[string]int `json:"tags"`
	}
	dir := t.TempDir()
	v := holder{Tags: map[string]int{"c": 3, "a": 1, "b": 2}}

	first := filepath.Join(dir, "a.json")
	second := filepath.Join(dir, "b.json")
	require.NoError(t, Write(first, v))
	require.NoError(t, Write(second, v))

	a, err := os.ReadFile(first)
	require.NoError(t, err)
	b, err := os.ReadFile(second)
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b))
	assert.Contains(t, string(a), `"tags":{"a":1,"b":2,"c":3}`)
}

func TestAtomicWriteReplacesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atomic.json")

	err := Write(path, testData{Name: "first", Value: 1})
	require.NoError(t, err)

	err = Write(path, testData{Name: "second", Value: 2})
	require.NoError(t, err)

	got, err := Read[testData](path)
	require.NoError(t, err)
	assert.Equal(t, "second", got.Name)
	assert.Equal(t, 2, got.Value)
}
