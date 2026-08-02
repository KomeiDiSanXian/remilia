package bundle

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/assert"
)

func TestCoreReturnsExpectedPlugins(t *testing.T) {
	plugins := Core()
	names := pluginNames(plugins)

	assert.Contains(t, names, "permission")
	assert.Contains(t, names, "acl")
	assert.Contains(t, names, "help")
	assert.Len(t, plugins, 3)
}

func TestAllReturnsAllPlugins(t *testing.T) {
	all := All()
	allNames := pluginNames(all)
	coreNames := pluginNames(Core())

	for _, name := range coreNames {
		assert.Contains(t, allNames, name)
	}
	assert.Contains(t, allNames, "about")
	assert.Contains(t, allNames, "cooldown")
	assert.Contains(t, allNames, "welcome")
	assert.Contains(t, allNames, "autoresponder")
	assert.Contains(t, allNames, "moderation")
	assert.Contains(t, allNames, "customcommands")
	assert.Len(t, all, len(Core())+6)
}

func TestDevReturnsDevPlugins(t *testing.T) {
	plugins := Dev()
	names := pluginNames(plugins)

	assert.Contains(t, names, "admin")
	assert.Contains(t, names, "debug")
	assert.Len(t, plugins, 2)
}

func TestCoreOrderIsCorrect(t *testing.T) {
	plugins := Core()
	names := pluginNames(plugins)

	assert.Equal(t, "permission", names[0])
	assert.Equal(t, "acl", names[1])
	assert.Equal(t, "help", names[2])
}

func TestAllOrderIsCorrect(t *testing.T) {
	plugins := All()
	names := pluginNames(plugins)

	assert.Equal(t, "permission", names[0])
	assert.Equal(t, "acl", names[1])
	assert.Equal(t, "help", names[2])
	assert.Equal(t, "about", names[3])
	assert.Equal(t, "cooldown", names[4])
	assert.Equal(t, "welcome", names[5])
	assert.Equal(t, "autoresponder", names[6])
	assert.Equal(t, "moderation", names[7])
	assert.Equal(t, "customcommands", names[8])
}

func TestDevOrderIsCorrect(t *testing.T) {
	plugins := Dev()
	names := pluginNames(plugins)

	assert.Equal(t, "admin", names[0])
	assert.Equal(t, "debug", names[1])
}

func TestNoDuplicatesInCore(t *testing.T) {
	names := pluginNames(Core())
	assert.Len(t, names, len(unique(names)))
}

func TestNoDuplicatesInAll(t *testing.T) {
	names := pluginNames(All())
	assert.Len(t, names, len(unique(names)))
}

func TestNoDuplicatesInDev(t *testing.T) {
	names := pluginNames(Dev())
	assert.Len(t, names, len(unique(names)))
}

func pluginNames(plugins []*plugin.Descriptor) []string {
	names := make([]string, len(plugins))
	for i, p := range plugins {
		names[i] = p.Name
	}
	return names
}

func unique(strs []string) []string {
	seen := make(map[string]struct{}, len(strs))
	res := make([]string, 0, len(strs))
	for _, s := range strs {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		res = append(res, s)
	}
	return res
}
