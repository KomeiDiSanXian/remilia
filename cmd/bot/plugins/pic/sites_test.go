package pic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRatingAllows(t *testing.T) {
	assert.True(t, RatingSafe.allows(RatingSafe))
	assert.False(t, RatingSafe.allows(RatingQuestionable))
	assert.False(t, RatingSafe.allows(RatingExplicit))

	assert.True(t, RatingQuestionable.allows(RatingSafe))
	assert.True(t, RatingQuestionable.allows(RatingQuestionable))
	assert.False(t, RatingQuestionable.allows(RatingExplicit))

	assert.True(t, RatingExplicit.allows(RatingExplicit))
	assert.True(t, RatingExplicit.allows(RatingSafe))

	// "all" 不限制
	assert.True(t, Rating("all").allows(RatingExplicit))
}

func TestParseRating(t *testing.T) {
	assert.Equal(t, RatingSafe, parseRating("safe"))
	assert.Equal(t, RatingQuestionable, parseRating("questionable"))
	assert.Equal(t, RatingExplicit, parseRating("explicit"))
	assert.Equal(t, Rating("all"), parseRating("all"))
	// 非法值回退 safe
	assert.Equal(t, RatingSafe, parseRating("garbage"))
	assert.Equal(t, RatingSafe, parseRating(""))
}

func TestSiteUsable(t *testing.T) {
	rule34, _ := findSite("rule34")
	safebooru, _ := findSite("safebooru")

	// rule34 仅 explicit 内容：safe 策略不可用
	assert.False(t, rule34.usable(RatingSafe))
	assert.False(t, rule34.usable(RatingQuestionable))
	assert.True(t, rule34.usable(RatingExplicit))
	assert.True(t, rule34.usable("all"))

	// safebooru 仅 safe 内容：任何策略都可用
	assert.True(t, safebooru.usable(RatingSafe))
	assert.True(t, safebooru.usable(RatingExplicit))
}

func TestSiteEffectiveRating(t *testing.T) {
	rule34, _ := findSite("rule34")
	safebooru, _ := findSite("safebooru")
	gelbooru, _ := findSite("gelbooru")

	// rule34 强制 explicit
	assert.Equal(t, RatingExplicit, rule34.effectiveRating(RatingExplicit))
	assert.Equal(t, RatingExplicit, rule34.effectiveRating("all"))
	// safebooru 最高只能到 safe
	assert.Equal(t, RatingSafe, safebooru.effectiveRating(RatingExplicit))
	// gelbooru 取 min(策略, 站点最高)
	assert.Equal(t, RatingQuestionable, gelbooru.effectiveRating(RatingQuestionable))
	assert.Equal(t, RatingExplicit, gelbooru.effectiveRating(RatingExplicit))
}

func TestCandidateSitesByRating(t *testing.T) {
	// safe 策略下不应包含 rule34
	for i := 0; i < 20; i++ {
		sites := candidateSites(nil, RatingSafe)
		require.NotEmpty(t, sites)
		for _, s := range sites {
			assert.True(t, s.usable(RatingSafe))
			assert.NotEqual(t, "rule34", s.Name)
		}
	}

	// explicit 策略下 rule34 可入选
	foundRule34 := false
	for i := 0; i < 20; i++ {
		for _, s := range candidateSites(nil, RatingExplicit) {
			if s.Name == "rule34" {
				foundRule34 = true
			}
		}
	}
	assert.True(t, foundRule34, "explicit 策略下应有机会包含 rule34")

	// 列表应包含全部内置站点（safe 下为 4 个）
	sites := candidateSites(nil, RatingSafe)
	assert.Len(t, sites, len(builtinSites)-1)
}

func TestCandidateSitesWithAllowlist(t *testing.T) {
	// 白名单限制在 konachan / yandere
	for i := 0; i < 20; i++ {
		sites := candidateSites([]string{"konachan", "yandere"}, RatingSafe)
		require.NotEmpty(t, sites)
		for _, s := range sites {
			assert.Contains(t, []string{"konachan", "yandere"}, s.Name)
		}
	}

	// 白名单中的站点在策略下不可用 → 空
	assert.Empty(t, candidateSites([]string{"rule34"}, RatingSafe))

	// 白名单包含不存在的名字 → 空
	assert.Empty(t, candidateSites([]string{"nonexistent"}, RatingSafe))
}

func TestFindSiteCaseInsensitive(t *testing.T) {
	s, ok := findSite("KONACHAN")
	require.True(t, ok)
	assert.Equal(t, "konachan", s.Name)
	assert.Equal(t, "Konachan", s.DisplayName)

	_, ok = findSite("unknown")
	assert.False(t, ok)
}

func TestRatingTag(t *testing.T) {
	assert.Equal(t, "rating:safe", ratingTag(RatingSafe))
	assert.Equal(t, "rating:explicit", ratingTag(RatingExplicit))
	assert.Equal(t, "", ratingTag("all"))
}

func TestBuildTags(t *testing.T) {
	assert.Equal(t, "touhou hairband rating:safe", buildTags([]string{"touhou", "hairband"}, RatingSafe))
	assert.Equal(t, "cat rating:explicit", buildTags([]string{"cat"}, RatingExplicit))
	// "all" 不附加 rating 标签
	assert.Equal(t, "cat", buildTags([]string{"cat"}, "all"))
	assert.Equal(t, "rating:safe", buildTags(nil, RatingSafe))
}
