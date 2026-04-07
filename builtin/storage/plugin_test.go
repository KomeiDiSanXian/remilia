package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	builtinstorage "github.com/KomeiDiSanXian/remilia/builtin/storage"
	"github.com/KomeiDiSanXian/remilia/infra/storage"
)

func TestPluginDescriptor(t *testing.T) {
	d := builtinstorage.New(storage.WithDSN(":memory:"))
	assert.Equal(t, "storage", d.Name)
	assert.NotNil(t, d.Setup)
}
