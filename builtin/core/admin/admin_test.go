package admin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdmin_New_ReturnsValidDescriptor(t *testing.T) {
	desc := New()
	assert.NotNil(t, desc)
	assert.Equal(t, "admin", desc.Name)
}

func TestAdmin_Descriptor_Version(t *testing.T) {
	desc := New()
	assert.Equal(t, "2.1.0", desc.Version)
}

func TestAdmin_Descriptor_Dependencies(t *testing.T) {
	desc := New()
	assert.Equal(t, []string{"permission"}, desc.Deps)
	assert.ElementsMatch(t, []string{"acl", "verifycode"}, desc.OptionalDeps)
}

func TestAdmin_Descriptor_Metadata(t *testing.T) {
	desc := New()
	require.NotNil(t, desc.Meta)
	assert.Equal(t, "Remilia Team", desc.Meta.Author)
	assert.Contains(t, desc.Meta.Description, "管理核心插件")
	assert.Equal(t, "系统", desc.Meta.Category)
	assert.NotEmpty(t, desc.Meta.Tags)
	assert.NotEmpty(t, desc.Meta.HelpText)
}

func TestAdmin_Descriptor_IsPrivileged(t *testing.T) {
	desc := New()
	assert.True(t, desc.Privileged)
}

func TestAdmin_Descriptor_SetupAndTeardown(t *testing.T) {
	desc := New()
	assert.NotNil(t, desc.Setup)
	assert.NotNil(t, desc.Teardown)
}
