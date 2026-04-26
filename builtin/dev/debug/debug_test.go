package debug

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebug_New_ReturnsValidDescriptor(t *testing.T) {
	desc := New()
	assert.NotNil(t, desc)
	assert.Equal(t, "debug", desc.Name)
}

func TestDebug_Descriptor_Version(t *testing.T) {
	desc := New()
	assert.Equal(t, "2.0.0", desc.Version)
}

func TestDebug_Descriptor_Dependencies(t *testing.T) {
	desc := New()
	assert.Equal(t, []string{"permission"}, desc.Deps)
}

func TestDebug_Descriptor_Metadata(t *testing.T) {
	desc := New()
	require.NotNil(t, desc.Meta)
	assert.Equal(t, "Remilia Team", desc.Meta.Author)
	assert.Contains(t, desc.Meta.Description, "调试工具")
	assert.Equal(t, "开发", desc.Meta.Category)
	assert.NotEmpty(t, desc.Meta.Tags)
	assert.NotEmpty(t, desc.Meta.HelpText)
}

func TestDebug_InternalPlugin_CreatesWithDevModeFalse(t *testing.T) {
	p := newDebugPluginInternal()
	assert.NotNil(t, p)
	assert.False(t, p.DevMode)
}

func TestDebug_Descriptor_SetupAndTeardown(t *testing.T) {
	desc := New()
	assert.NotNil(t, desc.Setup)
	assert.NotNil(t, desc.Teardown)
}

func TestDebug_Descriptor_NotPrivileged(t *testing.T) {
	desc := New()
	assert.False(t, desc.Privileged)
}
