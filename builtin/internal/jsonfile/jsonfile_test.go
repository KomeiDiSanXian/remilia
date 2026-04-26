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
