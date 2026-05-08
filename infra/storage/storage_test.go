package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

type testModel struct {
	ID   uint   `gorm:"primaryKey;autoIncrement"`
	Name string `gorm:"not null"`
	Val  int
}

func TestSQLiteMemoryDB(t *testing.T) {
	client, err := storage.NewMemory()
	require.NoError(t, err)
	require.NotNil(t, client)

	// AutoMigrate
	err = client.AutoMigrate(&testModel{})
	require.NoError(t, err)

	// Create
	m := &testModel{Name: "hello", Val: 42}
	err = client.Create(m)
	require.NoError(t, err)
	assert.NotZero(t, m.ID)

	// Find
	var rows []testModel
	err = client.Find(&rows)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, "hello", rows[0].Name)

	// First
	var first testModel
	err = client.First(&first)
	require.NoError(t, err)
	assert.Equal(t, "hello", first.Name)

	// Where + Find
	var filtered []testModel
	err = client.Where("val = ?", 42).Find(&filtered)
	require.NoError(t, err)
	assert.Len(t, filtered, 1)

	// Updates via raw DB
	err = client.DB().Model(&testModel{}).Where("id = ?", m.ID).Update("val", 99).Error
	require.NoError(t, err)

	var updated testModel
	err = client.First(&updated, m.ID)
	require.NoError(t, err)
	assert.Equal(t, 99, updated.Val)

	// Delete
	err = client.Delete(&testModel{}, m.ID)
	require.NoError(t, err)

	err = client.First(&testModel{}, m.ID)
	assert.ErrorIs(t, err, storage.ErrNotFound)
}
