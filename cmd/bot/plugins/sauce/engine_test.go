package sauce

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEngineSetDefault(t *testing.T) {
	p := &Plugin{}
	// 无配置时默认启用 IQDB/TraceMoe/AnimeTrace（SauceNAO 无 key 不可用）
	s, err := p.parseEngineSet("")
	require.NoError(t, err)
	assert.True(t, s["iqdb"])
	assert.True(t, s["tracemoe"])
	assert.True(t, s["animetrace"])
	assert.False(t, s["saucenao"])

	s, err = p.parseEngineSet("all")
	require.NoError(t, err)
	assert.True(t, s["iqdb"])

	s, err = p.parseEngineSet("ALL ")
	require.NoError(t, err)
	assert.True(t, s["iqdb"])
}

func TestParseEngineSetWithAPIKey(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"saucenao_api_key": "key"}}}
	s, err := p.parseEngineSet("all")
	require.NoError(t, err)
	assert.True(t, s["saucenao"])
	assert.True(t, s["iqdb"])
	assert.True(t, s["tracemoe"])
	assert.True(t, s["animetrace"])
}

func TestParseEngineSetSingle(t *testing.T) {
	p := &Plugin{}
	s, err := p.parseEngineSet("tracemoe")
	require.NoError(t, err)
	assert.Len(t, s, 1)
	assert.True(t, s["tracemoe"])
}

func TestParseEngineSetMultiple(t *testing.T) {
	p := &Plugin{}
	s, err := p.parseEngineSet("tracemoe, animetrace")
	require.NoError(t, err)
	assert.Len(t, s, 2)
	assert.True(t, s["tracemoe"])
	assert.True(t, s["animetrace"])
	assert.False(t, s["iqdb"])
}

func TestParseEngineSetUnknown(t *testing.T) {
	p := &Plugin{}
	_, err := p.parseEngineSet("yandex")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未知引擎")
}

func TestParseEngineSetRespectsDisabledConfig(t *testing.T) {
	// 配置关闭的引擎即使 -engine 指定也不启用（searchAll 二次校验）
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{
		"enable_iqdb": false, "enable_animetrace": false,
	}}}
	s, err := p.parseEngineSet("iqdb,animetrace,tracemoe")
	require.NoError(t, err)
	assert.True(t, s["iqdb"]) // parseEngineSet 只做名称合法性过滤
	assert.True(t, s["animetrace"])

	// searchAll 层面会按 enable 开关跳过
	eng := p.allEngines()
	assert.False(t, eng["iqdb"])
	assert.False(t, eng["animetrace"])
	assert.True(t, eng["tracemoe"])
}
