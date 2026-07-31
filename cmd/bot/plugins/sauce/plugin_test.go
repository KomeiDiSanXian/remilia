package sauce

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeConfig 最小化的 plugin.ConfigReader 实现，用于测试配置驱动的截断行为。
type fakeConfig struct {
	vals map[string]any
}

func (f *fakeConfig) Get(k string) any {
	return f.vals[k]
}

func (f *fakeConfig) GetString(k, d string) string {
	if v, ok := f.vals[k].(string); ok {
		return v
	}
	return d
}

func (f *fakeConfig) GetInt(k string, d int) int {
	if v, ok := f.vals[k].(int); ok {
		return v
	}
	return d
}

func (f *fakeConfig) GetBool(k string, d bool) bool {
	if v, ok := f.vals[k].(bool); ok {
		return v
	}
	return d
}

func (f *fakeConfig) GetDuration(k string, d time.Duration) time.Duration {
	if v, ok := f.vals[k].(time.Duration); ok {
		return v
	}
	return d
}

func (f *fakeConfig) GetFloat64(k string, d float64) float64 {
	if v, ok := f.vals[k].(float64); ok {
		return v
	}
	return d
}

func (f *fakeConfig) GetStringSlice(k string, d []string) []string {
	if v, ok := f.vals[k].([]string); ok {
		return v
	}
	return d
}

func (f *fakeConfig) GetStringMap(k string, d map[string]any) map[string]any {
	if v, ok := f.vals[k].(map[string]any); ok {
		return v
	}
	return d
}

func (f *fakeConfig) GetAll() map[string]any {
	return f.vals
}

func TestFormatOneResultTruncatesLongTitle(t *testing.T) {
	p := &Plugin{}
	longTags := strings.Repeat("tag_", 50) // 200 字符的标签串
	r := SearchResult{
		Similarity: "96.00",
		Title:      longTags,
		SourceName: "Danbooru",
		ExtURLs:    []string{"https://danbooru.donmai.us/posts/5132209"},
	}

	out := p.formatOneResult(r, 1)
	require.NotEmpty(t, out)

	lines := strings.Split(out, "\n")
	first := lines[0]
	// 首行 = "1. [96.00%] <title>"，标题被截断到 120 rune
	assert.LessOrEqual(t, utf8.RuneCountInString(first), 1+len("[96.00%] ")+maxTitleRunes+5)
	assert.Contains(t, first, "…")
	assert.Contains(t, out, "来源: Danbooru")
	assert.Contains(t, out, "https://danbooru.donmai.us/posts/5132209")
}

func TestFormatOneResultTruncationRespectsConfig(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"max_title_len": 10}}}
	out := p.formatOneResult(SearchResult{Title: strings.Repeat("x", 100)}, 1)
	first := strings.SplitN(out, "\n", 2)[0]
	assert.LessOrEqual(t, utf8.RuneCountInString(first), 1+10+5) // "1. <title10…>"
	assert.Contains(t, first, "…")
}

func TestFormatOneResultShortTitleUntouched(t *testing.T) {
	p := &Plugin{}
	out := p.formatOneResult(SearchResult{Title: "maid shikitani_asuka valentine", SourceName: "Yande.re"}, 2)
	assert.Equal(t, "2. maid shikitani_asuka valentine\n   来源: Yande.re", out)
}

func TestFormatResultsCapsMessageLength(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"max_message_len": 200}}}
	longURL := "https://example.com/" + strings.Repeat("a", 300)
	results := []SearchResult{
		{Similarity: "96.00", Title: strings.Repeat("x", 150), ExtURLs: []string{longURL}},
		{Similarity: "90.00", Title: strings.Repeat("y", 150), ExtURLs: []string{longURL}},
		{Similarity: "80.00", Title: strings.Repeat("z", 150), ExtURLs: []string{longURL}},
	}

	out := p.formatResults(results, nil, false)
	assert.LessOrEqual(t, utf8.RuneCountInString(out), 200)
	assert.Contains(t, out, "…")
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 0))
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hello…", truncate("hello world", 6))
	// CJK 按 rune 而非字节计算
	assert.Equal(t, "中文…", truncate("中文测试", 3))
}
