package idiomdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdiomdict_Contains_KnownIdiom(t *testing.T) {
	assert.True(t, Contains("一马当先"))
	assert.True(t, Contains("守株待兔"))
	assert.True(t, Contains("画蛇添足"))
}

func TestIdiomdict_Contains_UnknownString(t *testing.T) {
	assert.False(t, Contains("这不是成语"))
	assert.False(t, Contains(""))
}

func TestIdiomdict_Contains_SpecialCharacters(t *testing.T) {
	assert.False(t, Contains(" "))
	assert.False(t, Contains("\t"))
	assert.False(t, Contains("\n"))
}

func TestIdiomdict_Contains_ExtIdiom(t *testing.T) {
	assert.True(t, Contains("拔苗助长"))
	assert.True(t, Contains("画龙点睛"))
	assert.True(t, Contains("卧薪尝胆"))
}

func TestIdiomdict_All_ReturnsAllIdioms(t *testing.T) {
	result := All()
	require.NotEmpty(t, result)
	assert.Greater(t, len(result), 500)
	assert.Contains(t, result, "一马当先")
	assert.Contains(t, result, "卧薪尝胆")
}

func TestIdiomdict_Count_ReturnsCorrectTotal(t *testing.T) {
	count := Count()
	all := All()
	assert.Equal(t, len(all), count)
	assert.Greater(t, count, 500)
}

func TestIdiomdict_Random_ReturnsValidIdiom(t *testing.T) {
	idiom := Random()
	assert.NotEmpty(t, idiom)
	assert.True(t, Contains(idiom))
}

func TestIdiomdict_Random_ReturnsDifferentValues(t *testing.T) {
	results := make(map[string]int)
	for range 50 {
		results[Random()]++
	}
	assert.GreaterOrEqual(t, len(results), 2)
}

func TestIdiomdict_All_ReturnsCopy(t *testing.T) {
	result := All()
	originalCount := len(result)
	result[0] = "modified"
	assert.Equal(t, originalCount, Count())
	if Count() > 0 {
		assert.NotEqual(t, result[0], All()[0])
	}
}

func TestIdiomdict_Contains_InitIsLazy(t *testing.T) {
	assert.True(t, Contains("爱不释手"))
}
