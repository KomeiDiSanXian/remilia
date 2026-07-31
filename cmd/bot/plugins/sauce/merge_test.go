package sauce

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeResultsDedup(t *testing.T) {
	all := []SearchResult{
		{Source: "SauceNAO", Similarity: "90.00", ExtURLs: []string{"https://www.pixiv.net/artworks/1"}, Title: "A"},
		{Source: "IQDB", Similarity: "95.00", ExtURLs: []string{"//pixiv.net/artworks/1"}, Title: ""},
		{Source: "TraceMoe", Similarity: "88.00", ExtURLs: []string{"https://anilist.co/anime/5"}, Title: "B", Episode: "1", Timestamp: "01:00"},
	}
	merged := mergeResults(all, 0)
	require.Len(t, merged, 2)

	// 前两条去重合并：相似度取更高者，来源拼接，命中数累计
	first := merged[0]
	assert.Equal(t, "95.00", first.Similarity)
	assert.Equal(t, "SauceNAO+IQDB", first.Source)
	assert.Equal(t, 2, first.Hits)
	assert.Equal(t, "A", first.Title)
}

func TestMergeResultsSortBySimilarity(t *testing.T) {
	all := []SearchResult{
		{Source: "A", Similarity: "50.00"},
		{Source: "B", Similarity: "90.00"},
		{Source: "C", Similarity: "70.00"},
	}
	merged := mergeResults(all, 0)
	assert.Equal(t, "90.00", merged[0].Similarity)
	assert.Equal(t, "70.00", merged[1].Similarity)
	assert.Equal(t, "50.00", merged[2].Similarity)
}

func TestMergeResultsThreshold(t *testing.T) {
	all := []SearchResult{
		{Source: "A", Similarity: "90.00"},
		{Source: "B", Similarity: "40.00"},
	}

	// 存在 ≥60 的匹配时只返回高分项
	merged := mergeResults(all, 60)
	require.Len(t, merged, 1)
	assert.Equal(t, "90.00", merged[0].Similarity)

	// 全部低于阈值时保留全部（裁切图场景）
	merged = mergeResults(all, 95)
	require.Len(t, merged, 2)
}

func TestMergeResultsThresholdZero(t *testing.T) {
	all := []SearchResult{{Source: "A", Similarity: "10.00"}}
	assert.Len(t, mergeResults(all, 0), 1)
}

func TestPickResults(t *testing.T) {
	results := []SearchResult{{Similarity: "1"}, {Similarity: "2"}, {Similarity: "3"}}
	assert.Len(t, pickResults(results, 2), 2)
	assert.Len(t, pickResults(results, 0), 3)
}

func TestResultDedupKey(t *testing.T) {
	a := SearchResult{ExtURLs: []string{"https://www.pixiv.net/artworks/9"}}
	b := SearchResult{ExtURLs: []string{"//pixiv.net/artworks/9"}}
	assert.Equal(t, resultDedupKey(a), resultDedupKey(b))
}
