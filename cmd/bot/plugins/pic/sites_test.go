package pic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rng 测试辅助：构造精确档位区间。
func rng(r Rating) RatingRange {
	return RatingRange{Min: r, Max: r}
}

// rngRange 测试辅助：构造区间 [min..max]。
func rngRange(min, max Rating) RatingRange {
	return RatingRange{Min: min, Max: max}
}

func TestRatingRank(t *testing.T) {
	assert.Equal(t, 1, RatingSafe.rank())
	assert.Equal(t, 2, RatingSensitive.rank())
	assert.Equal(t, 3, RatingQuestionable.rank())
	assert.Equal(t, 4, RatingExplicit.rank())
	assert.Equal(t, 0, Rating("unknown").rank())
}

func TestRatingRangeContains(t *testing.T) {
	// 单档区间
	assert.True(t, rng(RatingSafe).contains(RatingSafe))
	assert.False(t, rng(RatingSafe).contains(RatingSensitive))
	assert.False(t, rng(RatingSafe).contains(RatingExplicit))

	// 多档区间
	mid := rngRange(RatingSensitive, RatingQuestionable)
	assert.False(t, mid.contains(RatingSafe))
	assert.True(t, mid.contains(RatingSensitive))
	assert.True(t, mid.contains(RatingQuestionable))
	assert.False(t, mid.contains(RatingExplicit))

	// 全区间（all）
	all := rngRange(RatingSafe, RatingExplicit)
	for _, r := range []Rating{RatingSafe, RatingSensitive, RatingQuestionable, RatingExplicit} {
		assert.True(t, all.contains(r))
	}
}

func TestParseRating(t *testing.T) {
	assert.Equal(t, RatingSafe, parseRating("safe"))
	assert.Equal(t, RatingSensitive, parseRating("sensitive"))
	assert.Equal(t, RatingQuestionable, parseRating("questionable"))
	assert.Equal(t, RatingExplicit, parseRating("explicit"))
	// 非法值回退 safe
	assert.Equal(t, RatingSafe, parseRating("garbage"))
	assert.Equal(t, RatingSafe, parseRating(""))
}

func TestParseRatingRange(t *testing.T) {
	// 单档 = 精确档位
	assert.Equal(t, rng(RatingSafe), parseRatingRange("safe"))
	assert.Equal(t, rng(RatingExplicit), parseRatingRange("explicit"))
	// 区间
	assert.Equal(t, rngRange(RatingSafe, RatingQuestionable), parseRatingRange("safe..questionable"))
	assert.Equal(t, rngRange(RatingQuestionable, RatingExplicit), parseRatingRange("questionable..explicit"))
	// all = 全区间
	assert.Equal(t, rngRange(RatingSafe, RatingExplicit), parseRatingRange("all"))
	// 乱序区间自动矫正
	assert.Equal(t, rngRange(RatingQuestionable, RatingExplicit), parseRatingRange("explicit..questionable"))
	// 非法值回退 safe
	assert.Equal(t, rng(RatingSafe), parseRatingRange("garbage"))
	assert.Equal(t, rng(RatingSafe), parseRatingRange(""))
}

func TestRatingRangeString(t *testing.T) {
	assert.Equal(t, "safe", rng(RatingSafe).String())
	assert.Equal(t, "safe..questionable", rngRange(RatingSafe, RatingQuestionable).String())
	assert.Equal(t, "all", rngRange(RatingSafe, RatingExplicit).String())
}

func TestSiteUsable(t *testing.T) {
	rule34, _ := findSite("rule34")
	safebooru, _ := findSite("safebooru")
	gelbooru, _ := findSite("gelbooru")

	// rule34 仅 explicit 档：safe/sensitive/questionable 区间不可用
	assert.False(t, rule34.usable(rng(RatingSafe)))
	assert.False(t, rule34.usable(rng(RatingSensitive)))
	assert.False(t, rule34.usable(rng(RatingQuestionable)))
	assert.True(t, rule34.usable(rng(RatingExplicit)))
	assert.True(t, rule34.usable(rngRange(RatingSafe, RatingExplicit)))

	// safebooru 仅 safe 档：不含 safe 的区间不可用
	assert.True(t, safebooru.usable(rng(RatingSafe)))
	assert.True(t, safebooru.usable(rngRange(RatingSafe, RatingExplicit)))
	assert.False(t, safebooru.usable(rng(RatingSensitive)))
	assert.False(t, safebooru.usable(rngRange(RatingQuestionable, RatingExplicit)))

	// gelbooru 4 档齐全
	assert.True(t, gelbooru.usable(rng(RatingSafe)))
	assert.True(t, gelbooru.usable(rng(RatingSensitive)))
	assert.True(t, gelbooru.usable(rngRange(RatingQuestionable, RatingExplicit)))
}

func TestSiteRangeTags(t *testing.T) {
	gelbooru, _ := findSite("gelbooru")
	safebooru, _ := findSite("safebooru")
	konachan, _ := findSite("konachan")
	yandere, _ := findSite("yandere")
	rule34, _ := findSite("rule34")

	// gelbooru 单档：正标签（safe 映射为 general，与站点迁移后的体系一致）
	assert.Equal(t, []string{"rating:general"}, gelbooru.rangeTags(rng(RatingSafe)))
	assert.Equal(t, []string{"rating:sensitive"}, gelbooru.rangeTags(rng(RatingSensitive)))
	assert.Equal(t, []string{"rating:questionable"}, gelbooru.rangeTags(rng(RatingQuestionable)))
	assert.Equal(t, []string{"rating:explicit"}, gelbooru.rangeTags(rng(RatingExplicit)))
	// gelbooru 多档：排除法
	assert.Equal(t, []string{"-rating:explicit"}, gelbooru.rangeTags(rngRange(RatingSafe, RatingQuestionable)))
	assert.Equal(t, []string{"-rating:questionable", "-rating:explicit"}, gelbooru.rangeTags(rngRange(RatingSafe, RatingSensitive)))
	assert.Equal(t, []string{"-rating:general", "-rating:sensitive"}, gelbooru.rangeTags(rngRange(RatingQuestionable, RatingExplicit)))
	// 全区间：无过滤
	assert.Empty(t, gelbooru.rangeTags(rngRange(RatingSafe, RatingExplicit)))

	// safebooru 整站仅 safe：覆盖即无过滤
	assert.Empty(t, safebooru.rangeTags(rng(RatingSafe)))
	assert.Empty(t, safebooru.rangeTags(rngRange(RatingSafe, RatingExplicit)))
	assert.Nil(t, safebooru.rangeTags(rng(RatingSensitive)))

	// konachan 同 safebooru
	assert.Empty(t, konachan.rangeTags(rng(RatingSafe)))
	assert.Nil(t, konachan.rangeTags(rng(RatingExplicit)))

	// yande.re 旧体系（无 sensitive 档）：精确 sensitive 区间不可用
	assert.Equal(t, []string{"rating:safe"}, yandere.rangeTags(rng(RatingSafe)))
	assert.Nil(t, yandere.rangeTags(rng(RatingSensitive)))
	assert.Equal(t, []string{"-rating:explicit"}, yandere.rangeTags(rngRange(RatingSafe, RatingQuestionable)))
	assert.Equal(t, []string{"-rating:safe"}, yandere.rangeTags(rngRange(RatingQuestionable, RatingExplicit)))

	// rule34 站点定位仅 explicit
	assert.Equal(t, []string{"rating:explicit"}, rule34.rangeTags(rng(RatingExplicit)))
	assert.Equal(t, []string{"rating:explicit"}, rule34.rangeTags(rngRange(RatingSafe, RatingExplicit)))
	assert.Nil(t, rule34.rangeTags(rng(RatingSafe)))
}

func TestCandidateSitesByRating(t *testing.T) {
	// safe 精确档位：safebooru / gelbooru / konachan / yandere（无 rule34）
	for range 20 {
		sites := candidateSites(nil, rng(RatingSafe))
		require.NotEmpty(t, sites)
		for _, s := range sites {
			assert.True(t, s.usable(rng(RatingSafe)))
			assert.NotEqual(t, "rule34", s.Name)
		}
	}

	// explicit 精确档位：rule34 可入选，safebooru/konachan 不可用
	foundRule34 := false
	for range 20 {
		for _, s := range candidateSites(nil, rng(RatingExplicit)) {
			if s.Name == "rule34" {
				foundRule34 = true
			}
			assert.NotEqual(t, "safebooru", s.Name)
			assert.NotEqual(t, "konachan", s.Name)
		}
	}
	assert.True(t, foundRule34, "explicit 区间下应包含 rule34")

	// sensitive 精确档位：仅 gelbooru（其余站点无此档）
	for range 20 {
		for _, s := range candidateSites(nil, rng(RatingSensitive)) {
			assert.Equal(t, "gelbooru", s.Name)
		}
	}

	// safe..explicit（all）：全部内置站点
	sites := candidateSites(nil, rngRange(RatingSafe, RatingExplicit))
	assert.Len(t, sites, len(builtinSites))
}

func TestCandidateSitesWithAllowlist(t *testing.T) {
	// 白名单限制在 konachan / yandere
	for range 20 {
		sites := candidateSites([]string{"konachan", "yandere"}, rng(RatingSafe))
		require.NotEmpty(t, sites)
		for _, s := range sites {
			assert.Contains(t, []string{"konachan", "yandere"}, s.Name)
		}
	}

	// 白名单中的站点在区间下不可用 → 空
	assert.Empty(t, candidateSites([]string{"rule34"}, rng(RatingSafe)))
	assert.Empty(t, candidateSites([]string{"safebooru"}, rng(RatingExplicit)))

	// 白名单包含不存在的名字 → 空
	assert.Empty(t, candidateSites([]string{"nonexistent"}, rng(RatingSafe)))
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
	assert.Equal(t, "rating:sensitive", ratingTag(RatingSensitive))
	assert.Equal(t, "rating:explicit", ratingTag(RatingExplicit))
}

func TestSiteRatingSearchTag(t *testing.T) {
	gelbooru, _ := findSite("gelbooru")
	safebooru, _ := findSite("safebooru")
	konachan, _ := findSite("konachan")
	yandere, _ := findSite("yandere")
	rule34, _ := findSite("rule34")

	// gelbooru：safe 映射为 general（站点已迁移分级体系）
	assert.Equal(t, "rating:general", gelbooru.ratingSearchTag(RatingSafe))
	assert.Equal(t, "rating:sensitive", gelbooru.ratingSearchTag(RatingSensitive))
	assert.Equal(t, "rating:questionable", gelbooru.ratingSearchTag(RatingQuestionable))
	assert.Equal(t, "rating:explicit", gelbooru.ratingSearchTag(RatingExplicit))

	// safebooru 整站仅 safe 内容，不附加 rating 过滤（新旧评级并存）
	assert.Equal(t, "", safebooru.ratingSearchTag(RatingSafe))

	// konachan.net 为 SFW 镜像，整站仅 safe 内容，不附加过滤
	assert.Equal(t, "", konachan.ratingSearchTag(RatingSafe))

	// yande.re 保留旧体系
	assert.Equal(t, "rating:safe", yandere.ratingSearchTag(RatingSafe))
	assert.Equal(t, "rating:questionable", yandere.ratingSearchTag(RatingQuestionable))
	assert.Equal(t, "rating:explicit", yandere.ratingSearchTag(RatingExplicit))

	// rule34 站点定位仅 explicit，显式过滤（其遗留 safe/questionable 帖较多）
	assert.Equal(t, "rating:explicit", rule34.ratingSearchTag(RatingExplicit))
}

func TestBuildTags(t *testing.T) {
	konachan, _ := findSite("konachan")
	gelbooru, _ := findSite("gelbooru")
	safebooru, _ := findSite("safebooru")
	yandere, _ := findSite("yandere")

	assert.Equal(t, "touhou hairband rating:safe", buildTags(yandere, []string{"touhou", "hairband"}, rng(RatingSafe)))
	// 全区间不附加 rating 标签
	assert.Equal(t, "cat", buildTags(yandere, []string{"cat"}, rngRange(RatingSafe, RatingExplicit)))
	assert.Equal(t, "rating:safe", buildTags(yandere, nil, rng(RatingSafe)))

	// gelbooru：safe 映射为 rating:general；区间 [safe..questionable] 排除 explicit
	assert.Equal(t, "cat rating:general", buildTags(gelbooru, []string{"cat"}, rng(RatingSafe)))
	assert.Equal(t, "cat -rating:explicit", buildTags(gelbooru, []string{"cat"}, rngRange(RatingSafe, RatingQuestionable)))
	// safebooru/konachan：不附加 rating 标签
	assert.Equal(t, "cat", buildTags(safebooru, []string{"cat"}, rng(RatingSafe)))
	assert.Equal(t, "cat", buildTags(konachan, []string{"cat"}, rng(RatingSafe)))
}
